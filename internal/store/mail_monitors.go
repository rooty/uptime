package store

import (
	"database/sql"
	"time"

	"snippetbook/internal/model"
)

func (d *DB) CreateMailMonitor(monitor *model.MailMonitor) error {
	now := time.Now().UTC()
	if monitor.CheckInterval == "" {
		monitor.CheckInterval = "24h"
	}
	monitor.CreatedAt, monitor.UpdatedAt = now, now
	result, err := d.sql.Exec(`INSERT INTO mail_monitors(domain,dkim_selector,check_interval,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		monitor.Domain, monitor.DKIMSelector, monitor.CheckInterval, boolInt(monitor.Enabled), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	monitor.ID, err = result.LastInsertId()
	return err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (d *DB) ListMailMonitors(enabledOnly bool) ([]model.MailMonitor, error) {
	query := `SELECT id,domain,dkim_selector,check_interval,enabled,last_status,last_score,last_checked_at,created_at,updated_at,registration_status,registration_expires_at,registration_fingerprint FROM mail_monitors`
	if enabledOnly {
		query += ` WHERE enabled=1`
	}
	query += ` ORDER BY domain`
	rows, err := d.sql.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var monitors []model.MailMonitor
	for rows.Next() {
		m, err := scanMailMonitor(rows)
		if err != nil {
			return nil, err
		}
		monitors = append(monitors, m)
	}
	return monitors, rows.Err()
}

func (d *DB) GetMailMonitor(id int64) (model.MailMonitor, error) {
	row := d.sql.QueryRow(`SELECT id,domain,dkim_selector,check_interval,enabled,last_status,last_score,last_checked_at,created_at,updated_at,registration_status,registration_expires_at,registration_fingerprint FROM mail_monitors WHERE id=?`, id)
	return scanMailMonitor(row)
}

func (d *DB) UpdateMailMonitor(monitor *model.MailMonitor) error {
	monitor.UpdatedAt = time.Now().UTC()
	_, err := d.sql.Exec(`UPDATE mail_monitors SET domain=?,dkim_selector=?,check_interval=?,enabled=?,last_status=CASE WHEN domain<>? THEN '' ELSE last_status END,last_score=CASE WHEN domain<>? THEN 0 ELSE last_score END,last_checked_at=CASE WHEN domain<>? THEN NULL ELSE last_checked_at END,registration_status=CASE WHEN domain<>? THEN '' ELSE registration_status END,registration_expires_at=CASE WHEN domain<>? THEN NULL ELSE registration_expires_at END,registration_fingerprint=CASE WHEN domain<>? THEN '' ELSE registration_fingerprint END,updated_at=? WHERE id=?`,
		monitor.Domain, monitor.DKIMSelector, monitor.CheckInterval, boolInt(monitor.Enabled), monitor.Domain, monitor.Domain, monitor.Domain, monitor.Domain, monitor.Domain, monitor.Domain, monitor.UpdatedAt.Format(time.RFC3339Nano), monitor.ID)
	return err
}

func (d *DB) DeleteMailMonitor(id int64) error {
	_, err := d.sql.Exec(`DELETE FROM mail_monitors WHERE id=?`, id)
	return err
}

func (d *DB) UpdateMailMonitorResult(id int64, status string, score int, checkedAt time.Time, registration model.DomainRegistration) error {
	expires := ""
	if registration.ExpiresAt != nil {
		expires = registration.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := d.sql.Exec(`UPDATE mail_monitors SET last_status=?,last_score=?,last_checked_at=?,updated_at=?,registration_status=?,registration_expires_at=?,registration_fingerprint=? WHERE id=?`,
		status, score, checkedAt.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), registration.Status, expires, registration.Fingerprint, id)
	return err
}

type scanner interface{ Scan(dest ...any) error }

func scanMailMonitor(row scanner) (model.MailMonitor, error) {
	var m model.MailMonitor
	var enabled int
	var lastChecked, created, updated, registrationExpires sql.NullString
	err := row.Scan(&m.ID, &m.Domain, &m.DKIMSelector, &m.CheckInterval, &enabled, &m.LastStatus, &m.LastScore, &lastChecked, &created, &updated, &m.RegistrationStatus, &registrationExpires, &m.RegistrationFingerprint)
	if err != nil {
		return m, err
	}
	m.Enabled = enabled != 0
	if registrationExpires.Valid {
		value, err := time.Parse(time.RFC3339Nano, registrationExpires.String)
		if err == nil {
			m.RegistrationExpiresAt = &value
		}
	}
	var parseErr error
	if lastChecked.Valid {
		value, err := time.Parse(time.RFC3339Nano, lastChecked.String)
		if err != nil {
			parseErr = err
		} else {
			m.LastChecked = &value
		}
	}
	if created.Valid {
		m.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created.String)
	}
	if updated.Valid {
		m.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updated.String)
	}
	return m, parseErr
}
