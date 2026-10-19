package store

import (
	"database/sql"
	"time"

	"snippetbook/internal/model"
)

func (d *DB) ActiveIncident(serviceID int64) (*model.Incident, error) {
	var incident model.Incident
	var started string
	var resolved, notified sql.NullString
	err := d.sql.QueryRow(`SELECT id,service_id,status,problem,started_at,resolved_at,duration_seconds,last_notified_at FROM service_incidents WHERE service_id=? AND status='active' ORDER BY started_at DESC,id DESC LIMIT 1`, serviceID).Scan(&incident.ID, &incident.ServiceID, &incident.Status, &incident.Problem, &started, &resolved, &incident.DurationSeconds, &notified)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	incident.StartedAt, _ = time.Parse(time.RFC3339, started)
	if notified.Valid {
		value, parseErr := time.Parse(time.RFC3339, notified.String)
		if parseErr == nil {
			incident.LastNotifiedAt = &value
		}
	}
	return &incident, nil
}

func (d *DB) CreateIncident(incident *model.Incident) error {
	result, err := d.sql.Exec(`INSERT INTO service_incidents(service_id,status,problem,started_at,last_notified_at) VALUES(?,?,?,?,?)`, incident.ServiceID, "active", incident.Problem, incident.StartedAt.UTC().Format(time.RFC3339), incident.StartedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	incident.ID, err = result.LastInsertId()
	incident.Status = "active"
	return err
}

func (d *DB) MarkIncidentNotified(incidentID int64, notifiedAt time.Time) error {
	_, err := d.sql.Exec(`UPDATE service_incidents SET last_notified_at=? WHERE id=?`, notifiedAt.UTC().Format(time.RFC3339), incidentID)
	return err
}

func (d *DB) ResolveIncident(incident *model.Incident, resolvedAt time.Time) error {
	duration := int64(resolvedAt.Sub(incident.StartedAt).Seconds())
	if duration < 0 {
		duration = 0
	}
	_, err := d.sql.Exec(`UPDATE service_incidents SET status='resolved',resolved_at=?,duration_seconds=? WHERE id=?`, resolvedAt.UTC().Format(time.RFC3339), duration, incident.ID)
	if err != nil {
		return err
	}
	incident.Status = "resolved"
	incident.DurationSeconds = duration
	resolved := resolvedAt.UTC()
	incident.ResolvedAt = &resolved
	return nil
}

func (d *DB) ListIncidents(serviceID int64, limit int) ([]model.Incident, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := d.sql.Query(`SELECT id,service_id,status,problem,started_at,resolved_at,duration_seconds,last_notified_at FROM service_incidents WHERE service_id=? ORDER BY started_at DESC,id DESC LIMIT ?`, serviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.Incident{}
	for rows.Next() {
		var incident model.Incident
		var started string
		var resolved, notified sql.NullString
		if err := rows.Scan(&incident.ID, &incident.ServiceID, &incident.Status, &incident.Problem, &started, &resolved, &incident.DurationSeconds, &notified); err != nil {
			return nil, err
		}
		incident.StartedAt, _ = time.Parse(time.RFC3339, started)
		if resolved.Valid {
			value, parseErr := time.Parse(time.RFC3339, resolved.String)
			if parseErr == nil {
				incident.ResolvedAt = &value
			}
		}
		if notified.Valid {
			value, parseErr := time.Parse(time.RFC3339, notified.String)
			if parseErr == nil {
				incident.LastNotifiedAt = &value
			}
		}
		result = append(result, incident)
	}
	return result, rows.Err()
}
