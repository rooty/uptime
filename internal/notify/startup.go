package notify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"snippetbook/internal/model"
	"snippetbook/internal/store"
)

// StartupInfo identifies the running release, not the outcome of the CI workflow.
type StartupInfo struct {
	Version   string
	Commit    string
	BuildTime string
	StartedAt time.Time
	Address   string
}

// Startup sends exactly one best-effort message to the configured Telegram chat.
// Call it asynchronously after binding the application's HTTP listener.
func Startup(ctx context.Context, db *store.DB, info StartupInfo) error {
	return startup(ctx, db, info, sendTelegram)
}

func startup(ctx context.Context, db *store.DB, info StartupInfo, send func(context.Context, model.NotificationSettings, model.Notification) error) error {
	settings, err := db.NotificationSettings()
	if err != nil {
		return fmt.Errorf("read startup notification settings: %w", err)
	}
	if !settings.TelegramEnabled {
		return nil
	}
	item := model.Notification{
		Kind:      "application_started",
		Title:     "Local Ops Board запущен",
		CreatedAt: info.StartedAt.UTC(),
		Message: fmt.Sprintf("Состояние: HTTP-сервер запущен\nВерсия: %s\nCommit: %s\nВремя сборки: %s\nВремя запуска (UTC): %s\nHTTP listener: %s",
			info.Version, info.Commit, info.BuildTime, info.StartedAt.UTC().Format(time.RFC3339), info.Address),
	}
	// Preserve the startup event in the existing notification history. A storage
	// error must not suppress delivery if the settings were successfully read.
	var failures []error
	if err := db.AddNotification(&item); err != nil {
		failures = append(failures, fmt.Errorf("save startup notification: %w", err))
	}
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := send(sendCtx, settings, item); err != nil {
		failures = append(failures, fmt.Errorf("startup telegram: %w", err))
	}
	return errors.Join(failures...)
}
