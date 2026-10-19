package monitor

import (
	"bufio"
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/smtp"
	"net/url"
	"strings"
	"sync"
	"time"

	"snippetbook/internal/domaincheck"
	"snippetbook/internal/httpua"
	"snippetbook/internal/mailcheck"
	"snippetbook/internal/model"
	"snippetbook/internal/notify"
	"snippetbook/internal/store"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
)

type Monitor struct {
	Store  *store.DB
	client *http.Client
	mu     sync.Mutex
}

func New(db *store.DB) *Monitor {
	return &Monitor{Store: db}
}

func (m *Monitor) Run(ctx context.Context) {
	nextChecks := make(map[int64]time.Time)
	nextMailChecks := make(map[int64]time.Time)
	for {
		if ctx.Err() != nil {
			return
		}
		settings, err := m.Store.Settings()
		if err != nil {
			log.Printf("monitor settings: %v", err)
			if !wait(ctx, time.Second) {
				return
			}
			continue
		}
		timeout := parseDurationOr(settings.CheckTimeout, 10*time.Second)
		services, err := m.Store.ListEnabledServices()
		if err != nil {
			log.Printf("monitor services: %v", err)
			if !wait(ctx, time.Second) {
				return
			}
			continue
		}
		mailMonitors, err := m.Store.ListMailMonitors(true)
		if err != nil {
			log.Printf("monitor mail monitors: %v", err)
		} else {
			activeMail := make(map[int64]bool, len(mailMonitors))
			now := time.Now()
			for _, mailMonitor := range mailMonitors {
				activeMail[mailMonitor.ID] = true
				interval := parseDurationOr(mailMonitor.CheckInterval, 24*time.Hour)
				due, exists := nextMailChecks[mailMonitor.ID]
				if exists && now.Before(due) {
					continue
				}
				if !exists && mailMonitor.LastChecked != nil && now.Before(mailMonitor.LastChecked.Add(interval)) {
					nextMailChecks[mailMonitor.ID] = mailMonitor.LastChecked.Add(interval)
					continue
				}
				if err := m.checkMailMonitor(ctx, mailMonitor, timeout); err != nil {
					log.Printf("monitor mail %d: %v", mailMonitor.ID, err)
				}
				nextMailChecks[mailMonitor.ID] = time.Now().Add(interval)
			}
			for id := range nextMailChecks {
				if !activeMail[id] {
					delete(nextMailChecks, id)
				}
			}
		}
		active := make(map[int64]bool, len(services))
		now := time.Now()
		m.mu.Lock()
		for _, service := range services {
			active[service.ID] = true
			interval := parseDurationOr(service.CheckInterval, 30*time.Second)
			if due, exists := nextChecks[service.ID]; exists {
				if now.Before(due) {
					continue
				}
			} else if service.LastCheck != nil {
				due := service.LastCheck.CheckedAt.Add(interval)
				nextChecks[service.ID] = due
				if now.Before(due) {
					continue
				}
			}
			if err := m.checkService(ctx, service, timeout, settings.CheckHistoryLimit); err != nil {
				log.Printf("monitor service %d: %v", service.ID, err)
			}
			nextChecks[service.ID] = time.Now().Add(interval)
		}
		m.mu.Unlock()
		for id := range nextChecks {
			if !active[id] {
				delete(nextChecks, id)
			}
		}
		if !wait(ctx, time.Second) {
			return
		}
	}
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (m *Monitor) RunOnce(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	settings, err := m.Store.Settings()
	if err != nil {
		return err
	}
	timeout := parseDurationOr(settings.CheckTimeout, 10*time.Second)
	services, err := m.Store.ListEnabledServices()
	if err != nil {
		return err
	}
	for _, service := range services {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := m.checkService(ctx, service, timeout, settings.CheckHistoryLimit); err != nil {
			return err
		}
	}
	mailMonitors, err := m.Store.ListMailMonitors(true)
	if err != nil {
		return err
	}
	for _, mailMonitor := range mailMonitors {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := m.checkMailMonitor(ctx, mailMonitor, timeout); err != nil {
			return err
		}
	}
	return nil
}

func (m *Monitor) checkMailMonitor(ctx context.Context, monitor model.MailMonitor, timeout time.Duration) error {
	previous, err := m.Store.ListMailChecks(monitor.Domain, 1)
	if err != nil {
		return err
	}
	checker := mailcheck.New(timeout)
	result, err := checker.Check(ctx, monitor.Domain, monitor.DKIMSelector)
	if err != nil {
		return err
	}
	if len(previous) > 0 {
		result.Registration = domaincheck.CompareRegistration(previous[0].Result.Registration, result.Registration)
	}
	status, score := mailcheck.Overall(result)
	check := model.MailCheck{Domain: monitor.Domain, DKIMSelector: monitor.DKIMSelector, Status: status, Score: score, Result: result, CheckedAt: time.Now().UTC()}
	if err := m.Store.AddMailCheck(&check); err != nil {
		return err
	}
	if err := m.Store.PurgeMailChecks(monitor.Domain, model.MailCheckHistoryLimit); err != nil {
		return err
	}
	if monitor.LastStatus != "" && monitor.LastStatus != status {
		notification := model.Notification{Kind: "mail_status_change", Title: monitor.Domain + " mail status changed: " + status, Message: fmt.Sprintf("Домен: %s\nСтатус: %s\nScore: %d/100\nПроверено: %s", monitor.Domain, status, score, check.CheckedAt.Format(time.RFC3339)), CreatedAt: time.Now().UTC()}
		if err := m.Store.AddNotification(&notification); err != nil {
			log.Printf("mail notification: %v", err)
		}
		if err := notify.Dispatch(ctx, m.Store, notification); err != nil {
			log.Printf("mail external notification: %v", err)
		}
	}
	if monitor.RegistrationFingerprint != "" && result.Registration.Fingerprint != "" && monitor.RegistrationFingerprint != result.Registration.Fingerprint {
		notification := model.Notification{Kind: "domain_registration_changed", Title: monitor.Domain + " registration data changed", Message: fmt.Sprintf("Домен: %s\nИзменились данные RDAP/регистратора.\nТекущий статус: %s\nПроверено: %s", monitor.Domain, result.Registration.Status, check.CheckedAt.Format(time.RFC3339)), CreatedAt: time.Now().UTC()}
		if err := m.Store.AddNotification(&notification); err != nil {
			log.Printf("registration notification: %v", err)
		}
		if err := notify.Dispatch(ctx, m.Store, notification); err != nil {
			log.Printf("registration external notification: %v", err)
		}
	}
	return m.Store.UpdateMailMonitorResult(monitor.ID, status, score, check.CheckedAt, result.Registration)
}

// RunService performs an immediate check for a single service, regardless of its
// enabled flag. It is used after creating a service and by manual integrations.
func (m *Monitor) RunService(ctx context.Context, service model.Service) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	settings, err := m.Store.Settings()
	if err != nil {
		return err
	}
	timeout := parseDurationOr(settings.CheckTimeout, 10*time.Second)
	return m.checkService(ctx, service, timeout, settings.CheckHistoryLimit)
}

