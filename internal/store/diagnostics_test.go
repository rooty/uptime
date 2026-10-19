package store

import (
	"testing"
	"time"

	"snippetbook/internal/model"
)

func TestCheckDiagnosticsPersisted(t *testing.T) {
	db := newTestDB(t)
	service := model.Service{Name: "HTTP test", URL: "https://example.com", CheckType: "https", Enabled: true}
	if err := db.CreateService(&service); err != nil {
		t.Fatalf("create service: %v", err)
	}
	check := model.Check{ServiceID: service.ID, Status: "offline", Error: "connection refused", CheckedAt: time.Now().UTC(), Diagnostics: map[string]any{
		"protocol": "https", "endpoint": service.URL, "hostname": "example.com", "port": "443", "phase": "tcp_or_tls_connect",
	}}
	if err := db.AddCheck(&check); err != nil {
		t.Fatalf("add check: %v", err)
	}
	history, err := db.CheckHistory(service.ID, 10)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if len(history) != 1 || history[0].Diagnostics["phase"] != "tcp_or_tls_connect" {
		t.Fatalf("diagnostics = %+v", history)
	}
}
