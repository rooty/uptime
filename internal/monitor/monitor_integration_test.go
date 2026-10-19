package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"snippetbook/internal/model"
	"snippetbook/internal/store"
)

func TestRunOnceChecksEnabledServicesAndPurgesHistory(t *testing.T) {
	db, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	}()
	if err := db.EnsureSettings(); err != nil {
		t.Fatalf("ensure settings: %v", err)
	}
	if err := db.UpdateSettings(model.Settings{CheckTimeout: "1s", CheckHistoryLimit: 1}); err != nil {
		t.Fatalf("configure settings: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	enabled := model.Service{Name: "enabled", URL: server.URL, Enabled: true}
	if err := db.CreateService(&enabled); err != nil {
		t.Fatalf("create enabled service: %v", err)
	}
	disabled := model.Service{Name: "disabled", URL: server.URL, Enabled: false}
	if err := db.CreateService(&disabled); err != nil {
		t.Fatalf("create disabled service: %v", err)
	}

	monitor := New(db)
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatalf("first monitoring cycle: %v", err)
	}
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatalf("second monitoring cycle: %v", err)
	}

	history, err := db.CheckHistory(enabled.ID, 10)
	if err != nil {
		t.Fatalf("read enabled history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("enabled history length = %d, want 1", len(history))
	}
	if history[0].Status != "online" || history[0].HTTPStatus != http.StatusOK {
		t.Fatalf("last check = %+v, want online HTTP 200", history[0])
	}
	if history[0].CheckedAt.After(time.Now().UTC()) {
		t.Fatal("check timestamp is in the future")
	}

	history, err = db.CheckHistory(disabled.ID, 10)
	if err != nil {
		t.Fatalf("read disabled history: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("disabled history length = %d, want 0", len(history))
	}
}

func TestRunUsesEachServiceInterval(t *testing.T) {
	db, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	}()
	if err := db.EnsureSettings(); err != nil {
		t.Fatalf("ensure settings: %v", err)
	}
	if err := db.UpdateSettings(model.Settings{CheckTimeout: "1s", CheckHistoryLimit: 100}); err != nil {
		t.Fatalf("configure settings: %v", err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	fast := model.Service{Name: "fast", URL: server.URL, Enabled: true, CheckInterval: "10ms"}
	slow := model.Service{Name: "slow", URL: server.URL, Enabled: true, CheckInterval: "1h"}
	if err := db.CreateService(&fast); err != nil {
		t.Fatalf("create fast service: %v", err)
	}
	if err := db.CreateService(&slow); err != nil {
		t.Fatalf("create slow service: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2200*time.Millisecond)
	defer cancel()
	New(db).Run(ctx)

	fastHistory, err := db.CheckHistory(fast.ID, 100)
	if err != nil {
		t.Fatalf("read fast history: %v", err)
	}
	slowHistory, err := db.CheckHistory(slow.ID, 100)
	if err != nil {
		t.Fatalf("read slow history: %v", err)
	}
	if len(fastHistory) < 3 {
		t.Fatalf("fast history length = %d, want at least 3", len(fastHistory))
	}
	if len(slowHistory) != 1 {
		t.Fatalf("slow history length = %d, want 1", len(slowHistory))
	}
	if requests.Load() < 4 {
		t.Fatalf("request count = %d, want at least 4", requests.Load())
	}
}

func TestProcessIncidentTracksStartResolutionAndDuration(t *testing.T) {
	db, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	}()
	service := model.Service{ID: 42, Name: "Mail", URL: "https://mail.example", Environment: "production", CheckInterval: "30s", RepeatNotificationsEnabled: true, RepeatNotificationInterval: "1m"}
	started := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	startNotification, err := ProcessIncident(db, service, model.Check{ServiceID: service.ID, Status: "offline", Error: "timeout", CheckedAt: started})
	if err != nil {
		t.Fatalf("start incident: %v", err)
	}
	if startNotification == nil || !strings.Contains(startNotification.Message, "Проблема началась") {
		t.Fatalf("start notification = %+v", startNotification)
	}
	secondNotification, err := ProcessIncident(db, service, model.Check{ServiceID: service.ID, Status: "offline", Error: "timeout", CheckedAt: started.Add(30 * time.Second)})
	if err != nil {
		t.Fatalf("repeat incident: %v", err)
	}
	if secondNotification != nil {
		t.Fatalf("repeat notification = %+v, want nil", secondNotification)
	}
	repeatNotification, err := ProcessIncident(db, service, model.Check{ServiceID: service.ID, Status: "offline", Error: "timeout", CheckedAt: started.Add(61 * time.Second)})
	if err != nil {
		t.Fatalf("repeat incident: %v", err)
	}
	if repeatNotification == nil || !strings.Contains(repeatNotification.Message, "Проблема продолжается") {
		t.Fatalf("repeat notification = %+v", repeatNotification)
	}
	resolved, err := ProcessIncident(db, service, model.Check{ServiceID: service.ID, Status: "online", HTTPStatus: http.StatusOK, CheckedAt: started.Add(2*time.Minute + 3*time.Second)})
	if err != nil {
		t.Fatalf("resolve incident: %v", err)
	}
	if resolved == nil || !strings.Contains(resolved.Message, "Проблема решена") || !strings.Contains(resolved.Message, "2 мин. 3 сек.") {
		t.Fatalf("resolved notification = %+v", resolved)
	}
}