func (m *Monitor) checkService(ctx context.Context, service model.Service, timeout time.Duration, historyLimit int) error {
	check := Check(ctx, service, timeout, m.client)
	if IsTLSProtocol(service) {
		check.TLS = CheckTLS(ctx, service, timeout)
		if check.TLS.Status == "error" {
			check.Status = "tls_error"
			check.Error = check.TLS.Error
		} else if check.Status == "online" && check.TLS.Status == "expiring" {
			check.Status = "degraded"
		}
	}
	if err := m.Store.AddCheck(&check); err != nil {
		return err
	}
	if err := m.Store.PurgeChecks(service.ID, historyLimit); err != nil {
		return err
	}
	notification, incidentErr := ProcessIncident(m.Store, service, check)
	if incidentErr != nil {
		return incidentErr
	}
	if notification != nil {
		if err := m.Store.AddNotification(notification); err != nil {
			log.Printf("monitor notification: %v", err)
		}
		if err := notify.Dispatch(ctx, m.Store, *notification); err != nil {
			log.Printf("monitor external notification: %v", err)
		}
	}
	return nil
}

func BuildStatusNotification(service model.Service, check model.Check) model.Notification {
	var message strings.Builder
	message.WriteString("Сервис: " + service.Name + "\n")
	message.WriteString("URL: " + service.URL + "\n")
	message.WriteString("Окружение: " + service.Environment + "\n")
	message.WriteString("Статус: " + check.Status + "\n")
	message.WriteString(fmt.Sprintf("HTTP status: %d\n", check.HTTPStatus))
	message.WriteString(fmt.Sprintf("Response time: %d ms\n", check.ResponseTimeMS))
	message.WriteString("Интервал: " + service.CheckInterval + "\n")
	message.WriteString("Проверено: " + check.CheckedAt.UTC().Format(time.RFC3339) + "\n")
	if check.Error != "" {
		message.WriteString("Ошибка: " + check.Error + "\n")
	}
	if check.TLS != nil {
		message.WriteString("TLS status: " + check.TLS.Status + "\n")
		if check.TLS.Hostname != "" {
			message.WriteString("TLS hostname: " + check.TLS.Hostname + "\n")
		}
		if check.TLS.Issuer != "" {
			message.WriteString("TLS issuer: " + check.TLS.Issuer + "\n")
		}
		if !check.TLS.ExpiresAt.IsZero() {
			message.WriteString("TLS expires: " + check.TLS.ExpiresAt.UTC().Format(time.RFC3339) + "\n")
		}
		message.WriteString(fmt.Sprintf("TLS days remaining: %d\n", check.TLS.DaysRemaining))
		if check.TLS.Error != "" {
			message.WriteString("TLS error: " + check.TLS.Error + "\n")
		}
	}
	return model.Notification{Kind: "status_change", Title: service.Name + " status changed: " + check.Status, Message: message.String(), ServiceID: service.ID, CreatedAt: time.Now().UTC()}
}

