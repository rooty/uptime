package mailcheck

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMxToolboxBlacklistResolverConvertsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "test-key" {
			t.Errorf("Authorization = %q, want test-key", request.Header.Get("Authorization"))
		}
		if request.URL.Path != "/api/v1/Lookup/blacklist/" || request.URL.Query().Get("argument") != "192.0.2.10" {
			t.Errorf("unexpected request URL: %s", request.URL.String())
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"Failed":[{"Name":"BARRACUDA","Info":"Blacklisted by BARRACUDA","Url":"https://example.test/detail","BlacklistTTL":"900","BlacklistResponseTime":"34","BlacklistReasonCode":"127.0.0.2","BlacklistReasonDescription":"Listed","DelistUrl":"https://example.test/delist"}],"Passed":[{"Name":"0SPAM","BlacklistResponseTime":"4"}],"Timeouts":[{"Name":"SPFBL DNSBL","Info":"provider timeout"}]}`)
	}))
	defer server.Close()

	resolver := &mxtoolboxBlacklistResolver{apiKey: "test-key", baseURL: server.URL + "/api/v1", client: &http.Client{Timeout: time.Second}}
	result, err := resolver.LookupBlacklist(context.Background(), "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "warning" || result.Checked != 3 || result.Listed != 1 || result.Timeout != 1 {
		t.Fatalf("unexpected result summary: %+v", result)
	}
	if result.Results[0].Status != "listed" || result.Results[0].TTL != 900 || result.Results[0].ResponseTimeMS != 34 {
		t.Fatalf("unexpected listed result: %+v", result.Results[0])
	}
	if result.Results[1].Status != "ok" || result.Results[2].Status != "timeout" {
		t.Fatalf("unexpected status results: %+v", result.Results)
	}
}

func TestMxToolboxBlacklistResolverHandlesUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	resolver := &mxtoolboxBlacklistResolver{apiKey: "bad-key", baseURL: server.URL, client: server.Client()}
	if _, err := resolver.LookupBlacklist(context.Background(), "192.0.2.10"); err == nil {
		t.Fatal("expected unauthorized error")
	}
}
