package store

import (
	"testing"
	"time"

	"snippetbook/internal/model"
)

func TestMailMonitorCRUDAndResult(t *testing.T) {
	db := newTestDB(t)
	monitor := model.MailMonitor{Domain: "example.com", DKIMSelector: "default", CheckInterval: "1h", Enabled: true}
	if err := db.CreateMailMonitor(&monitor); err != nil {
		t.Fatalf("create monitor: %v", err)
	}
	monitors, err := db.ListMailMonitors(true)
	if err != nil || len(monitors) != 1 {
		t.Fatalf("list monitors = %+v, err=%v", monitors, err)
	}
	checkedAt := time.Now().UTC()
	if err := db.UpdateMailMonitorResult(monitor.ID, "warning", 85, checkedAt, model.DomainRegistration{Status: "ok", Fingerprint: "test"}); err != nil {
		t.Fatalf("update result: %v", err)
	}
	loaded, err := db.GetMailMonitor(monitor.ID)
	if err != nil {
		t.Fatalf("get monitor: %v", err)
	}
	if loaded.LastStatus != "warning" || loaded.LastScore != 85 || loaded.LastChecked == nil {
		t.Fatalf("loaded monitor = %+v", loaded)
	}
	loaded.Enabled = false
	if err := db.UpdateMailMonitor(&loaded); err != nil {
		t.Fatalf("update monitor: %v", err)
	}
	if enabled, err := db.ListMailMonitors(true); err != nil || len(enabled) != 0 {
		t.Fatalf("enabled monitors = %+v, err=%v", enabled, err)
	}
	if err := db.DeleteMailMonitor(monitor.ID); err != nil {
		t.Fatalf("delete monitor: %v", err)
	}
}