func ProcessIncident(db *store.DB, service model.Service, check model.Check) (*model.Notification, error) {
	active, err := db.ActiveIncident(service.ID)
	if err != nil {
		return nil, err
	}
	if check.Status != "online" && active == nil {
		incident := model.Incident{ServiceID: service.ID, Problem: problemDescription(check), StartedAt: check.CheckedAt}
		if err := db.CreateIncident(&incident); err != nil {
			return nil, err
		}
		return incidentStartedNotification(service, check, incident), nil
	}
	if check.Status != "online" && active != nil && service.RepeatNotificationsEnabled {
		lastNotified := active.StartedAt
		if active.LastNotifiedAt != nil {
			lastNotified = *active.LastNotifiedAt
		}
		interval := parseDurationOr(service.RepeatNotificationInterval, 5*time.Minute)
		if !check.CheckedAt.Before(lastNotified.Add(interval)) {
			if err := db.MarkIncidentNotified(active.ID, check.CheckedAt); err != nil {
				return nil, err
			}
			active.LastNotifiedAt = &check.CheckedAt
			return incidentReminderNotification(service, check, *active), nil
		}
	}
	if check.Status == "online" && active != nil {
		if err := db.ResolveIncident(active, check.CheckedAt); err != nil {
			return nil, err
		}
		return incidentResolvedNotification(service, check, *active), nil
	}
	return nil, nil
}

func incidentReminderNotification(service model.Service, check model.Check, incident model.Incident) *model.Notification {
	duration := int64(check.CheckedAt.Sub(incident.StartedAt).Seconds())
	if duration < 0 {
		duration = 0
	}
	message := fmt.Sprintf("Сервис: %s\nURL: %s\nОкружение: %s\nПроблема продолжается\nКраткое описание: %s\nВремя начала: %s\nТекущая длительность: %s\nТекущий статус: %s\nHTTP status: %d\nResponse time: %d ms", service.Name, service.URL, service.Environment, incident.Problem, incident.StartedAt.UTC().Format(time.RFC3339), formatDuration(duration), check.Status, check.HTTPStatus, check.ResponseTimeMS)
	return &model.Notification{Kind: "incident_reminder", Title: service.Name + " — проблема продолжается", Message: message, ServiceID: service.ID, CreatedAt: time.Now().UTC()}
}

