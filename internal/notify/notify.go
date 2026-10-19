package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"

	"snippetbook/internal/model"
	"snippetbook/internal/store"
)

// Dispatch sends a status-change notification to every enabled configured channel.
// Individual channel failures are returned together so one channel cannot hide another failure.
func Dispatch(ctx context.Context, db *store.DB, item model.Notification) error {
	settings, err := db.NotificationSettings()
	if err != nil {
		return err
	}
	return DispatchSettings(ctx, settings, item)
}

func DispatchSettings(ctx context.Context, settings model.NotificationSettings, item model.Notification) error {
	var failures []string
	if settings.TelegramEnabled {
		if err := sendTelegram(ctx, settings, item); err != nil {
			failures = append(failures, "telegram: "+err.Error())
		}
	}
	if settings.SlackEnabled {
		if err := sendSlack(ctx, settings, item); err != nil {
			failures = append(failures, "slack: "+err.Error())
		}
	}
	if settings.EmailEnabled {
		if err := sendEmail(ctx, settings, item); err != nil {
			failures = append(failures, "email: "+err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func sendTelegram(ctx context.Context, settings model.NotificationSettings, item model.Notification) error {
	if settings.TelegramBotToken == "" || settings.TelegramChatID == "" {
		return fmt.Errorf("bot token and chat id are required")
	}
	payload, _ := json.Marshal(map[string]string{"chat_id": settings.TelegramChatID, "text": item.Title + "\n" + item.Message})
	endpoint := "https://api.telegram.org/bot" + url.PathEscape(settings.TelegramBotToken) + "/sendMessage"
	return postJSON(ctx, endpoint, payload, nil)
}

func sendSlack(ctx context.Context, settings model.NotificationSettings, item model.Notification) error {
	if settings.SlackWebhookURL == "" {
		return fmt.Errorf("webhook URL is required")
	}
	payload, _ := json.Marshal(map[string]string{"text": item.Title + " — " + item.Message})
	return postJSON(ctx, settings.SlackWebhookURL, payload, nil)
}

func postJSON(ctx context.Context, endpoint string, payload []byte, headers http.Header) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	response, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return nil
}

func sendEmail(ctx context.Context, settings model.NotificationSettings, item model.Notification) error {
	if settings.SMTPHost == "" || settings.EmailFrom == "" || settings.EmailTo == "" {
		return fmt.Errorf("SMTP host, from and to are required")
	}
	port := settings.SMTPPort
	if port == 0 {
		port = 587
	}
	address := settings.SMTPHost + ":" + strconv.Itoa(port)
	auth := smtp.Auth(nil)
	if settings.SMTPUsername != "" {
		auth = smtp.PlainAuth("", settings.SMTPUsername, settings.SMTPPassword, settings.SMTPHost)
	}
	message := []byte("To: " + settings.EmailTo + "\r\n" + "Subject: " + item.Title + "\r\n" + "Content-Type: text/plain; charset=UTF-8\r\n\r\n" + item.Message + "\r\n")
	result := make(chan error, 1)
	go func() {
		result <- smtp.SendMail(address, auth, settings.EmailFrom, []string{settings.EmailTo}, message)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-result:
		return err
	}
}
