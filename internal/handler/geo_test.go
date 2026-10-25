package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"snippetbook/internal/crypto"
	"snippetbook/internal/model"
	"snippetbook/internal/store"
)

func TestMapAPIUsesSavedDiagnosticsAndCatalogFilters(t *testing.T) {
	db, err := store.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	service := model.Service{Name: "Mail", URL: "smtp://user:secret@mail.example.test:25", Environment: "production", Tags: []string{"mail"}, Enabled: true, CheckType: "smtp"}
	if err := db.CreateService(&service); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	lastSuccess := now.Add(-25 * time.Hour)
	lat, lon := 51.0, 17.0
	if err := db.SaveServiceLocation(model.ServiceLocation{ServiceID: service.ID, Hostname: "mail.example.test", IPs: []string{"8.8.8.8", "10.0.0.1"}, ResolvedAt: now, NextAttemptAt: now.Add(time.Hour), Error: "DNS lookup mail.example.test: timeout"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveIPGeolocation(model.IPGeolocation{IP: "8.8.8.8", Latitude: &lat, Longitude: &lon, LastSuccessAt: &lastSuccess, Status: "error", Error: "ip-api.com IP 8.8.8.8: HTTP status 429"}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddCheck(&model.Check{ServiceID: service.ID, Status: "offline", Error: "TCP connect mail.example.test:25: refused", CheckedAt: now, Diagnostics: map[string]any{"phase": "TCP connect", "port": 25}}); err != nil {
		t.Fatal(err)
	}
	router := SetupRouter(db, crypto.NewVault(db))
	get := func(query string) []mapService {
		t.Helper()
		rw := httptest.NewRecorder()
		router.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/api/v1/services/map"+query, nil))
		if rw.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
		}
		if strings.Contains(rw.Body.String(), "secret") {
			t.Fatal("endpoint credentials leaked in map response")
		}
		var result struct {
			Services []mapService `json:"services"`
		}
		if err := json.Unmarshal(rw.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Services
	}
	items := get("?environment=production&tag=mail&check_type=smtp&search=Mail&status=offline")
	if len(items) != 1 || len(items[0].Addresses) != 2 {
		t.Fatal(items)
	}
	ip := items[0].Addresses[0]
	if !ip.Stale || ip.Location == nil || !strings.Contains(ip.Error, "429") || *ip.Location.Latitude != lat {
		t.Fatalf("missing saved geolocation diagnostics: %+v", ip)
	}
	if items[0].Addresses[1].Status != "special" || !strings.Contains(items[0].Error, "DNS lookup") || items[0].LastCheck.Diagnostics["phase"] != "TCP connect" {
		t.Fatal(items)
	}
	for _, query := range []string{"?environment=staging", "?tag=other", "?search=absent", "?check_type=http", "?status=online"} {
		if len(get(query)) != 0 {
			t.Errorf("filter ignored: %s", query)
		}
	}
	// An endpoint change must not expose old coordinates while waiting for DNS.
	service.URL = "https://new.example.test"
	if err := db.UpdateService(&service); err != nil {
		t.Fatal(err)
	}
	items = get("")
	if len(items[0].Addresses) != 0 || items[0].Hostname != "new.example.test" || items[0].Error == "" {
		t.Fatal(items)
	}
	if err := db.DeleteService(service.ID); err != nil {
		t.Fatal(err)
	}
	bindings, err := db.ServiceLocations()
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 0 {
		t.Fatal("deleted service binding retained")
	}
	if err := db.SaveServiceLocation(model.ServiceLocation{ServiceID: service.ID, Hostname: "new.example.test"}); err != nil {
		t.Fatal(err)
	}
	bindings, err = db.ServiceLocations()
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 0 || len(get("")) != 0 {
		t.Fatal("deleted service resurrected")
	}
}

func TestMapRouteIsProtectedByCloudflareAccess(t *testing.T) {
	t.Setenv("CLOUDFLARE_ACCESS_TEAM_DOMAIN", "https://example.cloudflareaccess.com")
	t.Setenv("CLOUDFLARE_ACCESS_AUDIENCE", "test-audience")
	db, err := store.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	router := SetupRouter(db, crypto.NewVault(db))
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/api/v1/services/map", nil))
	if rw.Code != http.StatusUnauthorized && rw.Code != http.StatusForbidden {
		t.Fatalf("unprotected map: %d", rw.Code)
	}
}
