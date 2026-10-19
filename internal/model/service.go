package model

import "time"

type Service struct {
	ID                         int64     `json:"id"`
	Name                       string    `json:"name"`
	URL                        string    `json:"url"`
	CheckType                  string    `json:"check_type"`
	Environment                string    `json:"environment"`
	Description                string    `json:"description"`
	Tags                       []string  `json:"tags"`
	Enabled                    bool      `json:"enabled"`
	CheckInterval              string    `json:"check_interval"`
	RepeatNotificationsEnabled bool      `json:"repeat_notifications_enabled"`
	RepeatNotificationInterval string    `json:"repeat_notification_interval"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
	LastCheck                  *Check    `json:"last_check,omitempty"`
}

type Check struct {
	ID             int64          `json:"id"`
	ServiceID      int64          `json:"service_id"`
	Status         string         `json:"status"`
	HTTPStatus     int            `json:"http_status,omitempty"`
	ResponseTimeMS int64          `json:"response_time_ms"`
	Error          string         `json:"error,omitempty"`
	Diagnostics    map[string]any `json:"diagnostics,omitempty"`
	Metrics        *CheckMetrics  `json:"metrics,omitempty"`
	CheckedAt      time.Time      `json:"checked_at"`
	TLS            *TLSInfo       `json:"tls,omitempty"`
}

type CheckMetrics struct {
	DNSMS         int64 `json:"dns_ms,omitempty"`
	ConnectMS     int64 `json:"connect_ms,omitempty"`
	TLSMS         int64 `json:"tls_ms,omitempty"`
	TTFBMS        int64 `json:"ttfb_ms,omitempty"`
	PageSizeBytes int64 `json:"page_size_bytes,omitempty"`
}

type TLSInfo struct {
	Status        string    `json:"status"`
	Issuer        string    `json:"issuer,omitempty"`
	Hostname      string    `json:"hostname,omitempty"`
	ExpiresAt     time.Time `json:"expires_at,omitempty"`
	DaysRemaining int       `json:"days_remaining"`
	Error         string    `json:"error,omitempty"`
}

type Dashboard struct {
	Total         int                 `json:"total"`
	Online        int                 `json:"online"`
	Degraded      int                 `json:"degraded"`
	Offline       int                 `json:"offline"`
	RecentErrors  []Check             `json:"recent_errors"`
	ResponseTimes []ResponseTimePoint `json:"response_times"`
}

type ResponseTimePoint struct {
	ServiceID    int64     `json:"service_id"`
	ServiceName  string    `json:"service_name"`
	Milliseconds int64     `json:"milliseconds"`
	CheckedAt    time.Time `json:"checked_at"`
}

type MetricBucket struct {
	Bucket           string `json:"bucket"`
	TotalChecks      int64  `json:"total_checks"`
	OnlineChecks     int64  `json:"online_checks"`
	DegradedChecks   int64  `json:"degraded_checks"`
	OfflineChecks    int64  `json:"offline_checks"`
	ResponseSumMS    int64  `json:"response_sum_ms"`
	ResponseMinMS    int64  `json:"response_min_ms"`
	ResponseMaxMS    int64  `json:"response_max_ms"`
	DNSsumMS         int64  `json:"dns_sum_ms"`
	ConnectSumMS     int64  `json:"connect_sum_ms"`
	TLSSumMS         int64  `json:"tls_sum_ms"`
	TTFBSumMS        int64  `json:"ttfb_sum_ms"`
	PageSizeSumBytes int64  `json:"page_size_sum_bytes"`
	LastStatus       string `json:"last_status"`
	LastCheckedAt    string `json:"last_checked_at"`
}

type ServiceMetrics struct {
	Period       string         `json:"period"`
	Source       string         `json:"source"`
	From         time.Time      `json:"from"`
	To           time.Time      `json:"to"`
	TotalChecks  int64          `json:"total_checks"`
	OnlineChecks int64          `json:"online_checks"`
	Degraded     int64          `json:"degraded_checks"`
	Offline      int64          `json:"offline_checks"`
	Uptime       float64        `json:"uptime_percent"`
	Buckets      []MetricBucket `json:"buckets"`
}

type Settings struct {
	CheckTimeout      string `json:"check_timeout"`
	CheckHistoryLimit int    `json:"check_history_limit"`
}

type AISettings struct {
	Enabled          bool   `json:"enabled"`
	Provider         string `json:"provider"`
	BaseURL          string `json:"base_url"`
	Model            string `json:"model"`
	MaxTokens        int    `json:"max_tokens"`
	APIKeyConfigured bool   `json:"api_key_configured"`
}

type Runbook struct {
	ID           int64     `json:"id"`
	ServiceID    int64     `json:"service_id"`
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	RenderedHTML string    `json:"rendered_html,omitempty"`
}

type Reminder struct {
	ID          int64     `json:"id"`
	ServiceID   int64     `json:"service_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	DueAt       time.Time `json:"due_at"`
	Completed   bool      `json:"completed"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Notification struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	ServiceID int64     `json:"service_id,omitempty"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"created_at"`
}

type NotificationSettings struct {
	TelegramEnabled  bool   `json:"telegram_enabled"`
	TelegramBotToken string `json:"telegram_bot_token,omitempty"`
	TelegramChatID   string `json:"telegram_chat_id,omitempty"`
	SlackEnabled     bool   `json:"slack_enabled"`
	SlackWebhookURL  string `json:"slack_webhook_url,omitempty"`
	EmailEnabled     bool   `json:"email_enabled"`
	SMTPHost         string `json:"smtp_host,omitempty"`
	SMTPPort         int    `json:"smtp_port"`
	SMTPUsername     string `json:"smtp_username,omitempty"`
	SMTPPassword     string `json:"smtp_password,omitempty"`
	EmailFrom        string `json:"email_from,omitempty"`
	EmailTo          string `json:"email_to,omitempty"`
}

type Incident struct {
	ID              int64      `json:"id"`
	ServiceID       int64      `json:"service_id"`
	Status          string     `json:"status"`
	Problem         string     `json:"problem"`
	StartedAt       time.Time  `json:"started_at"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	DurationSeconds int64      `json:"duration_seconds"`
	LastNotifiedAt  *time.Time `json:"last_notified_at,omitempty"`
}
