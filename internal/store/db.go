package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	_ "modernc.org/sqlite"

	"snippetbook/internal/model"
)

type DB struct {
	sql *sql.DB
}

func New(dbPath string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	d := &DB{sql: sqlDB}
	if err := d.migrate(); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error { return d.sql.Close() }

func (d *DB) ensureColumn(table, col, decl string) error {
	rows, err := d.sql.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == col {
			return nil
		}
	}
	_, err = d.sql.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + col + ` ` + decl)
	return err
}

func newUUID() string { return uuid.New().String() }

func (d *DB) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			uuid TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL,
			title TEXT NOT NULL,
			language TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			tags TEXT NOT NULL DEFAULT '[]',
			favorite INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			deleted_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS secrets (
			item_id INTEGER PRIMARY KEY,
			ciphertext BLOB NOT NULL,
			nonce BLOB NOT NULL,
			FOREIGN KEY(item_id) REFERENCES items(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_items_kind ON items(kind)`,
		`CREATE INDEX IF NOT EXISTS idx_items_updated ON items(updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_items_uuid ON items(uuid)`,
		`CREATE INDEX IF NOT EXISTS idx_items_deleted ON items(deleted_at)`,
		`CREATE TABLE IF NOT EXISTS services (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			check_type TEXT NOT NULL DEFAULT 'http',
			environment TEXT NOT NULL DEFAULT 'production',
			description TEXT NOT NULL DEFAULT '',
			 tags TEXT NOT NULL DEFAULT '[]',
			enabled INTEGER NOT NULL DEFAULT 1,
			check_interval TEXT NOT NULL DEFAULT '30s',
			repeat_notifications_enabled INTEGER NOT NULL DEFAULT 0,
			repeat_notification_interval TEXT NOT NULL DEFAULT '5m',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS service_checks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			service_id INTEGER NOT NULL,
			status TEXT NOT NULL,
			http_status INTEGER NOT NULL DEFAULT 0,
			response_time_ms INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '',
			checked_at TEXT NOT NULL,
			FOREIGN KEY(service_id) REFERENCES services(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS service_metrics_hourly (
			service_id INTEGER NOT NULL,
			bucket_start TEXT NOT NULL,
			total_checks INTEGER NOT NULL DEFAULT 0,
			online_checks INTEGER NOT NULL DEFAULT 0,
			degraded_checks INTEGER NOT NULL DEFAULT 0,
			offline_checks INTEGER NOT NULL DEFAULT 0,
			response_sum_ms INTEGER NOT NULL DEFAULT 0,
			response_min_ms INTEGER NOT NULL DEFAULT 0,
			response_max_ms INTEGER NOT NULL DEFAULT 0,
			dns_sum_ms INTEGER NOT NULL DEFAULT 0,
			connect_sum_ms INTEGER NOT NULL DEFAULT 0,
			tls_sum_ms INTEGER NOT NULL DEFAULT 0,
			ttfb_sum_ms INTEGER NOT NULL DEFAULT 0,
			page_size_sum_bytes INTEGER NOT NULL DEFAULT 0,
			last_status TEXT NOT NULL DEFAULT '',
			last_checked_at TEXT NOT NULL,
			PRIMARY KEY(service_id, bucket_start),
			FOREIGN KEY(service_id) REFERENCES services(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS service_metrics_daily (
			service_id INTEGER NOT NULL,
			bucket_date TEXT NOT NULL,
			total_checks INTEGER NOT NULL DEFAULT 0,
			online_checks INTEGER NOT NULL DEFAULT 0,
			degraded_checks INTEGER NOT NULL DEFAULT 0,
			offline_checks INTEGER NOT NULL DEFAULT 0,
			response_sum_ms INTEGER NOT NULL DEFAULT 0,
			response_min_ms INTEGER NOT NULL DEFAULT 0,
			response_max_ms INTEGER NOT NULL DEFAULT 0,
			dns_sum_ms INTEGER NOT NULL DEFAULT 0,
			connect_sum_ms INTEGER NOT NULL DEFAULT 0,
			tls_sum_ms INTEGER NOT NULL DEFAULT 0,
			ttfb_sum_ms INTEGER NOT NULL DEFAULT 0,
			page_size_sum_bytes INTEGER NOT NULL DEFAULT 0,
			last_status TEXT NOT NULL DEFAULT '',
			last_checked_at TEXT NOT NULL,
			PRIMARY KEY(service_id, bucket_date),
			FOREIGN KEY(service_id) REFERENCES services(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_services_enabled ON services(enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_service_checks_service ON service_checks(service_id, checked_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_service_metrics_hourly_service ON service_metrics_hourly(service_id, bucket_start DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_service_metrics_daily_service ON service_metrics_daily(service_id, bucket_date DESC)`,
		`CREATE TABLE IF NOT EXISTS app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS runbooks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			service_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(service_id) REFERENCES services(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS reminders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			service_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			due_at TEXT NOT NULL,
			completed INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(service_id) REFERENCES services(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,
			title TEXT NOT NULL,
			message TEXT NOT NULL,
			service_id INTEGER NOT NULL DEFAULT 0,
			read INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS service_incidents (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			service_id INTEGER NOT NULL,
			status TEXT NOT NULL,
			problem TEXT NOT NULL,
			started_at TEXT NOT NULL,
			resolved_at TEXT,
			duration_seconds INTEGER NOT NULL DEFAULT 0,
			last_notified_at TEXT,
			FOREIGN KEY(service_id) REFERENCES services(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS mail_checks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain TEXT NOT NULL,
			dkim_selector TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			score INTEGER NOT NULL DEFAULT 0,
			result_json TEXT NOT NULL,
			checked_at TEXT NOT NULL,
			ai_report TEXT NOT NULL DEFAULT '',
			ai_error TEXT NOT NULL DEFAULT '',
			ai_model TEXT NOT NULL DEFAULT '',
			ai_updated_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS mail_monitors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain TEXT NOT NULL UNIQUE,
			dkim_selector TEXT NOT NULL DEFAULT '',
			check_interval TEXT NOT NULL DEFAULT '1h',
			enabled INTEGER NOT NULL DEFAULT 1,
			last_status TEXT NOT NULL DEFAULT '',
			last_score INTEGER NOT NULL DEFAULT 0,
			last_checked_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_reminders_due ON reminders(completed, due_at)`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_created ON notifications(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_incidents_service ON service_incidents(service_id, started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_mail_checks_domain ON mail_checks(domain, checked_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_mail_monitors_enabled ON mail_monitors(enabled)`,
	}
	for _, s := range stmts {
		if _, err := d.sql.Exec(s); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	// migrate older schemas
	if err := d.ensureColumn("items", "uuid", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := d.ensureColumn("items", "deleted_at", "TEXT"); err != nil {
		return err
	}
	if err := d.ensureColumn("services", "check_interval", "TEXT NOT NULL DEFAULT '30s'"); err != nil {
		return err
	}
	if err := d.ensureColumn("services", "check_type", "TEXT NOT NULL DEFAULT 'http'"); err != nil {
		return err
	}
	if err := d.ensureColumn("services", "repeat_notifications_enabled", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := d.ensureColumn("services", "repeat_notification_interval", "TEXT NOT NULL DEFAULT '5m'"); err != nil {
		return err
	}
	if err := d.ensureColumn("service_incidents", "last_notified_at", "TEXT"); err != nil {
		return err
	}
	if err := d.ensureColumn("mail_checks", "ai_report", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := d.ensureColumn("mail_checks", "ai_error", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := d.ensureColumn("mail_checks", "ai_model", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := d.ensureColumn("mail_checks", "ai_updated_at", "TEXT"); err != nil {
		return err
	}
	if err := d.ensureColumn("service_checks", "diagnostics_json", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		return err
	}
	for _, column := range []struct{ name, decl string }{
		{"registration_status", "TEXT NOT NULL DEFAULT ''"},
		{"registration_expires_at", "TEXT"},
		{"registration_fingerprint", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := d.ensureColumn("mail_monitors", column.name, column.decl); err != nil {
			return err
		}
	}
	for _, column := range []struct{ name, decl string }{
		{"tls_status", "TEXT NOT NULL DEFAULT ''"},
		{"tls_issuer", "TEXT NOT NULL DEFAULT ''"},
		{"tls_hostname", "TEXT NOT NULL DEFAULT ''"},
		{"tls_expires_at", "TEXT NOT NULL DEFAULT ''"},
		{"tls_days_remaining", "INTEGER NOT NULL DEFAULT 0"},
		{"tls_error", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := d.ensureColumn("service_checks", column.name, column.decl); err != nil {
			return err
		}
	}
	if _, err := d.sql.Exec(`INSERT OR IGNORE INTO service_metrics_hourly(service_id,bucket_start,total_checks,online_checks,degraded_checks,offline_checks,response_sum_ms,response_min_ms,response_max_ms,last_status,last_checked_at) SELECT service_id,substr(checked_at,1,13)||':00:00Z',COUNT(*),SUM(CASE WHEN status='online' THEN 1 ELSE 0 END),SUM(CASE WHEN status IN ('degraded','tls_error') THEN 1 ELSE 0 END),SUM(CASE WHEN status NOT IN ('online','degraded','tls_error') THEN 1 ELSE 0 END),COALESCE(SUM(response_time_ms),0),COALESCE(MIN(response_time_ms),0),COALESCE(MAX(response_time_ms),0),'',MAX(checked_at) FROM service_checks GROUP BY service_id,substr(checked_at,1,13)`); err != nil {
		return err
	}
	if _, err := d.sql.Exec(`INSERT OR IGNORE INTO service_metrics_daily(service_id,bucket_date,total_checks,online_checks,degraded_checks,offline_checks,response_sum_ms,response_min_ms,response_max_ms,last_status,last_checked_at) SELECT service_id,substr(checked_at,1,10),COUNT(*),SUM(CASE WHEN status='online' THEN 1 ELSE 0 END),SUM(CASE WHEN status IN ('degraded','tls_error') THEN 1 ELSE 0 END),SUM(CASE WHEN status NOT IN ('online','degraded','tls_error') THEN 1 ELSE 0 END),COALESCE(SUM(response_time_ms),0),COALESCE(MIN(response_time_ms),0),COALESCE(MAX(response_time_ms),0),'',MAX(checked_at) FROM service_checks GROUP BY service_id,substr(checked_at,1,10)`); err != nil {
		return err
	}
	// backfill uuids
	rows, err := d.sql.Query(`SELECT id FROM items WHERE uuid='' OR uuid IS NULL`)
	if err == nil {
		defer rows.Close()
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err == nil {
				ids = append(ids, id)
			}
		}
		for _, id := range ids {
			u := newUUID()
			_, _ = d.sql.Exec(`UPDATE items SET uuid=? WHERE id=?`, u, id)
		}
	}
	// FTS5 (best-effort; falls back to LIKE search if unavailable)
	_, _ = d.sql.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS items_fts USING fts5(title, content, tags, content='items', content_rowid='id')`)
	return nil
}

// MetaStore implementation
func (d *DB) MetaGet(key string) (string, error) {
	var v string
	err := d.sql.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (d *DB) MetaSet(key, value string) error {
	_, err := d.sql.Exec(`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (d *DB) now() string { return time.Now().UTC().Format(time.RFC3339) }

func (d *DB) tagsToJSON(tags []string) string {
	if tags == nil {
		tags = []string{}
	}
	b, _ := json.Marshal(tags)
	return string(b)
}

func (d *DB) scanItem(row *sql.Row) (model.Item, error) {
	var it model.Item
	var tags string
	var fav int
	var created, updated string
	var uuidVal string
	var deleted sql.NullString
	err := row.Scan(&it.ID, &uuidVal, &it.Kind, &it.Title, &it.Language, &it.Content, &tags, &fav, &created, &updated, &deleted)
	if err != nil {
		return it, err
	}
	it.UUID = uuidVal
	_ = json.Unmarshal([]byte(tags), &it.Tags)
	if it.Tags == nil {
		it.Tags = []string{}
	}
	it.Favorite = fav == 1
	ct, _ := time.Parse(time.RFC3339, created)
	ut, _ := time.Parse(time.RFC3339, updated)
	it.CreatedAt = ct
	it.UpdatedAt = ut
	if deleted.Valid {
		t, _ := time.Parse(time.RFC3339, deleted.String)
		it.DeletedAt = &t
	}
	return it, nil
}

func (d *DB) scanItemRows(rows *sql.Rows) (model.Item, error) {
	var it model.Item
	var tags string
	var fav int
	var created, updated string
	var uuidVal string
	var deleted sql.NullString
	err := rows.Scan(&it.ID, &uuidVal, &it.Kind, &it.Title, &it.Language, &it.Content, &tags, &fav, &created, &updated, &deleted)
	if err != nil {
		return it, err
	}
	it.UUID = uuidVal
	_ = json.Unmarshal([]byte(tags), &it.Tags)
	if it.Tags == nil {
		it.Tags = []string{}
	}
	it.Favorite = fav == 1
	ct, _ := time.Parse(time.RFC3339, created)
	ut, _ := time.Parse(time.RFC3339, updated)
	it.CreatedAt = ct
	it.UpdatedAt = ut
	if deleted.Valid {
		t, _ := time.Parse(time.RFC3339, deleted.String)
		it.DeletedAt = &t
	}
	return it, nil
}
