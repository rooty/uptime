package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"snippetbook/internal/crypto"
	"snippetbook/internal/store"
)

func TestHealthzReturnsBuildInformation(t *testing.T) {
	db, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	}()

	router := SetupRouter(db, crypto.NewVault(db))
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Header().Get("X-App-Version") == "" || response.Header().Get("Cache-Control") == "" {
		t.Fatalf("unexpected health headers: %v", response.Header())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, field := range []string{"status", "version", "commit", "build_time", "started_at", "uptime_seconds"} {
		if _, ok := payload[field]; !ok {
			t.Errorf("response is missing %q: %v", field, payload)
		}
	}
	if payload["status"] != "ok" {
		t.Errorf("status = %v, want ok", payload["status"])
	}
}
