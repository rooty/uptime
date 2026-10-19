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

	"snippetbook/internal/httpua"
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
	payload, err := json.Marshal(map[string]string{
		"chat_id":    settings.TelegramChatID,
		"text":       telegramMessage(item),
		"parse_mode": "HTML",
	})
	if err != nil {
		return fmt.Errorf("encode Telegram message: %w", err)
	}
	endpoint := "https://api.telegram.org/bot" + url.PathEscape(settings.TelegramBotToken) + "/sendMessage"
	return postJSON(ctx, endpoint, payload, nil)
}

func telegramMessage(item model.Notification) string {
	header := "ℹ️ УВЕДОМЛЕНИЕ"
	switch item.Kind {
	case "incident_started":
		header = "🚨 ПРОБЛЕМА НАЧАЛАСЬ"
	case "incident_resolved":
		header = "✅ ПРОБЛЕМА РЕШЕНА"
	case "incident_reminder":
		header = "⚠️ ПРОБЛЕМА ПРОДОЛЖАЕТСЯ"
	case "test_started":
		header = "🧪 ТЕСТ: ОШИБКА"
	case "test_resolved":
		header = "🧪 ТЕСТ: ВОССТАНОВЛЕНИЕ"
	}
	lines := make([]string, 0, strings.Count(item.Message, "\n")+1)
	if item.Title != "" {
		lines = append(lines, "<b>Событие:</b> "+escapeTelegramHTML(item.Title))
	}
	for _, line := range strings.Split(strings.TrimSpace(item.Message), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if separator := strings.Index(line, ": "); separator > 0 {
			lines = append(lines, "<b>"+escapeTelegramHTML(line[:separator+1])+"</b> "+escapeTelegramHTML(line[separator+2:]))
		} else {
			lines = append(lines, escapeTelegramHTML(line))
		}
	}
	return "<b>" + escapeTelegramHTML(header) + "</b>\n<blockquote>" + strings.Join(lines, "\n") + "</blockquote>"
}

func escapeTelegramHTML(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	return strings.ReplaceAll(value, ">", "&gt;")
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
	req.Header.Set("User-Agent", httpua.UserAgent)
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
