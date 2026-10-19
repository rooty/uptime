package store

import (
	"strings"
	"time"

	"snippetbook/internal/model"
)

func (d *DB) Create(item *model.Item) (int64, error) {
	now := d.now()
	if item.Tags == nil {
		item.Tags = []string{}
	}
	if item.UUID == "" {
		item.UUID = newUUID()
	}
	res, err := d.sql.Exec(
		`INSERT INTO items (uuid,kind,title,language,content,tags,favorite,created_at,updated_at,deleted_at) VALUES (?,?,?,?,?,?,?,?,?,NULL)`,
		item.UUID, item.Kind, item.Title, item.Language, item.Content, d.tagsToJSON(item.Tags), boolToInt(item.Favorite), now, now,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	item.ID = id
	item.CreatedAt, _ = parseTime(now)
	item.UpdatedAt = item.CreatedAt
	item.DeletedAt = nil
	d.updateFTS(id, item.Title, item.Content, d.tagsToJSON(item.Tags))
	return id, nil
}

func (d *DB) Get(id int64) (model.Item, error) {
	row := d.sql.QueryRow(
		`SELECT id,uuid,kind,title,language,content,tags,favorite,created_at,updated_at,deleted_at FROM items WHERE id=?`, id)
	it, err := d.scanItem(row)
	if err != nil {
		return it, err
	}
	return it, nil
}

func (d *DB) GetByUUID(u string) (model.Item, error) {
	row := d.sql.QueryRow(
		`SELECT id,uuid,kind,title,language,content,tags,favorite,created_at,updated_at,deleted_at FROM items WHERE uuid=?`, u)
	it, err := d.scanItem(row)
	if err != nil {
		return it, err
	}
	return it, nil
}

func (d *DB) Update(item *model.Item) error {
	if item.Tags == nil {
		item.Tags = []string{}
	}
	if item.UUID == "" {
		it, err := d.Get(item.ID)
		if err == nil {
			item.UUID = it.UUID
		}
	}
	now := d.now()
	deleted := interface{}(nil)
	if item.DeletedAt != nil {
		deleted = item.DeletedAt.UTC().Format(time.RFC3339)
	}
	_, err := d.sql.Exec(
		`UPDATE items SET uuid=?,kind=?,title=?,language=?,content=?,tags=?,favorite=?,updated_at=?,deleted_at=? WHERE id=?`,
		item.UUID, item.Kind, item.Title, item.Language, item.Content, d.tagsToJSON(item.Tags), boolToInt(item.Favorite), now, deleted, item.ID,
	)
	if err != nil {
		return err
	}
	item.UpdatedAt, _ = parseTime(now)
	d.deleteFTS(item.ID)
	if item.DeletedAt == nil {
		d.updateFTS(item.ID, item.Title, item.Content, d.tagsToJSON(item.Tags))
	}
	return nil
}

func (d *DB) SoftDelete(id int64) error {
	now := d.now()
	_, err := d.sql.Exec(`UPDATE items SET deleted_at=?,updated_at=? WHERE id=?`, now, now, id)
	if err != nil {
		return err
	}
	d.deleteFTS(id)
	return nil
}

func (d *DB) Delete(id int64) error {
	_, err := d.sql.Exec(`DELETE FROM items WHERE id=?`, id)
	if err != nil {
		return err
	}
	d.deleteFTS(id)
	_, _ = d.sql.Exec(`DELETE FROM secrets WHERE item_id=?`, id)
	return nil
}

func (d *DB) List(kind, tag, q string, limit, offset int) ([]model.Item, error) {
	if limit <= 0 {
		limit = 50
	}
	sb := strings.Builder{}
	sb.WriteString(`SELECT id,uuid,kind,title,language,content,tags,favorite,created_at,updated_at,deleted_at FROM items WHERE deleted_at IS NULL`)
	args := []any{}
	if kind != "" {
		sb.WriteString(` AND kind=?`)
		args = append(args, kind)
	}
	if tag != "" {
		sb.WriteString(` AND tags LIKE ?`)
		args = append(args, `%"`+tag+`"%`)
	}
	if q != "" {
		sb.WriteString(` AND (title LIKE ? OR content LIKE ? OR tags LIKE ?)`)
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	sb.WriteString(` ORDER BY updated_at DESC LIMIT ? OFFSET ?`)
	args = append(args, limit, offset)

	rows, err := d.sql.Query(sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Item{}
	for rows.Next() {
		it, err := d.scanItemRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (d *DB) SetSecret(itemID int64, ciphertext, nonce []byte) error {
	_, err := d.sql.Exec(
		`INSERT INTO secrets (item_id,ciphertext,nonce) VALUES (?,?,?) ON CONFLICT(item_id) DO UPDATE SET ciphertext=excluded.ciphertext, nonce=excluded.nonce`,
		itemID, ciphertext, nonce,
	)
	return err
}

func (d *DB) GetSecret(itemID int64) ([]byte, []byte, error) {
	var ct, nonce []byte
	err := d.sql.QueryRow(`SELECT ciphertext,nonce FROM secrets WHERE item_id=?`, itemID).Scan(&ct, &nonce)
	if err != nil {
		return nil, nil, err
	}
	return ct, nonce, nil
}

// ListChangedSince returns items (including soft-deleted) updated after the given time.
func (d *DB) ListChangedSince(since time.Time) ([]model.Item, error) {
	sincstr := since.UTC().Format(time.RFC3339)
	rows, err := d.sql.Query(
		`SELECT id,uuid,kind,title,language,content,tags,favorite,created_at,updated_at,deleted_at FROM items WHERE updated_at > ? ORDER BY updated_at ASC`,
		sincstr,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Item{}
	for rows.Next() {
		it, err := d.scanItemRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (d *DB) DeleteSecretByItem(itemID int64) error {
	_, err := d.sql.Exec(`DELETE FROM secrets WHERE item_id=?`, itemID)
	return err
}

func nullTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func (d *DB) updateFTS(id int64, title, content, tags string) {
	_, _ = d.sql.Exec(`INSERT INTO items_fts(rowid,title,content,tags) VALUES(?,?,?,?)`, id, title, content, tags)
}

func (d *DB) deleteFTS(id int64) {
	_, _ = d.sql.Exec(`DELETE FROM items_fts WHERE rowid=?`, id)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}
