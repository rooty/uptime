package store

import (
	"testing"
	"time"

	"snippetbook/internal/model"
)

func TestMailCheckAISnapshotPersists(t *testing.T) {
	db := newTestDB(t)
	check := model.MailCheck{
		Domain:    "example.com",
		Status:    "ok",
		Score:     100,
		CheckedAt: time.Now().UTC(),
		Result:    model.MailCheckResult{},
	}
	if err := db.AddMailCheck(&check); err != nil {
		t.Fatalf("add mail check: %v", err)
	}
	updatedAt := time.Now().UTC()
	if err := db.UpdateMailCheckAI(check.ID, "# Отчёт\n\nВсё хорошо.", "", "test-model", updatedAt); err != nil {
		t.Fatalf("update AI report: %v", err)
	}
	loaded, err := db.GetMailCheck(check.ID)
	if err != nil {
		t.Fatalf("get mail check: %v", err)
	}
	if loaded.AIReport == "" || loaded.AIError != "" || loaded.AIModel != "test-model" || loaded.AIUpdatedAt == nil {
		t.Fatalf("loaded AI snapshot = %+v", loaded)
	}
	if err := db.UpdateMailCheckAI(check.ID, "", "AI returned an empty report (finish_reason=length)", "test-model", updatedAt); err != nil {
		t.Fatalf("update AI error: %v", err)
	}
	loaded, err = db.GetMailCheck(check.ID)
	if err != nil {
		t.Fatalf("get mail check after error: %v", err)
	}
	if loaded.AIReport != "" || loaded.AIError == "" {
		t.Fatalf("loaded AI error snapshot = %+v", loaded)
	}
}