func problemDescription(check model.Check) string {
	if check.Error != "" {
		return check.Error
	}
	if check.HTTPStatus > 0 {
		return fmt.Sprintf("HTTP status %d", check.HTTPStatus)
	}
	return "service is not online"
}

func incidentStartedNotification(service model.Service, check model.Check, incident model.Incident) *model.Notification {
	message := fmt.Sprintf("Сервис: %s\nURL: %s\nОкружение: %s\nПроблема началась: %s\nСтатус: %s\nКраткое описание: %s\nHTTP status: %d\nResponse time: %d ms\nВремя начала: %s", service.Name, service.URL, service.Environment, incident.StartedAt.UTC().Format(time.RFC3339), check.Status, incident.Problem, check.HTTPStatus, check.ResponseTimeMS, incident.StartedAt.UTC().Format(time.RFC3339))
	return &model.Notification{Kind: "incident_started", Title: service.Name + " — проблема началась", Message: message, ServiceID: service.ID, CreatedAt: time.Now().UTC()}
}

func incidentResolvedNotification(service model.Service, check model.Check, incident model.Incident) *model.Notification {
	resolvedAt := check.CheckedAt.UTC()
	message := fmt.Sprintf("Сервис: %s\nURL: %s\nОкружение: %s\nПроблема решена\nКраткое описание: %s\nВремя начала: %s\nВремя решения: %s\nПродолжительность проблемы: %s\nТекущий статус: %s\nHTTP status: %d\nResponse time: %d ms", service.Name, service.URL, service.Environment, incident.Problem, incident.StartedAt.UTC().Format(time.RFC3339), resolvedAt.Format(time.RFC3339), formatDuration(incident.DurationSeconds), check.Status, check.HTTPStatus, check.ResponseTimeMS)
	return &model.Notification{Kind: "incident_resolved", Title: service.Name + " — проблема решена", Message: message, ServiceID: service.ID, CreatedAt: time.Now().UTC()}
}

func formatDuration(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%d сек.", seconds)
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%d мин. %d сек.", minutes, seconds%60)
	}
	hours := minutes / 60
	return fmt.Sprintf("%d ч. %d мин.", hours, minutes%60)
}

func parseDurationOr(value string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func CheckTLS(ctx context.Context, service model.Service, timeout time.Duration) *model.TLSInfo {
	result := &model.TLSInfo{Status: "error"}
	parsed, err := url.Parse(service.URL)
	if err != nil || parsed.Hostname() == "" {
		result.Error = "invalid HTTPS URL"
		return result
	}
	result.Hostname = parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	dialer := &tls.Dialer{Config: &tls.Config{ServerName: parsed.Hostname(), MinVersion: tls.VersionTLS12}}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := dialer.DialContext(dialCtx, "tcp", net.JoinHostPort(parsed.Hostname(), port))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer conn.Close()
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		result.Error = "TLS connection did not expose certificate state"
		return result
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		result.Error = "TLS peer returned no certificate"
		return result
	}
	certificate := state.PeerCertificates[0]
	result.Issuer = certificate.Issuer.String()
	result.ExpiresAt = certificate.NotAfter.UTC()
	result.DaysRemaining = int(time.Until(certificate.NotAfter).Hours() / 24)
	if result.DaysRemaining <= 21 {
		result.Status = "expiring"
	} else {
		result.Status = "ok"
	}
	return result
}

func IsTLSProtocol(service model.Service) bool {
	protocol := protocolForService(service)
	if protocol == "mysql" {
		if parsed, err := url.Parse(service.URL); err == nil {
			return strings.EqualFold(parsed.Query().Get("tls"), "true") || strings.EqualFold(parsed.Query().Get("sslmode"), "require")
		}
	}
	switch protocol {
	case "https", "smtps", "imaps", "pop3s", "rediss", "amqps":
		return true
	default:
		return false
	}
}

