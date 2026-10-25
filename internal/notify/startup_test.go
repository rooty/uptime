package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"snippetbook/internal/model"
	"snippetbook/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func telegramTestClient(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Timeout:       time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			copy := request.Clone(request.Context())
			address := *request.URL
			address.Scheme, address.Host = target.Scheme, target.Host
			copy.URL, copy.Host = &address, target.Host
			return http.DefaultTransport.RoundTrip(copy)
		}),
	}
}

func startupTestDB(t *testing.T) *store.DB {
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
	if err := db.EnsureSettings(); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestStartupSendsConfiguredTelegramAndSavesReleaseInformation(t *testing.T) {
	db := startupTestDB(t)
	settings := model.NotificationSettings{
		TelegramEnabled: true, TelegramBotToken: "test-token", TelegramChatID: "-100123",
		SlackEnabled: true, EmailEnabled: true,
	}
	if err := db.UpdateNotificationSettings(settings); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := telegramTestClient(t, func(rw http.ResponseWriter, request *http.Request) {
		calls++
		if request.Method != http.MethodPost || request.URL.Path != "/bottest-token/sendMessage" {
			t.Errorf("unexpected Telegram request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Content-Type") != "application/json" || request.Header.Get("User-Agent") == "" {
			t.Errorf("missing request headers")
		}
		var payload map[string]string
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		if payload["chat_id"] != settings.TelegramChatID || payload["parse_mode"] != "HTML" {
			t.Errorf("wrong Telegram destination or mode: %#v", payload)
		}
		for _, expected := range []string{"🚀 СЕРВИС ЗАПУЩЕН", "release&lt;1&gt;", "abc1234", "2026-10-25T22:00:00Z", "127.0.0.1:8080"} {
			if !strings.Contains(payload["text"], expected) {
				t.Errorf("message missing %q: %s", expected, payload["text"])
			}
		}
		if strings.Contains(payload["text"], settings.TelegramBotToken) {
			t.Error("token leaked into message")
		}
		if _, err := rw.Write([]byte(`{"ok":true,"result":{"message_id":42}}`)); err != nil {
			t.Error(err)
		}
	})
	info := StartupInfo{Version: "release<1>", Commit: "abc1234", BuildTime: "2026-10-25T21:00:00Z", StartedAt: time.Date(2026, 10, 25, 22, 0, 0, 0, time.UTC), Address: "127.0.0.1:8080"}
	err := startup(context.Background(), db, info, func(ctx context.Context, settings model.NotificationSettings, item model.Notification) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Error("startup send has no bounded timeout")
		}
		return sendTelegramWithClient(ctx, client, settings, item)
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("want one Telegram request, got %d", calls)
	}
	notifications, err := db.ListNotifications(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(notifications) != 1 || notifications[0].Kind != "application_started" || notifications[0].ServiceID != 0 || !notifications[0].CreatedAt.Equal(info.StartedAt) || !strings.Contains(notifications[0].Message, "abc1234") {
		t.Fatalf("startup not persisted: %+v", notifications)
	}
	// A restart of the same commit still produces a startup message, once per call.
	if err := startup(context.Background(), db, info, func(context.Context, model.NotificationSettings, model.Notification) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("restart notification suppressed")
	}
}

func TestStartupDisabledTelegramDoesNotSendOrWriteHistory(t *testing.T) {
	db := startupTestDB(t)
	if err := startup(context.Background(), db, StartupInfo{}, func(context.Context, model.NotificationSettings, model.Notification) error {
		t.Error("disabled Telegram called")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	items, err := db.ListNotifications(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatal("disabled Telegram wrote startup history")
	}
}

func TestStartupReportsDeliveryFailureWithoutLosingHistory(t *testing.T) {
	db := startupTestDB(t)
	if err := db.UpdateNotificationSettings(model.NotificationSettings{TelegramEnabled: true}); err != nil {
		t.Fatal(err)
	}
	err := startup(context.Background(), db, StartupInfo{StartedAt: time.Now()}, func(context.Context, model.NotificationSettings, model.Notification) error {
		return errors.New("delivery failed")
	})
	if err == nil || !strings.Contains(err.Error(), "startup telegram: delivery failed") {
		t.Fatalf("failure not reported: %v", err)
	}
	items, err := db.ListNotifications(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatal("startup history lost on delivery failure")
	}
}

func TestTelegramResponseValidationAndSafeErrors(t *testing.T) {
	for _, test := range []struct {
		name           string
		status         int
		body, expected string
	}{
		{"success", 200, `{"ok":true,"result":{"message_id":1}}`, ""},
		{"api failure", 200, `{"ok":false,"error_code":400,"description":"chat not found"}`, "chat not found"},
		{"unauthorized", 401, `{"ok":false,"error_code":401,"description":"Unauthorized"}`, "HTTP 401"},
		{"rate limit", 429, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":30}}`, "retry_after=30s"},
		{"broken JSON", 200, `not json`, "decode Telegram response"},
		{"missing ok", 200, `{}`, "API error"},
		{"redacted description", 400, `{"ok":false,"description":"invalid token test-token"}`, "[REDACTED]"},
		{"redirect", 302, ``, "HTTP 302"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := telegramTestClient(t, func(rw http.ResponseWriter, request *http.Request) {
				if test.status == 302 {
					rw.Header().Set("Location", "https://other.test/")
				}
				rw.WriteHeader(test.status)
				if _, err := rw.Write([]byte(test.body)); err != nil {
					t.Error(err)
				}
			})
			err := sendTelegramWithClient(context.Background(), client, model.NotificationSettings{TelegramBotToken: "test-token", TelegramChatID: "test-chat"}, model.Notification{})
			if test.expected == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.expected) || strings.Contains(err.Error(), "test-token") {
				t.Fatalf("unexpected Telegram error: %v", err)
			}
		})
	}
}

func TestTelegramTransportFailureDoesNotExposeToken(t *testing.T) {
	token := "test/secret-token"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("connection failed for %s", request.URL.String())
	})}
	err := sendTelegramWithClient(context.Background(), client, model.NotificationSettings{TelegramBotToken: token, TelegramChatID: "test-chat"}, model.Notification{})
	if err == nil || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), url.PathEscape(token)) || !strings.Contains(err.Error(), "connection failed") {
		t.Fatalf("unsafe transport error: %v", err)
	}
}

func TestTelegramCancelledContextStopsRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := telegramTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("cancelled request reached Telegram") })
	err := sendTelegramWithClient(ctx, client, model.NotificationSettings{TelegramBotToken: "test-token", TelegramChatID: "test-chat"}, model.Notification{})
	if err == nil || !strings.Contains(err.Error(), "context canceled") || strings.Contains(err.Error(), "test-token") {
		t.Fatalf("cancellation not handled safely: %v", err)
	}
}

func TestTelegramRequestDeadlineStopsUnresponsiveAPI(t *testing.T) {
	release := make(chan struct{})
	client := telegramTestClient(t, func(rw http.ResponseWriter, request *http.Request) {
		<-release
	})
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := sendTelegramWithClient(ctx, client, model.NotificationSettings{TelegramBotToken: "test-token", TelegramChatID: "test-chat"}, model.Notification{})
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") || strings.Contains(err.Error(), "test-token") {
		t.Fatalf("unsafe or missing timeout: %v", err)
	}
}
