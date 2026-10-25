package geo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"snippetbook/internal/model"
	"snippetbook/internal/store"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

func testDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func addService(t *testing.T, db *store.DB, endpoint string) model.Service {
	t.Helper()
	service := model.Service{Name: endpoint, URL: endpoint, Enabled: true}
	if err := db.CreateService(&service); err != nil {
		t.Fatal(err)
	}
	return service
}

func step(t *testing.T, worker *Worker) {
	t.Helper()
	if err := worker.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHostnameAndPublicIP(t *testing.T) {
	if host := Hostname("postgres://user:secret@DB.Example.com.:5432/db?password=secret"); host != "db.example.com" {
		t.Fatal(host)
	}
	if host := Hostname("https://[2606:4700:4700::1111]:443/"); host != "2606:4700:4700::1111" {
		t.Fatal(host)
	}
	for _, text := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.0.1", "100.64.0.1", "169.254.1.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "::1", "fc00::1", "fe80::1", "2001:db8::1", "::ffff:192.168.1.1", "64:ff9b::808:808"} {
		if PublicIP(netip.MustParseAddr(text)) {
			t.Errorf("special address accepted: %s", text)
		}
	}
	for _, text := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "::ffff:8.8.8.8"} {
		if !PublicIP(netip.MustParseAddr(text)) {
			t.Errorf("public address rejected: %s", text)
		}
	}
}

func TestSharedCacheDailyRefreshRestartAndIPChange(t *testing.T) {
	db := testDB(t)
	first := addService(t, db, "https://example.test/health")
	addService(t, db, "smtp://example.test:25")
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	calls, dnsCalls := 0, 0
	ip := "8.8.8.8"
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		calls++
		address := strings.TrimPrefix(req.URL.Path, "/")
		if _, err := netip.ParseAddr(address); err != nil {
			t.Error(err)
		}
		if _, err := fmt.Fprintf(rw, `{"status":"success","query":%q,"lat":0,"lon":0,"country":"Test","isp":"Example"}`, address); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	worker := New(db)
	worker.BaseURL = server.URL
	worker.Now = func() time.Time { return now }
	worker.Resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		dnsCalls++
		return []netip.Addr{netip.MustParseAddr(ip), netip.MustParseAddr(ip)}, nil
	})
	step(t, worker)
	if calls != 1 || dnsCalls != 1 {
		t.Fatalf("provider=%d DNS=%d", calls, dnsCalls)
	}
	cache, err := db.IPGeolocations()
	if err != nil {
		t.Fatal(err)
	}
	if cache[ip].LastSuccessAt == nil || *cache[ip].Latitude != 0 || cache[ip].NextAttemptAt.Sub(now) != 24*time.Hour {
		t.Fatal(cache)
	}
	// A new worker uses persisted freshness, not its own in-memory timer.
	restarted := New(db)
	restarted.BaseURL, restarted.Resolver, restarted.Now = worker.BaseURL, worker.Resolver, worker.Now
	now = now.Add(23 * time.Hour)
	step(t, restarted)
	if calls != 1 {
		t.Fatalf("fresh data requested again: %d", calls)
	}
	now = now.Add(time.Hour)
	step(t, restarted)
	if calls != 2 {
		t.Fatalf("daily refresh missing: %d", calls)
	}
	// Endpoint changes bypass the old hostname's DNS cache immediately.
	first.URL = "https://changed.test/"
	if err := db.UpdateService(&first); err != nil {
		t.Fatal(err)
	}
	ip = "1.1.1.1"
	now = now.Add(2 * time.Second)
	step(t, restarted)
	if calls != 3 {
		t.Fatalf("new IP not looked up immediately: %d", calls)
	}
	bindings, err := db.ServiceLocations()
	if err != nil {
		t.Fatal(err)
	}
	if bindings[first.ID].Hostname != "changed.test" || len(bindings[first.ID].IPs) != 1 || bindings[first.ID].IPs[0] != ip {
		t.Fatal(bindings)
	}
}

func TestMultipleAddressesSpecialIPsAndDNSFailure(t *testing.T) {
	db := testDB(t)
	service := addService(t, db, "https://example.test/")
	now := time.Now()
	worker := New(db)
	worker.Now = func() time.Time { return now }
	failDNS := false
	worker.Resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		if failDNS {
			return nil, errors.New("test timeout")
		}
		return []netip.Addr{netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("2606:4700:4700::1111"), netip.MustParseAddr("8.8.8.8")}, nil
	})
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		calls++
		ip := strings.TrimPrefix(req.URL.Path, "/")
		if ip == "10.0.0.1" {
			t.Error("private IP leaked")
		}
		if _, err := fmt.Fprintf(rw, `{"status":"success","query":%q,"lat":50,"lon":17}`, ip); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	worker.BaseURL = server.URL
	step(t, worker)
	now = now.Add(2 * time.Second)
	step(t, worker)
	if calls != 2 {
		t.Fatalf("want two public IP requests, got %d", calls)
	}
	failDNS = true
	now = now.Add(time.Hour)
	step(t, worker)
	bindings, err := db.ServiceLocations()
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings[service.ID].IPs) != 3 || !strings.Contains(bindings[service.ID].Error, "DNS lookup example.test: test timeout") {
		t.Fatal(bindings)
	}
}