func protocolForService(service model.Service) string {
	if parsed, err := url.Parse(service.URL); err == nil && parsed.Scheme != "" {
		return strings.ToLower(parsed.Scheme)
	}
	return strings.ToLower(service.CheckType)
}

func ShouldNotify(previous, current *model.Check) bool {
	if previous == nil {
		return current.Status != "online"
	}
	return previous.Status != current.Status
}

func Check(ctx context.Context, service model.Service, timeout time.Duration, client *http.Client) model.Check {
	checkType := protocolForService(service)
	if checkType != "http" && checkType != "https" && checkType != "" {
		return checkProtocol(ctx, service, timeout, checkType)
	}
	started := time.Now()
	check := model.Check{ServiceID: service.ID, Status: "offline", CheckedAt: time.Now().UTC(), Diagnostics: serviceDiagnostics(service, checkType)}
	metrics := &model.CheckMetrics{}
	check.Metrics = metrics
	var dnsStart, connectStart, tlsStart, requestStart time.Time
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone: func(httptrace.DNSDoneInfo) {
			if !dnsStart.IsZero() {
				metrics.DNSMS = time.Since(dnsStart).Milliseconds()
			}
		},
		ConnectStart: func(_, _ string) { connectStart = time.Now() },
		ConnectDone: func(_, _ string, _ error) {
			if !connectStart.IsZero() {
				metrics.ConnectMS = time.Since(connectStart).Milliseconds()
			}
		},
		TLSHandshakeStart: func() { tlsStart = time.Now() },
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			if !tlsStart.IsZero() {
				metrics.TLSMS = time.Since(tlsStart).Milliseconds()
			}
		},
		GotFirstResponseByte: func() {
			if !requestStart.IsZero() {
				metrics.TTFBMS = time.Since(requestStart).Milliseconds()
			}
		},
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(requestCtx, trace), http.MethodGet, service.URL, nil)
	if err != nil {
		check.Diagnostics["phase"] = "request_create"
		check.Error = err.Error()
		return check
	}
	req.Header.Set("User-Agent", httpua.UserAgent)
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	requestStart = time.Now()
	resp, err := client.Do(req)
	check.ResponseTimeMS = time.Since(started).Milliseconds()
	if err != nil {
		check.Diagnostics["phase"] = "http_request"
		check.Error = err.Error()
		return check
	}
	defer resp.Body.Close()
	if size, readErr := io.Copy(io.Discard, resp.Body); readErr == nil {
		metrics.PageSizeBytes = size
	}
	check.HTTPStatus = resp.StatusCode
	check.Diagnostics["phase"] = "http_response"
	check.Diagnostics["http_status"] = resp.StatusCode
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		check.Status = "online"
	} else {
		check.Status = "degraded"
		check.Error = resp.Status
	}
	return check
}

