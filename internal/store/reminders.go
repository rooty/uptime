package store

import (
	"time"

	"snippetbook/internal/model"
)

func (d *DB) ListReminders(includeCompleted bool) ([]model.Reminder, error) {
	query := `SELECT id,service_id,title,description,due_at,completed,created_at,updated_at FROM reminders ORDER BY completed,due_at,id`
	args := []any{}
	if !includeCompleted {
		query = `SELECT id,service_id,title,description,due_at,completed,created_at,updated_at FROM reminders WHERE completed=0 ORDER BY due_at,id`
	}
	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.Reminder{}
	for rows.Next() {
		item, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (d *DB) CreateReminder(item *model.Reminder) error {
	now := d.now()
	result, err := d.sql.Exec(`INSERT INTO reminders(service_id,title,description,due_at,completed,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, item.ServiceID, item.Title, item.Description, item.DueAt.UTC().Format(time.RFC3339), boolToInt(item.Completed), now, now)
	if err != nil {
		return err
	}
	item.ID, err = result.LastInsertId()
	item.CreatedAt, _ = time.Parse(time.RFC3339, now)
	item.UpdatedAt = item.CreatedAt
	return err
}

func (d *DB) UpdateReminder(item *model.Reminder) error {
	now := d.now()
	_, err := d.sql.Exec(`UPDATE reminders SET service_id=?,title=?,description=?,due_at=?,completed=?,updated_at=? WHERE id=?`, item.ServiceID, item.Title, item.Description, item.DueAt.UTC().Format(time.RFC3339), boolToInt(item.Completed), now, item.ID)
	if err == nil {
		item.UpdatedAt, _ = time.Parse(time.RFC3339, now)
	}
	return err
}

func (d *DB) DeleteReminder(id int64) error {
	_, err := d.sql.Exec(`DELETE FROM reminders WHERE id=?`, id)
	return err
}

func scanReminder(rows interface{ Scan(...any) error }) (model.Reminder, error) {
	var item model.Reminder
	var due, created, updated string
	var completed int
	err := rows.Scan(&item.ID, &item.ServiceID, &item.Title, &item.Description, &due, &completed, &created, &updated)
	if err != nil {
		return item, err
	}
	item.DueAt, _ = time.Parse(time.RFC3339, due)
	item.CreatedAt, _ = time.Parse(time.RFC3339, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	item.Completed = completed == 1
	return item, nil
}
