package store

import (
	"database/sql"
	"time"

	"snippetbook/internal/model"
)

func (d *DB) ListRunbooks(serviceID int64) ([]model.Runbook, error) {
	rows, err := d.sql.Query(`SELECT id,service_id,title,content,created_at,updated_at FROM runbooks WHERE service_id=? ORDER BY title COLLATE NOCASE,id`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.Runbook{}
	for rows.Next() {
		var item model.Runbook
		var created, updated string
		if err := rows.Scan(&item.ID, &item.ServiceID, &item.Title, &item.Content, &created, &updated); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339, created)
		item.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (d *DB) CreateRunbook(item *model.Runbook) error {
	now := d.now()
	result, err := d.sql.Exec(`INSERT INTO runbooks(service_id,title,content,created_at,updated_at) VALUES(?,?,?,?,?)`, item.ServiceID, item.Title, item.Content, now, now)
	if err != nil {
		return err
	}
	item.ID, err = result.LastInsertId()
	item.CreatedAt, _ = time.Parse(time.RFC3339, now)
	item.UpdatedAt = item.CreatedAt
	return err
}

func (d *DB) UpdateRunbook(item *model.Runbook) error {
	now := d.now()
	_, err := d.sql.Exec(`UPDATE runbooks SET title=?,content=?,updated_at=? WHERE id=? AND service_id=?`, item.Title, item.Content, now, item.ID, item.ServiceID)
	if err == nil {
		item.UpdatedAt, _ = time.Parse(time.RFC3339, now)
	}
	return err
}

func (d *DB) DeleteRunbook(id int64) error {
	_, err := d.sql.Exec(`DELETE FROM runbooks WHERE id=?`, id)
	return err
}

func (d *DB) GetRunbook(id int64) (model.Runbook, error) {
	var item model.Runbook
	var created, updated string
	err := d.sql.QueryRow(`SELECT id,service_id,title,content,created_at,updated_at FROM runbooks WHERE id=?`, id).Scan(&item.ID, &item.ServiceID, &item.Title, &item.Content, &created, &updated)
	if err != nil {
		return item, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return item, nil
}

func IsNotFound(err error) bool { return err == sql.ErrNoRows }