func checkProtocol(ctx context.Context, service model.Service, timeout time.Duration, protocol string) model.Check {
	started := time.Now()
	check := model.Check{ServiceID: service.ID, Status: "offline", CheckedAt: time.Now().UTC(), Diagnostics: serviceDiagnostics(service, protocol)}
	parsed, err := url.Parse(service.URL)
	if err != nil || parsed.Hostname() == "" {
		check.Diagnostics["phase"] = "url_parse"
		check.Error = "invalid protocol URL"
		return check
	}
	switch protocol {
	case "mysql":
		return checkMySQL(ctx, service, timeout)
	case "postgres", "postgresql":
		return checkPostgreSQL(ctx, service, timeout)
	case "redis", "rediss":
		return checkRedis(ctx, service, timeout)
	case "rabbitmq", "amqp", "amqps":
		return checkRabbitMQ(ctx, service, timeout)
	}
	port := parsed.Port()
	if port == "" {
		port = defaultProtocolPort(protocol)
	}
	address := net.JoinHostPort(parsed.Hostname(), port)
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var conn net.Conn
	if protocol == "smtps" || protocol == "imaps" || protocol == "pop3s" || protocol == "rediss" || protocol == "amqps" {
		conn, err = (&tls.Dialer{Config: &tls.Config{ServerName: parsed.Hostname(), MinVersion: tls.VersionTLS12}}).DialContext(dialCtx, "tcp", address)
	} else {
		conn, err = (&net.Dialer{}).DialContext(dialCtx, "tcp", address)
	}
	check.ResponseTimeMS = time.Since(started).Milliseconds()
	if err != nil {
		check.Diagnostics["phase"] = "tcp_or_tls_connect"
		check.Error = err.Error()
		return check
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	reader := bufio.NewReader(conn)
	switch protocol {
	case "smtp", "smtps":
		client, clientErr := smtp.NewClient(conn, parsed.Hostname())
		if clientErr != nil {
			check.Diagnostics["phase"] = "smtp_handshake"
			check.Error = clientErr.Error()
			return check
		}
		defer client.Close()
		if clientErr = client.Hello("local-ops-board"); clientErr != nil {
			check.Diagnostics["phase"] = "smtp_hello"
			check.Error = clientErr.Error()
			return check
		}
		if parsed.User != nil {
			password, _ := parsed.User.Password()
			if clientErr = client.Auth(smtp.PlainAuth("", parsed.User.Username(), password, parsed.Hostname())); clientErr != nil {
				check.Diagnostics["phase"] = "smtp_auth"
				check.Status = "degraded"
				check.Error = clientErr.Error()
				return check
			}
		}
	case "imap", "imaps":
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			check.Diagnostics["phase"] = "imap_greeting"
			check.Error = readErr.Error()
			return check
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "*") || !strings.Contains(strings.ToUpper(trimmed), "OK") {
			check.Diagnostics["phase"] = "imap_greeting"
			check.Status = "degraded"
			check.Error = "invalid IMAP greeting"
			return check
		}
	case "pop3", "pop3s":
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			check.Diagnostics["phase"] = "pop3_greeting"
			check.Error = readErr.Error()
			return check
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToUpper(trimmed), "+OK") {
			check.Diagnostics["phase"] = "pop3_greeting"
			check.Status = "degraded"
			check.Error = "invalid POP3 greeting"
			return check
		}
	default:
		check.Status = "degraded"
		check.Error = "unsupported protocol"
		return check
	}
	check.Status = "online"
	return check
}

func defaultProtocolPort(protocol string) string {
	switch protocol {
	case "smtp":
		return "25"
	case "smtps":
		return "465"
	case "imap":
		return "143"
	case "imaps":
		return "993"
	case "pop3":
		return "110"
	case "pop3s":
		return "995"
	case "mysql":
		return "3306"
	case "redis", "rediss":
		return "6379"
	case "rabbitmq", "amqp":
		return "5672"
	case "amqps":
		return "5671"
	case "postgres", "postgresql":
		return "5432"
	default:
		return "0"
	}
}

func protocolCheck(service model.Service, started time.Time, err error) model.Check {
	protocol := protocolForService(service)
	check := model.Check{ServiceID: service.ID, Status: "offline", CheckedAt: time.Now().UTC(), ResponseTimeMS: time.Since(started).Milliseconds(), Diagnostics: serviceDiagnostics(service, protocol)}
	if err != nil {
		check.Diagnostics["phase"] = "connection_or_query"
		check.Error = err.Error()
		return check
	}
	check.Status = "online"
	return check
}

func serviceDiagnostics(service model.Service, protocol string) map[string]any {
	parsed, err := url.Parse(service.URL)
	details := map[string]any{"protocol": protocol, "endpoint": service.URL}
	if err == nil {
		details["hostname"] = parsed.Hostname()
		if parsed.Port() != "" {
			details["port"] = parsed.Port()
		} else {
			details["port"] = defaultProtocolPort(protocol)
		}
	}
	return details
}

