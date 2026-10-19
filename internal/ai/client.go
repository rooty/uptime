package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"snippetbook/internal/model"
)

type Client struct {
	HTTP *http.Client
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	Reasoning   any           `json:"reasoning,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		FinishReason string      `json:"finish_reason"`
		Message      chatMessage `json:"message"`
	} `json:"choices"`
}

func (c Client) GenerateMailReport(ctx context.Context, settings model.AISettings, check model.MailCheck) (string, error) {
	if !settings.Enabled {
		return "", errors.New("AI-отчёты отключены в настройках")
	}
	apiKey := strings.TrimSpace(os.Getenv("AI_API_KEY"))
	if apiKey == "" {
		return "", errors.New("AI_API_KEY не настроен")
	}
	if strings.TrimSpace(settings.Model) == "" {
		return "", errors.New("AI model не указан в настройках")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(settings.BaseURL), "/")
	if baseURL == "" {
		return "", errors.New("AI base URL не указан")
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
		return "", errors.New("AI base URL должен быть HTTP(S)-адресом API, например https://api.openai.com/v1")
	}
	data, err := json.Marshal(check)
	if err != nil {
		return "", fmt.Errorf("marshal mail check: %w", err)
	}
	prompt := `Проанализируй результат проверки почтового домена. Составь подробный отчёт на русском языке.
Используй только факты из JSON и явно отмечай, если данных недостаточно. Обязательно включи разделы:
1. Краткий итог и оценка риска.
2. SPF: запись, lookup count, проблемы и точные рекомендации.
3. DKIM: selector, public key, проблемы и рекомендации.
4. DMARC: policy, alignment/reporting и рекомендации.
5. MX и MX TLS: конкретные hosts, порты, ошибки и последствия.
6. DNSBL/RBL: каждый IP и список, причина проблемы и безопасный порядок delisting.
7. RDAP: регистрация и срок действия.
8. Пошаговый план исправления с приоритетами.
Не отправляй письма и не выполняй действия автоматически. Не выдумывай blacklist-статус, DNS-записи или контакты.

Результат проверки в JSON:
` + string(data)

	requestBody := chatRequest{
		Model: settings.Model,
		Messages: []chatMessage{
			{Role: "system", Content: "Ты технический аналитик email-инфраструктуры. Отвечай структурированно и конкретно."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   settings.MaxTokens,
		Temperature: 0.2,
	}
	if strings.EqualFold(parsedURL.Hostname(), "openrouter.ai") || strings.HasSuffix(strings.ToLower(parsedURL.Hostname()), ".openrouter.ai") {
		requestBody.Reasoning = map[string]any{"effort": "none", "exclude": true}
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "SnippetBook-Mail-Diagnostics/1.0")
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 45 * time.Second}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("AI request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("%s", providerError(response.StatusCode, responseBody))
	}
	var result chatResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return "", fmt.Errorf("AI вернул не JSON. Проверьте Base URL: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", errors.New("AI returned no choices")
	}
	content := strings.TrimSpace(result.Choices[0].Message.Content)
	if content == "" {
		finishReason := strings.TrimSpace(result.Choices[0].FinishReason)
		if finishReason == "" {
			finishReason = "unknown"
		}
		return "", fmt.Errorf("AI returned an empty report (finish_reason=%s)", finishReason)
	}
	return content, nil
}

func providerError(status int, body []byte) string {
	text := strings.TrimSpace(string(body))
	if strings.Contains(strings.ToLower(text), "<!doctype html") || strings.Contains(strings.ToLower(text), "<html") {
		return fmt.Sprintf("AI provider returned HTTP %d with an HTML gateway page; проверьте Base URL и доступность /chat/completions", status)
	}
	if len(text) > 1000 {
		text = text[:1000] + "…"
	}
	return fmt.Sprintf("AI provider returned HTTP %d: %s", status, text)
}
