package store

import (
	"database/sql"
	"encoding/json"
	"strconv"

	"snippetbook/internal/model"
)

const notificationSettingsKey = "notification_settings"
const aiSettingsKey = "ai_settings"

const (
	settingCheckTimeout = "check_timeout"
	settingCheckHistory = "check_history_limit"
)

var defaultSettings = model.Settings{CheckTimeout: "10s", CheckHistoryLimit: 500}

// EnsureSettings seeds only missing values, preserving changes made in the admin UI.
func (d *DB) EnsureSettings() error {
	defaults := map[string]string{
		settingCheckTimeout:     defaultSettings.CheckTimeout,
		settingCheckHistory:     strconv.Itoa(defaultSettings.CheckHistoryLimit),
		notificationSettingsKey: `{"smtp_port":587}`,
		aiSettingsKey:           `{"provider":"openai-compatible","base_url":"https://api.openai.com/v1","model":"","max_tokens":1800}`,
	}
	for key, value := range defaults {
		if _, err := d.sql.Exec(`INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO NOTHING`, key, value, d.now()); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) AISettings() (model.AISettings, error) {
	settings := model.AISettings{Provider: "openai-compatible", BaseURL: "https://api.openai.com/v1", MaxTokens: 1800}
	var raw string
	err := d.sql.QueryRow(`SELECT value FROM app_settings WHERE key=?`, aiSettingsKey).Scan(&raw)
	if err == sql.ErrNoRows {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return settings, err
	}
	if settings.Provider == "" {
		settings.Provider = "openai-compatible"
	}
	if settings.BaseURL == "" {
		settings.BaseURL = "https://api.openai.com/v1"
	}
	if settings.MaxTokens <= 0 {
		settings.MaxTokens = 1800
	}
	return settings, nil
}

func (d *DB) UpdateAISettings(settings model.AISettings) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = d.sql.Exec(`INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, aiSettingsKey, string(raw), d.now())
	return err
}

func (d *DB) Settings() (model.Settings, error) {
	settings := defaultSettings
	rows, err := d.sql.Query(`SELECT key,value FROM app_settings WHERE key IN (?,?)`, settingCheckTimeout, settingCheckHistory)
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return settings, err
		}
		switch key {
		case settingCheckTimeout:
			settings.CheckTimeout = value
		case settingCheckHistory:
			if parsed, err := strconv.Atoi(value); err == nil {
				settings.CheckHistoryLimit = parsed
			}
		}
	}
	return settings, rows.Err()
}

func (d *DB) UpdateSettings(settings model.Settings) error {
	values := map[string]string{
		settingCheckTimeout: settings.CheckTimeout,
		settingCheckHistory: strconv.Itoa(settings.CheckHistoryLimit),
	}
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	for key, value := range values {
		if _, err := tx.Exec(`INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, key, value, d.now()); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) NotificationSettings() (model.NotificationSettings, error) {
	var raw string
	err := d.sql.QueryRow(`SELECT value FROM app_settings WHERE key=?`, notificationSettingsKey).Scan(&raw)
	if err == sql.ErrNoRows {
		return model.NotificationSettings{SMTPPort: 587}, nil
	}
	if err != nil {
		return model.NotificationSettings{}, err
	}
	var settings model.NotificationSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return settings, err
	}
	if settings.SMTPPort == 0 {
		settings.SMTPPort = 587
	}
	return settings, nil
}

func (d *DB) UpdateNotificationSettings(settings model.NotificationSettings) error {
	if settings.SMTPPort == 0 {
		settings.SMTPPort = 587
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = d.sql.Exec(`INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, notificationSettingsKey, string(raw), d.now())
	return err
}
