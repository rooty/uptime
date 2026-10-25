package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"snippetbook/internal/model"
)

func (d *DB) IPGeolocations() (map[string]model.IPGeolocation, error) {
	rows, err := d.sql.Query(`SELECT ip,payload FROM ip_geolocation`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]model.IPGeolocation)
	for rows.Next() {
		var ip, payload string
		if err := rows.Scan(&ip, &payload); err != nil {
			return nil, err
		}
		var location model.IPGeolocation
		if err := json.Unmarshal([]byte(payload), &location); err != nil {
			return nil, err
		}
		result[ip] = location
	}
	return result, rows.Err()
}

func (d *DB) SaveIPGeolocation(location model.IPGeolocation) error {
	payload, err := json.Marshal(location)
	if err != nil {
		return err
	}
	_, err = d.sql.Exec(`INSERT INTO ip_geolocation(ip,payload) VALUES(?,?) ON CONFLICT(ip) DO UPDATE SET payload=excluded.payload`, location.IP, string(payload))
	return err
}

func (d *DB) ServiceLocations() (map[int64]model.ServiceLocation, error) {
	rows, err := d.sql.Query(`SELECT service_id,payload FROM service_locations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64]model.ServiceLocation)
	for rows.Next() {
		var id int64
		var payload string
		if err := rows.Scan(&id, &payload); err != nil {
			return nil, err
		}
		var location model.ServiceLocation
		if err := json.Unmarshal([]byte(payload), &location); err != nil {
			return nil, err
		}
		result[id] = location
	}
	return result, rows.Err()
}

func (d *DB) SaveServiceLocation(location model.ServiceLocation) error {
	payload, err := json.Marshal(location)
	if err != nil {
		return err
	}
	// Do not resurrect a binding when a service is deleted during DNS lookup.
	_, err = d.sql.Exec(`INSERT INTO service_locations(service_id,payload) SELECT id,? FROM services WHERE id=? ON CONFLICT(service_id) DO UPDATE SET payload=excluded.payload`, string(payload), location.ServiceID)
	return err
}

func (d *DB) GeoProviderNextAttempt() (time.Time, error) {
	var value string
	err := d.sql.QueryRow(`SELECT value FROM geo_state WHERE key='next_attempt'`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, value)
}

func (d *DB) SetGeoProviderNextAttempt(next time.Time) error {
	_, err := d.sql.Exec(`INSERT INTO geo_state(key,value) VALUES('next_attempt',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, next.UTC().Format(time.RFC3339Nano))
	return err
}
