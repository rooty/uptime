package store

import (
	"time"

	"snippetbook/internal/model"
)

func (d *DB) AddNotification(item *model.Notification) error {
	result, err := d.sql.Exec(`INSERT INTO notifications(kind,title,message,service_id,read,created_at) VALUES(?,?,?,?,?,?)`, item.Kind, item.Title, item.Message, item.ServiceID, boolToInt(item.Read), item.CreatedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	item.ID, err = result.LastInsertId()
	return err
}

func (d *DB) ListNotifications(limit int) ([]model.Notification, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := d.sql.Query(`SELECT id,kind,title,message,service_id,read,created_at FROM notifications ORDER BY created_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.Notification{}
	for rows.Next() {
		var item model.Notification
		var created string
		var read int
		if err := rows.Scan(&item.ID, &item.Kind, &item.Title, &item.Message, &item.ServiceID, &read, &created); err != nil {
			return nil, err
		}
		item.Read = read == 1
		item.CreatedAt, _ = time.Parse(time.RFC3339, created)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (d *DB) MarkNotificationsRead() error {
	_, err := d.sql.Exec(`UPDATE notifications SET read=1 WHERE read=0`)
	return err
}
