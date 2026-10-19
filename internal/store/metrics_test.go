package store

import (
	"testing"
	"time"

	"snippetbook/internal/model"
)

func TestServiceMetricsAggregateChecks(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	service := model.Service{Name: "API", URL: "https://example.test", Enabled: true}
	if err := db.CreateService(&service); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	checks := []model.Check{
		{ServiceID: service.ID, Status: "online", ResponseTimeMS: 100, CheckedAt: now.Add(-10 * time.Minute), Metrics: &model.CheckMetrics{DNSMS: 4, ConnectMS: 8, TLSMS: 12, TTFBMS: 30, PageSizeBytes: 1000}},
		{ServiceID: service.ID, Status: "offline", ResponseTimeMS: 300, CheckedAt: now.Add(-5 * time.Minute), Metrics: &model.CheckMetrics{DNSMS: 5, ConnectMS: 9, TLSMS: 13, TTFBMS: 40, PageSizeBytes: 1200}},
	}
	for index := range checks {
		if err := db.AddCheck(&checks[index]); err != nil {
			t.Fatal(err)
		}
	}

	metrics, err := db.ServiceMetrics(service.ID, "24h")
	if err != nil {
		t.Fatal(err)
	}
	if metrics.TotalChecks != 2 || metrics.OnlineChecks != 1 || metrics.Offline != 1 {
		t.Fatalf("unexpected summary: %+v", metrics)
	}
	if metrics.Uptime != 50 {
		t.Fatalf("uptime = %v, want 50", metrics.Uptime)
	}
	if len(metrics.Buckets) != 1 || metrics.Buckets[0].ResponseMaxMS != 300 {
		t.Fatalf("unexpected buckets: %+v", metrics.Buckets)
	}
}