func checkMySQL(ctx context.Context, service model.Service, timeout time.Duration) model.Check {
	started := time.Now()
	parsed, err := url.Parse(service.URL)
	if err != nil || parsed.Hostname() == "" {
		return protocolCheck(service, started, fmt.Errorf("invalid MySQL URL"))
	}
	user, password := "", ""
	if parsed.User != nil {
		user = parsed.User.Username()
		password, _ = parsed.User.Password()
	}
	database := strings.TrimPrefix(parsed.Path, "/")
	if database == "" {
		database = "mysql"
	}
	port := parsed.Port()
	if port == "" {
		port = "3306"
	}
	query := parsed.Query()
	query.Set("parseTime", "true")
	if strings.EqualFold(query.Get("tls"), "true") {
		query.Set("tls", "true")
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?%s", user, password, net.JoinHostPort(parsed.Hostname(), port), database, query.Encode())
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return protocolCheck(service, started, err)
	}
	defer db.Close()
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err = db.PingContext(pingCtx); err != nil {
		return protocolCheck(service, started, err)
	}
	var value int
	if err = db.QueryRowContext(pingCtx, "SELECT 1").Scan(&value); err != nil {
		return protocolCheck(service, started, err)
	}
	return protocolCheck(service, started, nil)
}

func checkPostgreSQL(ctx context.Context, service model.Service, timeout time.Duration) model.Check {
	started := time.Now()
	parsed, err := url.Parse(service.URL)
	if err != nil || parsed.Hostname() == "" {
		return protocolCheck(service, started, fmt.Errorf("invalid PostgreSQL URL"))
	}
	parsed.Scheme = "postgres"
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/postgres"
	}
	db, err := sql.Open("pgx", parsed.String())
	if err != nil {
		return protocolCheck(service, started, err)
	}
	defer db.Close()
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err = db.PingContext(pingCtx); err != nil {
		return protocolCheck(service, started, err)
	}
	var value int
	if err = db.QueryRowContext(pingCtx, "SELECT 1").Scan(&value); err != nil {
		return protocolCheck(service, started, err)
	}
	return protocolCheck(service, started, nil)
}

func checkRedis(ctx context.Context, service model.Service, timeout time.Duration) model.Check {
	started := time.Now()
	parsed, err := url.Parse(service.URL)
	if err != nil || parsed.Hostname() == "" {
		return protocolCheck(service, started, fmt.Errorf("invalid Redis URL"))
	}
	redisURL := service.URL
	if parsed.Scheme == "rediss" {
		redisURL = "rediss" + strings.TrimPrefix(service.URL, "rediss")
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return protocolCheck(service, started, err)
	}
	options.DialTimeout = timeout
	options.ReadTimeout = timeout
	options.WriteTimeout = timeout
	client := redis.NewClient(options)
	defer client.Close()
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err = client.Ping(pingCtx).Err(); err != nil {
		return protocolCheck(service, started, err)
	}
	return protocolCheck(service, started, nil)
}

func checkRabbitMQ(ctx context.Context, service model.Service, timeout time.Duration) model.Check {
	started := time.Now()
	parsed, err := url.Parse(service.URL)
	if err != nil || parsed.Hostname() == "" {
		return protocolCheck(service, started, fmt.Errorf("invalid RabbitMQ URL"))
	}
	if parsed.Scheme == "rabbitmq" {
		parsed.Scheme = "amqp"
	}
	if parsed.Scheme == "amqps" && parsed.Port() == "" {
		parsed.Host = net.JoinHostPort(parsed.Hostname(), "5671")
	}
	dialer := &net.Dialer{Timeout: timeout}
	config := amqp091.Config{Dial: func(network, address string) (net.Conn, error) {
		dialCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return dialer.DialContext(dialCtx, network, address)
	}}
	connection, err := amqp091.DialConfig(parsed.String(), config)
	if err != nil {
		return protocolCheck(service, started, err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		return protocolCheck(service, started, err)
	}
	defer channel.Close()
	return protocolCheck(service, started, nil)
}