func TestProviderErrorsPreserveCoordinatesAndCooldown(t *testing.T) {
	for _, mode := range []string{"429", "fail", "json", "mismatch", "coordinates", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			db := testDB(t)
			addService(t, db, "https://8.8.8.8")
			now := time.Now().UTC()
			previous := now.Add(-25 * time.Hour)
			lat, lon := 51.0, 17.0
			if err := db.SaveIPGeolocation(model.IPGeolocation{IP: "8.8.8.8", Latitude: &lat, Longitude: &lon, LastSuccessAt: &previous}); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				rw.Header().Set("X-Rl", "0")
				rw.Header().Set("X-Ttl", "90")
				body := ""
				switch mode {
				case "429":
					rw.WriteHeader(429)
				case "fail":
					body = `{"status":"fail","message":"reserved range","query":"8.8.8.8"}`
				case "json":
					body = "not json"
				case "mismatch":
					body = `{"status":"success","query":"1.1.1.1","lat":50,"lon":17}`
				case "coordinates":
					body = `{"status":"success","query":"8.8.8.8","lat":91,"lon":17}`
				case "timeout":
					<-req.Context().Done()
					return
				}
				if _, err := rw.Write([]byte(body)); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			worker := New(db)
			worker.BaseURL, worker.Now = server.URL, func() time.Time { return now }
			worker.Client.Timeout = 30 * time.Millisecond
			step(t, worker)
			cache, err := db.IPGeolocations()
			if err != nil {
				t.Fatal(err)
			}
			location := cache["8.8.8.8"]
			if location.Status != "error" || !strings.Contains(location.Error, "IP 8.8.8.8") || !Stale(location, now) || *location.Latitude != lat || !location.LastSuccessAt.Equal(previous) {
				t.Fatalf("lost diagnostic or old coordinates: %+v", location)
			}
			if !location.NextAttemptAt.After(now) {
				t.Fatal("missing backoff")
			}
			if mode != "timeout" {
				next, err := db.GeoProviderNextAttempt()
				if err != nil {
					t.Fatal(err)
				}
				if next.Sub(now) != 91*time.Second {
					t.Fatalf("cooldown %s", next.Sub(now))
				}
			}
		})
	}
}

func TestSuccessfulResponseExhaustingLimitIsPersistedAcrossDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "geo.db")
	db, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	addService(t, db, "https://8.8.8.8")
	addService(t, db, "https://1.1.1.1")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		calls++
		rw.Header().Set("X-Rl", "0")
		rw.Header().Set("X-Ttl", "60")
		if err := json.NewEncoder(rw).Encode(map[string]any{"status": "success", "query": strings.TrimPrefix(req.URL.Path, "/"), "lat": 50, "lon": 17}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	now := time.Now()
	worker := New(db)
	worker.BaseURL, worker.Now = server.URL, func() time.Time { return now }
	step(t, worker)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	worker = New(db)
	worker.BaseURL, worker.Now = server.URL, func() time.Time { return now }
	now = now.Add(30 * time.Second)
	step(t, worker)
	if calls != 1 {
		t.Fatalf("ignored persisted cooldown: %d", calls)
	}
	now = now.Add(32 * time.Second)
	step(t, worker)
	if calls != 2 {
		t.Fatalf("queue did not resume: %d", calls)
	}
}

func TestDNSNegativeCachePreservesEachServicesLastKnownIPs(t *testing.T) {
	db := testDB(t)
	old := addService(t, db, "https://shared.test/old")
	fresh := addService(t, db, "http://shared.test/new")
	now := time.Now()
	if err := db.SaveServiceLocation(model.ServiceLocation{ServiceID: old.ID, Hostname: "shared.test", IPs: []string{"8.8.8.8"}, ResolvedAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetGeoProviderNextAttempt(now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	calls := 0
	worker := New(db)
	worker.Now = func() time.Time { return now }
	worker.Resolver = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		calls++
		return nil, errors.New("DNS timeout")
	})
	step(t, worker)
	bindings, err := db.ServiceLocations()
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings[old.ID].IPs) != 1 || len(bindings[fresh.ID].IPs) != 0 || bindings[old.ID].Error == "" || bindings[fresh.ID].Error == "" {
		t.Fatal(bindings)
	}
	now = now.Add(2 * time.Minute)
	step(t, worker)
	if calls != 1 {
		t.Fatalf("negative DNS cache ignored: %d", calls)
	}
}
