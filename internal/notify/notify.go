package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

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
	client := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return sendTelegramWithClient(ctx, client, settings, item)
}

func sendTelegramWithClient(ctx context.Context, client *http.Client, settings model.NotificationSettings, item model.Notification) error {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("create Telegram request: %s", redactTelegramError(err.Error(), settings.TelegramBotToken))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", httpua.UserAgent)
	response, err := client.Do(req)
	if err != nil {
		// net/http includes the full bot-token URL in url.Error.Error().
		var requestError *url.Error
		if errors.As(err, &requestError) {
			err = requestError.Err
		}
		return fmt.Errorf("Telegram HTTP request: %s", redactTelegramError(err.Error(), settings.TelegramBotToken))
	}
	defer response.Body.Close()
	var result struct {
		OK          bool   `json:"ok"`
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&result)
	if response.StatusCode < 200 || response.StatusCode >= 300 || (decodeErr == nil && !result.OK) {
		message := fmt.Sprintf("Telegram HTTP %d, API error %d", response.StatusCode, result.ErrorCode)
		if result.Description != "" {
			message += ": " + redactTelegramError(result.Description, settings.TelegramBotToken)
		}
		if result.Parameters.RetryAfter > 0 {
			message += fmt.Sprintf(" (retry_after=%ds)", result.Parameters.RetryAfter)
		}
		return errors.New(message)
	}
	if decodeErr != nil {
		return fmt.Errorf("decode Telegram response: %s", redactTelegramError(decodeErr.Error(), settings.TelegramBotToken))
	}
	return nil
}

func redactTelegramError(message, token string) string {
	if token != "" {
		message = strings.ReplaceAll(message, url.PathEscape(token), "[REDACTED]")
		message = strings.ReplaceAll(message, token, "[REDACTED]")
	}
	return message
}

func telegramMessage(item model.Notification) string {
	header := "ℹ️ УВЕДОМЛЕНИЕ"
	switch item.Kind {
	case "application_started":
		header = "🚀 СЕРВИС ЗАПУЩЕН"
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
