package notify

import (
	"strings"
	"testing"

	"snippetbook/internal/model"
)

func TestTelegramMessageHighlightsIncidentState(t *testing.T) {
	started := telegramMessage(model.Notification{
		Kind:    "incident_started",
		Title:   "SMTP — проблема началась",
		Message: "Сервис: SMTP\nОшибка: connection refused",
	})
	if !strings.Contains(started, "🚨 ПРОБЛЕМА НАЧАЛАСЬ") || !strings.Contains(started, "<b>Ошибка:</b>") {
		t.Fatalf("started Telegram message is not highlighted: %s", started)
	}

	resolved := telegramMessage(model.Notification{
		Kind:    "incident_resolved",
		Title:   "SMTP — проблема решена",
		Message: "Сервис: SMTP\nТекущий статус: online",
	})
	if !strings.Contains(resolved, "✅ ПРОБЛЕМА РЕШЕНА") || !strings.Contains(resolved, "<b>Текущий статус:</b>") {
		t.Fatalf("resolved Telegram message is not highlighted: %s", resolved)
	}
}
