package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"snippetbook/internal/model"
)

func (d *DB) CreateService(service *model.Service) error {
	now := d.now()
	if service.CheckType == "" {
		service.CheckType = "http"
	}
	if service.Tags == nil {
		service.Tags = []string{}
	}
	if service.CheckInterval == "" {
		service.CheckInterval = "30s"
	}
	if service.RepeatNotificationInterval == "" {
		service.RepeatNotificationInterval = "5m"
	}
	result, err := d.sql.Exec(`INSERT INTO services(name,url,check_type,environment,description,tags,enabled,check_interval,repeat_notifications_enabled,repeat_notification_interval,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		service.Name, service.URL, service.CheckType, service.Environment, service.Description, d.tagsToJSON(service.Tags), boolToInt(service.Enabled), service.CheckInterval, boolToInt(service.RepeatNotificationsEnabled), service.RepeatNotificationInterval, now, now)
	if err != nil {
		return err
	}
	service.ID, err = result.LastInsertId()
	if err != nil {
		return err
	}
	service.CreatedAt, _ = parseTime(now)
	service.UpdatedAt = service.CreatedAt
	return nil
}

func (d *DB) GetService(id int64) (model.Service, error) {
	row := d.sql.QueryRow(`SELECT id,name,url,check_type,environment,description,tags,enabled,check_interval,repeat_notifications_enabled,repeat_notification_interval,created_at,updated_at FROM services WHERE id=?`, id)
	service, err := scanService(row)
	if err != nil {
		return service, err
	}
	service.LastCheck, _ = d.LastCheck(id)
	return service, nil
}

func (d *DB) ListServices() ([]model.Service, error) {
	rows, err := d.sql.Query(`SELECT id,name,url,check_type,environment,description,tags,enabled,check_interval,repeat_notifications_enabled,repeat_notification_interval,created_at,updated_at FROM services ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	services := []model.Service{}
	for rows.Next() {
		service, err := scanServiceRows(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		services = append(services, service)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range services {
		services[index].LastCheck, _ = d.LastCheck(services[index].ID)
	}
	return services, nil
}

func (d *DB) ListEnabledServices() ([]model.Service, error) {
	services, err := d.ListServices()
	if err != nil {
		return nil, err
	}
	enabled := make([]model.Service, 0, len(services))
	for _, service := range services {
		if service.Enabled {
			enabled = append(enabled, service)
		}
	}
	return enabled, nil
}

func (d *DB) UpdateService(service *model.Service) error {
	if service.CheckType == "" {
		service.CheckType = "http"
	}
	if service.Tags == nil {
		service.Tags = []string{}
	}
	if service.CheckInterval == "" {
		service.CheckInterval = "30s"
	}
	if service.RepeatNotificationInterval == "" {
		service.RepeatNotificationInterval = "5m"
	}
	now := d.now()
	_, err := d.sql.Exec(`UPDATE services SET name=?,url=?,check_type=?,environment=?,description=?,tags=?,enabled=?,check_interval=?,repeat_notifications_enabled=?,repeat_notification_interval=?,updated_at=? WHERE id=?`,
		service.Name, service.URL, service.CheckType, service.Environment, service.Description, d.tagsToJSON(service.Tags), boolToInt(service.Enabled), service.CheckInterval, boolToInt(service.RepeatNotificationsEnabled), service.RepeatNotificationInterval, now, service.ID)
	if err == nil {
		service.UpdatedAt, _ = parseTime(now)
	}
	return err
}

func (d *DB) DeleteService(id int64) error {
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM service_locations WHERE service_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM services WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) AddCheck(check *model.Check) error {
	var tls model.TLSInfo
	if check.TLS != nil {
		tls = *check.TLS
	}
	payload := struct {
		Diagnostics map[string]any      `json:"diagnostics,omitempty"`
		Metrics     *model.CheckMetrics `json:"metrics,omitempty"`
	}{Diagnostics: check.Diagnostics, Metrics: check.Metrics}
	diagnostics, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	result, err := d.sql.Exec(`INSERT INTO service_checks(service_id,status,http_status,response_time_ms,error,checked_at,tls_status,tls_issuer,tls_hostname,tls_expires_at,tls_days_remaining,tls_error,diagnostics_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		check.ServiceID, check.Status, check.HTTPStatus, check.ResponseTimeMS, check.Error, check.CheckedAt.UTC().Format(time.RFC3339), tls.Status, tls.Issuer, tls.Hostname, formatOptionalTime(tls.ExpiresAt), tls.DaysRemaining, tls.Error, string(diagnostics))
	if err != nil {
		return err
	}
	check.ID, err = result.LastInsertId()
	if err != nil {
		return err
	}
	return d.updateMetricAggregates(check)
}

func (d *DB) updateMetricAggregates(check *model.Check) error {
	metrics := check.Metrics
	if metrics == nil {
		metrics = &model.CheckMetrics{}
	}
	checkedAt := check.CheckedAt.UTC()
	args := metricAggregateArgs(check, metrics)
	queries := []struct {
		table  string
		bucket string
	}{
		{"service_metrics_hourly", checkedAt.Truncate(time.Hour).Format(time.RFC3339)},
		{"service_metrics_daily", checkedAt.Format("2006-01-02")},
	}
	for _, query := range queries {
		if _, err := d.sql.Exec(`INSERT INTO `+query.table+`(service_id,`+bucketColumn(query.table)+`,total_checks,online_checks,degraded_checks,offline_checks,response_sum_ms,response_min_ms,response_max_ms,dns_sum_ms,connect_sum_ms,tls_sum_ms,ttfb_sum_ms,page_size_sum_bytes,last_status,last_checked_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(service_id,`+bucketColumn(query.table)+`) DO UPDATE SET total_checks=total_checks+1,online_checks=online_checks+excluded.online_checks,degraded_checks=degraded_checks+excluded.degraded_checks,offline_checks=offline_checks+excluded.offline_checks,response_sum_ms=response_sum_ms+excluded.response_sum_ms,response_min_ms=CASE WHEN response_min_ms=0 OR (excluded.response_min_ms>0 AND excluded.response_min_ms<response_min_ms) THEN excluded.response_min_ms ELSE response_min_ms END,response_max_ms=MAX(response_max_ms,excluded.response_max_ms),dns_sum_ms=dns_sum_ms+excluded.dns_sum_ms,connect_sum_ms=connect_sum_ms+excluded.connect_sum_ms,tls_sum_ms=tls_sum_ms+excluded.tls_sum_ms,ttfb_sum_ms=ttfb_sum_ms+excluded.ttfb_sum_ms,page_size_sum_bytes=page_size_sum_bytes+excluded.page_size_sum_bytes,last_status=excluded.last_status,last_checked_at=excluded.last_checked_at`, append([]any{check.ServiceID, query.bucket}, args...)...); err != nil {
			return err
		}
	}
	return nil
}

func bucketColumn(table string) string {
	if table == "service_metrics_daily" {
		return "bucket_date"
	}
	return "bucket_start"
}

func metricAggregateArgs(check *model.Check, metrics *model.CheckMetrics) []any {
	online, degraded, offline := 0, 0, 0
	switch check.Status {
	case "online":
		online = 1
	case "degraded", "tls_error":
		degraded = 1
	default:
		offline = 1
	}
	return []any{1, online, degraded, offline, check.ResponseTimeMS, check.ResponseTimeMS, check.ResponseTimeMS, metrics.DNSMS, metrics.ConnectMS, metrics.TLSMS, metrics.TTFBMS, metrics.PageSizeBytes, check.Status, check.CheckedAt.UTC().Format(time.RFC3339)}
}

func (d *DB) LastCheck(serviceID int64) (*model.Check, error) {
	row := d.sql.QueryRow(`SELECT id,service_id,status,http_status,response_time_ms,error,checked_at,tls_status,tls_issuer,tls_hostname,tls_expires_at,tls_days_remaining,tls_error,diagnostics_json FROM service_checks WHERE service_id=? ORDER BY checked_at DESC,id DESC LIMIT 1`, serviceID)
	check, err := scanCheck(row)
	if err != nil {
		return nil, err
	}
	return &check, nil
}

func (d *DB) CheckHistory(serviceID int64, limit int) ([]model.Check, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := d.sql.Query(`SELECT id,service_id,status,http_status,response_time_ms,error,checked_at,tls_status,tls_issuer,tls_hostname,tls_expires_at,tls_days_remaining,tls_error,diagnostics_json FROM service_checks WHERE service_id=? ORDER BY checked_at DESC,id DESC LIMIT ?`, serviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	checks := []model.Check{}
	for rows.Next() {
		check, err := scanCheckRows(rows)
		if err != nil {
			return nil, err
		}
		checks = append(checks, check)
	}
	return checks, rows.Err()
}

func (d *DB) PurgeChecks(serviceID int64, keep int) error {
	if keep <= 0 {
		keep = 500
	}
	_, err := d.sql.Exec(`DELETE FROM service_checks WHERE service_id=? AND id NOT IN (SELECT id FROM service_checks WHERE service_id=? ORDER BY checked_at DESC,id DESC LIMIT ?)`, serviceID, serviceID, keep)
	return err
}

func (d *DB) ClearChecksBefore(serviceID int64, before time.Time) error {
	_, err := d.sql.Exec(`DELETE FROM service_checks WHERE service_id=? AND checked_at<?`, serviceID, before.UTC().Format(time.RFC3339))
	return err
}

func (d *DB) ServiceMetrics(serviceID int64, period string) (model.ServiceMetrics, error) {
	if period != "6h" && period != "24h" && period != "7d" && period != "30d" {
		period = "24h"
	}
	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour)
	table, bucketColumn := "service_metrics_hourly", "bucket_start"
	source := "hourly"
	switch period {
	case "6h":
		from = now.Add(-6 * time.Hour)
	case "7d":
		from = now.Add(-7 * 24 * time.Hour)
	case "30d":
		from = now.Add(-30 * 24 * time.Hour)
		table, bucketColumn, source = "service_metrics_daily", "bucket_date", "daily"
	}
	result := model.ServiceMetrics{Period: period, Source: source, From: from, To: now, Buckets: []model.MetricBucket{}}
	fromValue := from.Format(time.RFC3339)
	if table == "service_metrics_daily" {
		fromValue = from.Format("2006-01-02")
	}
	query := `SELECT ` + bucketColumn + `,total_checks,online_checks,degraded_checks,offline_checks,response_sum_ms,response_min_ms,response_max_ms,dns_sum_ms,connect_sum_ms,tls_sum_ms,ttfb_sum_ms,page_size_sum_bytes,last_status,last_checked_at FROM ` + table + ` WHERE service_id=? AND ` + bucketColumn + `>=? ORDER BY ` + bucketColumn
	rows, err := d.sql.Query(query, serviceID, fromValue)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var bucket model.MetricBucket
		if err := rows.Scan(&bucket.Bucket, &bucket.TotalChecks, &bucket.OnlineChecks, &bucket.DegradedChecks, &bucket.OfflineChecks, &bucket.ResponseSumMS, &bucket.ResponseMinMS, &bucket.ResponseMaxMS, &bucket.DNSsumMS, &bucket.ConnectSumMS, &bucket.TLSSumMS, &bucket.TTFBSumMS, &bucket.PageSizeSumBytes, &bucket.LastStatus, &bucket.LastCheckedAt); err != nil {
			return result, err
		}
		result.TotalChecks += bucket.TotalChecks
		result.OnlineChecks += bucket.OnlineChecks
		result.Degraded += bucket.DegradedChecks
		result.Offline += bucket.OfflineChecks
		result.Buckets = append(result.Buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if result.TotalChecks > 0 {
		result.Uptime = float64(result.OnlineChecks) / float64(result.TotalChecks) * 100
	}
	return result, nil
}

func (d *DB) Dashboard() (model.Dashboard, error) {
	services, err := d.ListServices()
	if err != nil {
		return model.Dashboard{}, err
	}
	result := model.Dashboard{Total: len(services), RecentErrors: []model.Check{}, ResponseTimes: []model.ResponseTimePoint{}}
	for _, service := range services {
		if service.LastCheck == nil || service.LastCheck.Status == "offline" {
			result.Offline++
		} else if service.LastCheck.Status != "online" {
			result.Degraded++
		} else {
			result.Online++
		}
	}
	rows, err := d.sql.Query(`SELECT c.id,c.service_id,c.status,c.http_status,c.response_time_ms,c.error,c.checked_at,c.tls_status,c.tls_issuer,c.tls_hostname,c.tls_expires_at,c.tls_days_remaining,c.tls_error,c.diagnostics_json FROM service_checks c WHERE c.status <> 'online' OR c.tls_status IN ('error','expiring') ORDER BY c.checked_at DESC,c.id DESC LIMIT 10`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		check, scanErr := scanCheckRows(rows)
		if scanErr != nil {
			_ = rows.Close()
			return result, scanErr
		}
		result.RecentErrors = append(result.RecentErrors, check)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return result, err
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	rows, err = d.sql.Query(`SELECT c.service_id,s.name,c.response_time_ms,c.checked_at FROM service_checks c JOIN services s ON s.id=c.service_id ORDER BY c.checked_at DESC,c.id DESC LIMIT 30`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var point model.ResponseTimePoint
		var checked string
		if err := rows.Scan(&point.ServiceID, &point.ServiceName, &point.Milliseconds, &checked); err != nil {
			_ = rows.Close()
			return result, err
		}
		point.CheckedAt, _ = time.Parse(time.RFC3339, checked)
		result.ResponseTimes = append(result.ResponseTimes, point)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return result, err
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	return result, nil
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func scanCheck(row *sql.Row) (model.Check, error) {
	var check model.Check
	var checked, tlsStatus, issuer, hostname, expiresAt, tlsError, diagnosticsJSON string
	var days int
	if err := row.Scan(&check.ID, &check.ServiceID, &check.Status, &check.HTTPStatus, &check.ResponseTimeMS, &check.Error, &checked, &tlsStatus, &issuer, &hostname, &expiresAt, &days, &tlsError, &diagnosticsJSON); err != nil {
		return check, err
	}
	check.CheckedAt, _ = time.Parse(time.RFC3339, checked)
	check.TLS = makeTLS(tlsStatus, issuer, hostname, expiresAt, days, tlsError)
	decodeCheckPayload(diagnosticsJSON, &check)
	return check, nil
}

func scanCheckRows(rows *sql.Rows) (model.Check, error) {
	var check model.Check
	var checked, tlsStatus, issuer, hostname, expiresAt, tlsError, diagnosticsJSON string
	var days int
	if err := rows.Scan(&check.ID, &check.ServiceID, &check.Status, &check.HTTPStatus, &check.ResponseTimeMS, &check.Error, &checked, &tlsStatus, &issuer, &hostname, &expiresAt, &days, &tlsError, &diagnosticsJSON); err != nil {
		return check, err
	}
	check.CheckedAt, _ = time.Parse(time.RFC3339, checked)
	check.TLS = makeTLS(tlsStatus, issuer, hostname, expiresAt, days, tlsError)
	decodeCheckPayload(diagnosticsJSON, &check)
	return check, nil
}

func decodeCheckPayload(payload string, check *model.Check) {
	var decoded struct {
		Diagnostics map[string]any      `json:"diagnostics"`
		Metrics     *model.CheckMetrics `json:"metrics"`
	}
	if err := json.Unmarshal([]byte(payload), &decoded); err == nil && (decoded.Diagnostics != nil || decoded.Metrics != nil) {
		check.Diagnostics = decoded.Diagnostics
		check.Metrics = decoded.Metrics
		return
	}
	_ = json.Unmarshal([]byte(payload), &check.Diagnostics)
}

func makeTLS(status, issuer, hostname, expiresAt string, days int, tlsError string) *model.TLSInfo {
	if status == "" && issuer == "" && hostname == "" && expiresAt == "" && tlsError == "" {
		return nil
	}
	tls := &model.TLSInfo{Status: status, Issuer: issuer, Hostname: hostname, DaysRemaining: days, Error: tlsError}
	if expiresAt != "" {
		tls.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	}
	return tls
}

func scanService(row *sql.Row) (model.Service, error) {
	var service model.Service
	var tags, created, updated string
	var enabled int
	var repeatEnabled int
	err := row.Scan(&service.ID, &service.Name, &service.URL, &service.CheckType, &service.Environment, &service.Description, &tags, &enabled, &service.CheckInterval, &repeatEnabled, &service.RepeatNotificationInterval, &created, &updated)
	service.RepeatNotificationsEnabled = repeatEnabled == 1
	if err != nil {
		return service, err
	}
	return fillService(service, tags, enabled, created, updated), nil
}

func scanServiceRows(rows *sql.Rows) (model.Service, error) {
	var service model.Service
	var tags, created, updated string
	var enabled int
	var repeatEnabled int
	err := rows.Scan(&service.ID, &service.Name, &service.URL, &service.CheckType, &service.Environment, &service.Description, &tags, &enabled, &service.CheckInterval, &repeatEnabled, &service.RepeatNotificationInterval, &created, &updated)
	service.RepeatNotificationsEnabled = repeatEnabled == 1
	if err != nil {
		return service, err
	}
	return fillService(service, tags, enabled, created, updated), nil
}

func fillService(service model.Service, tags string, enabled int, created, updated string) model.Service {
	_ = json.Unmarshal([]byte(tags), &service.Tags)
	if service.Tags == nil {
		service.Tags = []string{}
	}
	service.Enabled = enabled == 1
	service.CreatedAt, _ = time.Parse(time.RFC3339, created)
	service.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return service
}
