package monitor

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"snippetbook/internal/model"
	"snippetbook/internal/store"
)

func TestCheckClassifiesSuccessfulResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	check := Check(context.Background(), model.Service{ID: 7, URL: server.URL}, time.Second, nil)
	if check.Status != "online" {
		t.Fatalf("status = %q, want online", check.Status)
	}
	if check.HTTPStatus != http.StatusNoContent {
		t.Fatalf("http status = %d, want %d", check.HTTPStatus, http.StatusNoContent)
	}
	if check.ResponseTimeMS < 0 {
		t.Fatalf("response time = %d, want non-negative", check.ResponseTimeMS)
	}
}

func TestCheckCollectsHTTPMetrics(t *testing.T) {
	const body = "snippetbook metrics"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	check := Check(context.Background(), model.Service{ID: 70, URL: server.URL}, time.Second, nil)
	if check.Metrics == nil {
		t.Fatal("metrics are nil")
	}
	if check.Metrics.PageSizeBytes != int64(len(body)) {
		t.Fatalf("page size = %d, want %d", check.Metrics.PageSizeBytes, len(body))
	}
	if check.Metrics.TTFBMS < 0 || check.Metrics.ConnectMS < 0 || check.Metrics.DNSMS < 0 {
		t.Fatalf("invalid HTTP metrics: %+v", check.Metrics)
	}
}

func TestCheckClassifiesUnreachableService(t *testing.T) {
	check := Check(context.Background(), model.Service{ID: 8, URL: "http://127.0.0.1:1"}, 50*time.Millisecond, nil)
	if check.Status != "offline" {
		t.Fatalf("status = %q, want offline", check.Status)
	}
	if check.Error == "" {
		t.Fatal("offline check has no error")
	}
}

func TestCheckTLSReportsCertificateErrors(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tlsInfo := CheckTLS(context.Background(), model.Service{ID: 9, URL: server.URL}, time.Second)
	if tlsInfo.Status != "error" || tlsInfo.Error == "" || tlsInfo.Hostname == "" {
		t.Fatalf("TLS info = %+v, want certificate error and hostname", tlsInfo)
	}
}

func TestCheckClassifiesHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	check := Check(context.Background(), model.Service{ID: 10, URL: server.URL}, time.Second, nil)
	if check.Status != "degraded" || check.HTTPStatus != http.StatusBadGateway {
		t.Fatalf("check = %+v, want degraded HTTP 502", check)
	}
}

func TestCheckProtocolGreetings(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		greeting string
		reply    string
	}{
		{name: "smtp", protocol: "smtp", greeting: "220 mail.example ESMTP\r\n", reply: "250 mail.example\r\n"},
		{name: "imap", protocol: "imap", greeting: "* OK IMAP4 ready\r\n"},
		{name: "pop3", protocol: "pop3", greeting: "+OK POP3 ready\r\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			serverDone := make(chan error, 1)
			go func() {
				connection, acceptErr := listener.Accept()
				if acceptErr != nil {
					serverDone <- acceptErr
					return
				}
				defer connection.Close()
				if _, writeErr := connection.Write([]byte(test.greeting)); writeErr != nil {
					serverDone <- writeErr
					return
				}
				if test.reply != "" {
					_, readErr := bufio.NewReader(connection).ReadString('\n')
					if readErr == nil {
						_, readErr = connection.Write([]byte(test.reply))
					}
					serverDone <- readErr
					return
				}
				serverDone <- nil
			}()
			serviceURL := fmt.Sprintf("%s://%s", test.protocol, listener.Addr().String())
			check := Check(context.Background(), model.Service{ID: 20, URL: serviceURL, CheckType: test.protocol}, time.Second, nil)
			if check.Status != "online" {
				t.Fatalf("check = %+v, want online", check)
			}
			if err := <-serverDone; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCheckTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(100 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	check := Check(context.Background(), model.Service{ID: 11, URL: server.URL}, 10*time.Millisecond, nil)
	if check.Status != "offline" || check.Error == "" {
		t.Fatalf("check = %+v, want timeout offline result", check)
	}
}

func TestRunStopsWhenContextIsCanceled(t *testing.T) {
	db, err := store.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	monitor := New(db)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		monitor.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor did not stop after context cancellation")
	}
}

func TestBuildStatusNotificationIncludesServiceDetails(t *testing.T) {
	notification := BuildStatusNotification(model.Service{Name: "Mail", URL: "https://mail.example", Environment: "production", CheckInterval: "30s"}, model.Check{Status: "degraded", HTTPStatus: http.StatusBadGateway, ResponseTimeMS: 120, CheckedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Error: "upstream unavailable"})
	for _, expected := range []string{"Mail", "https://mail.example", "production", "degraded", "502", "120 ms", "30s", "upstream unavailable"} {
		if !strings.Contains(notification.Message, expected) {
			t.Fatalf("notification message misses %q: %s", expected, notification.Message)
		}
	}
}
