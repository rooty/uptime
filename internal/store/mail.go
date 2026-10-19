package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"snippetbook/internal/model"
)

func (d *DB) AddMailCheck(check *model.MailCheck) error {
	if check.CheckedAt.IsZero() {
		check.CheckedAt = time.Now().UTC()
	}
	result, err := json.Marshal(check.Result)
	if err != nil {
		return err
	}
	row, err := d.sql.Exec(`INSERT INTO mail_checks(domain,dkim_selector,status,score,result_json,checked_at,ai_report,ai_error,ai_model,ai_updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		check.Domain, check.DKIMSelector, check.Status, check.Score, string(result), check.CheckedAt.Format(time.RFC3339Nano), check.AIReport, check.AIError, check.AIModel, formatMailOptionalTime(check.AIUpdatedAt))
	if err != nil {
		return err
	}
	check.ID, err = row.LastInsertId()
	return err
}

func (d *DB) PurgeMailChecks(domain string, limit int) error {
	if limit <= 0 {
		limit = 500
	}
	_, err := d.sql.Exec(`DELETE FROM mail_checks WHERE domain=? AND id NOT IN (SELECT id FROM mail_checks WHERE domain=? ORDER BY checked_at DESC, id DESC LIMIT ?)`, domain, domain, limit)
	return err
}

func (d *DB) ListMailChecks(domain string, limit int) ([]model.MailCheck, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := d.sql.Query(`SELECT id,domain,dkim_selector,status,score,result_json,checked_at,ai_report,ai_error,ai_model,COALESCE(ai_updated_at,'') FROM mail_checks WHERE domain=? ORDER BY checked_at DESC, id DESC LIMIT ?`, domain, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var checks []model.MailCheck
	for rows.Next() {
		var check model.MailCheck
		var resultJSON, checkedAt, aiUpdatedAt string
		if err := rows.Scan(&check.ID, &check.Domain, &check.DKIMSelector, &check.Status, &check.Score, &resultJSON, &checkedAt, &check.AIReport, &check.AIError, &check.AIModel, &aiUpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(resultJSON), &check.Result); err != nil {
			return nil, err
		}
		check.CheckedAt, err = time.Parse(time.RFC3339Nano, checkedAt)
		if err != nil {
			return nil, err
		}
		check.AIUpdatedAt, err = parseOptionalTime(aiUpdatedAt)
		if err != nil {
			return nil, err
		}
		checks = append(checks, check)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return checks, nil
}

func (d *DB) GetMailCheck(id int64) (model.MailCheck, error) {
	var check model.MailCheck
	var resultJSON, checkedAt string
	var aiUpdatedAt string
	err := d.sql.QueryRow(`SELECT id,domain,dkim_selector,status,score,result_json,checked_at,ai_report,ai_error,ai_model,COALESCE(ai_updated_at,'') FROM mail_checks WHERE id=?`, id).
		Scan(&check.ID, &check.Domain, &check.DKIMSelector, &check.Status, &check.Score, &resultJSON, &checkedAt, &check.AIReport, &check.AIError, &check.AIModel, &aiUpdatedAt)
	if err != nil {
		return check, err
	}
	if err := json.Unmarshal([]byte(resultJSON), &check.Result); err != nil {
		return check, err
	}
	check.CheckedAt, err = time.Parse(time.RFC3339Nano, checkedAt)
	if err != nil {
		return check, err
	}
	check.AIUpdatedAt, err = parseOptionalTime(aiUpdatedAt)
	return check, err
}

func (d *DB) UpdateMailCheckAI(id int64, report, aiError, aiModel string, updatedAt time.Time) error {
	_, err := d.sql.Exec(`UPDATE mail_checks SET ai_report=?,ai_error=?,ai_model=?,ai_updated_at=? WHERE id=?`, report, aiError, aiModel, updatedAt.UTC().Format(time.RFC3339Nano), id)
	return err
}

func formatMailOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseOptionalTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func IsMailCheckNotFound(err error) bool { return err == sql.ErrNoRows }

func (d *DB) DeleteMailCheck(id int64) error {
	_, err := d.sql.Exec(`DELETE FROM mail_checks WHERE id=?`, id)
	return err
}
