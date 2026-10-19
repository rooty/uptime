package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"snippetbook/internal/crypto"
	"snippetbook/internal/store"
)

func TestSettingsAPIValidatesAndPersistsValues(t *testing.T) {
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
	router := SetupRouter(db, crypto.NewVault(db))

	invalid := `{"check_timeout":"not-a-duration","check_history_limit":10}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/settings", bytes.NewBufferString(invalid))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid settings status = %d, want %d", response.Code, http.StatusBadRequest)
	}

	valid := `{"check_timeout":"2s","check_history_limit":25}`
	request = httptest.NewRequest(http.MethodPut, "/api/v1/settings", bytes.NewBufferString(valid))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("valid settings status = %d, want %d", response.Code, http.StatusOK)
	}

	var settings map[string]any
	if err := json.NewDecoder(response.Body).Decode(&settings); err != nil {
		t.Fatalf("decode settings response: %v", err)
	}
	if settings["check_timeout"] != "2s" || settings["check_history_limit"] != float64(25) {
		t.Fatalf("settings response = %#v", settings)
	}
}

func TestServicesAPICRUDAndURLValidation(t *testing.T) {
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
	router := SetupRouter(db, crypto.NewVault(db))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	requestJSON := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}

	bad := requestJSON(http.MethodPost, "/api/v1/services", `{"name":"bad","url":"file:///tmp/x"}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid URL status = %d, want %d", bad.Code, http.StatusBadRequest)
	}

	created := requestJSON(http.MethodPost, "/api/v1/services", `{"name":"API","url":"`+server.URL+`/health","tags":["API"," api ","critical"]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", created.Code, http.StatusCreated)
	}
	var service struct {
		ID        int64     `json:"id"`
		Tags      []string  `json:"tags"`
		LastCheck *struct{} `json:"last_check"`
	}
	if err := json.NewDecoder(created.Body).Decode(&service); err != nil {
		t.Fatalf("decode service: %v", err)
	}
	if len(service.Tags) != 2 || service.Tags[0] != "api" || service.Tags[1] != "critical" {
		t.Fatalf("normalized tags = %#v", service.Tags)
	}
	if service.LastCheck == nil {
		t.Fatal("new service has no immediate check")
	}

	id := strconv.FormatInt(service.ID, 10)
	updated := requestJSON(http.MethodPut, "/api/v1/services/"+id, `{"name":"API updated","url":"`+server.URL+`/status","enabled":false}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d", updated.Code, http.StatusOK)
	}
	list := requestJSON(http.MethodGet, "/api/v1/services", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", list.Code, http.StatusOK)
	}
	deleted := requestJSON(http.MethodDelete, "/api/v1/services/"+id, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d", deleted.Code, http.StatusNoContent)
	}
	dashboard := requestJSON(http.MethodGet, "/api/v1/dashboard", "")
	if dashboard.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want %d", dashboard.Code, http.StatusOK)
	}
}

func TestRunbookAPIParsesEditsAndDeletesMarkdown(t *testing.T) {
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
	router := SetupRouter(db, crypto.NewVault(db))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	requestJSON := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	createdService := requestJSON(http.MethodPost, "/api/v1/services", `{"name":"Docs","url":"`+server.URL+`"}`)
	var service struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(createdService.Body).Decode(&service); err != nil {
		t.Fatalf("decode service: %v", err)
	}

	path := "/api/v1/services/" + strconv.FormatInt(service.ID, 10) + "/runbooks"
	created := requestJSON(http.MethodPost, path, `{"title":"Health","content":"# Check\n\n**OK**"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create runbook status = %d", created.Code)
	}
	var runbook struct {
		ID           int64  `json:"id"`
		RenderedHTML string `json:"rendered_html"`
	}
	if err := json.NewDecoder(created.Body).Decode(&runbook); err != nil {
		t.Fatalf("decode runbook: %v", err)
	}
	if !strings.Contains(runbook.RenderedHTML, "<h1>Check</h1>") {
		t.Fatalf("rendered HTML = %q", runbook.RenderedHTML)
	}

	updated := requestJSON(http.MethodPut, "/api/v1/runbooks/"+strconv.FormatInt(runbook.ID, 10), `{"title":"Updated","content":"- item"}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update runbook status = %d", updated.Code)
	}
	deleted := requestJSON(http.MethodDelete, "/api/v1/runbooks/"+strconv.FormatInt(runbook.ID, 10), "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete runbook status = %d", deleted.Code)
	}
}

func TestNotificationSettingsAPIValidatesGlobalChannels(t *testing.T) {
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
	router := SetupRouter(db, crypto.NewVault(db))
	request := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-settings", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	if response := request(`{"telegram_enabled":true}`); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid Telegram settings status = %d", response.Code)
	}
	if response := request(`{"telegram_enabled":true,"telegram_bot_token":"token","telegram_chat_id":"chat","slack_enabled":false,"email_enabled":false}`); response.Code != http.StatusOK {
		t.Fatalf("valid notification settings status = %d", response.Code)
	}
	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/notification-settings", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "telegram_enabled") {
		t.Fatalf("get notification settings response = %s", get.Body.String())
	}
}
