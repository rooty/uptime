package handler

import (
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"snippetbook/internal/model"
	"snippetbook/internal/monitor"
	"snippetbook/internal/notify"
	"snippetbook/internal/store"
)

type ServicesHandler struct {
	Store *store.DB
}

func NewServicesHandler(db *store.DB) *ServicesHandler {
	return &ServicesHandler{Store: db}
}

type serviceRequest struct {
	Name                       string   `json:"name"`
	URL                        string   `json:"url"`
	CheckType                  string   `json:"check_type"`
	Environment                string   `json:"environment"`
	Description                string   `json:"description"`
	Tags                       []string `json:"tags"`
	Enabled                    *bool    `json:"enabled"`
	CheckInterval              string   `json:"check_interval"`
	RepeatNotificationsEnabled bool     `json:"repeat_notifications_enabled"`
	RepeatNotificationInterval string   `json:"repeat_notification_interval"`
}

func (h *ServicesHandler) List(c *gin.Context) {
	services, err := h.filteredServices(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"services": services})
}

func (h *ServicesHandler) filteredServices(c *gin.Context) ([]model.Service, error) {
	services, err := h.Store.ListServices()
	if err != nil {
		return nil, err
	}
	environment := strings.TrimSpace(c.Query("environment"))
	tag := strings.ToLower(strings.TrimSpace(c.Query("tag")))
	status := strings.TrimSpace(c.Query("status"))
	checkType := strings.TrimSpace(c.Query("check_type"))
	search := strings.ToLower(strings.TrimSpace(c.Query("search")))
	filtered := services[:0]
	for _, service := range services {
		if environment != "" && service.Environment != environment {
			continue
		}
		if tag != "" && !containsTag(service.Tags, tag) {
			continue
		}
		if checkType != "" && canonicalCheckType(service.CheckType) != canonicalCheckType(checkType) {
			continue
		}
		if search != "" && !serviceMatchesSearch(service, search) {
			continue
		}
		if status != "" {
			current := "offline"
			if service.LastCheck != nil {
				current = service.LastCheck.Status
			}
			if current != status {
				continue
			}
		}
		filtered = append(filtered, service)
	}
	switch c.Query("sort") {
	case "name", "":
		sort.SliceStable(filtered, func(i, j int) bool { return strings.ToLower(filtered[i].Name) < strings.ToLower(filtered[j].Name) })
	case "response_time":
		sort.SliceStable(filtered, func(i, j int) bool { return responseTime(filtered[i]) < responseTime(filtered[j]) })
	case "status":
		sort.SliceStable(filtered, func(i, j int) bool { return serviceStatus(filtered[i]) < serviceStatus(filtered[j]) })
	case "last_check":
		sort.SliceStable(filtered, func(i, j int) bool { return lastCheckTime(filtered[i]).After(lastCheckTime(filtered[j])) })
	}
	return filtered, nil
}

func serviceMatchesSearch(service model.Service, search string) bool {
	if strings.Contains(strings.ToLower(service.Name), search) || strings.Contains(strings.ToLower(service.URL), search) || strings.Contains(strings.ToLower(service.Description), search) {
		return true
	}
	for _, tag := range service.Tags {
		if strings.Contains(strings.ToLower(tag), search) {
			return true
		}
	}
	return false
}

func lastCheckTime(service model.Service) time.Time {
	if service.LastCheck == nil {
		return time.Time{}
	}
	return service.LastCheck.CheckedAt
}

func (h *ServicesHandler) Dashboard(c *gin.Context) {
	dashboard, err := h.Store.Dashboard()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, dashboard)
}

func (h *ServicesHandler) Settings(c *gin.Context) {
	settings, err := h.Store.Settings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, settings)
}

func (h *ServicesHandler) UpdateSettings(c *gin.Context) {
	var settings model.Settings
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	timeout, timeoutErr := time.ParseDuration(settings.CheckTimeout)
	if timeoutErr != nil || timeout <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "check_timeout must be a positive duration, for example 10s"})
		return
	}
	if settings.CheckHistoryLimit < 1 || settings.CheckHistoryLimit > 100000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "check_history_limit must be between 1 and 100000"})
		return
	}
	if err := h.Store.UpdateSettings(settings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, settings)
}

func (h *ServicesHandler) Create(c *gin.Context) {
	var req serviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	service, err := h.serviceFromRequest(req, 0)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.Store.CreateService(&service); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := monitor.New(h.Store).RunService(c.Request.Context(), service); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "service created, but initial check failed: " + err.Error()})
		return
	}
	service.LastCheck, _ = h.Store.LastCheck(service.ID)
	c.JSON(http.StatusCreated, service)
}

func (h *ServicesHandler) Update(c *gin.Context) {
	id, err := serviceID(c)
	if err != nil {
		return
	}
	var req serviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	service, err := h.serviceFromRequest(req, id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.Store.GetService(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}
	if err := h.Store.UpdateService(&service); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, service)
}

func (h *ServicesHandler) Delete(c *gin.Context) {
	id, err := serviceID(c)
	if err != nil {
		return
	}
	if err := h.Store.DeleteService(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ServicesHandler) Check(c *gin.Context) {
	id, err := serviceID(c)
	if err != nil {
		return
	}
	service, err := h.Store.GetService(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}
	settings, err := h.Store.Settings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	timeout, err := time.ParseDuration(settings.CheckTimeout)
	if err != nil || timeout <= 0 {
		timeout = 10 * time.Second
	}
	check := monitor.Check(c.Request.Context(), service, timeout, nil)
	if monitor.IsTLSProtocol(service) {
		check.TLS = monitor.CheckTLS(c.Request.Context(), service, timeout)
		if check.TLS.Status == "error" {
			check.Status = "tls_error"
			check.Error = check.TLS.Error
		} else if check.Status == "online" && check.TLS.Status == "expiring" {
			check.Status = "degraded"
		}
	}
	if err := h.Store.AddCheck(&check); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	notification, incidentErr := monitor.ProcessIncident(h.Store, service, check)
	if incidentErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": incidentErr.Error()})
		return
	}
	if notification != nil {
		if err := h.Store.AddNotification(notification); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := notify.Dispatch(c.Request.Context(), h.Store, *notification); err != nil {
			c.Header("X-Notification-Error", err.Error())
		}
	}
	c.JSON(http.StatusOK, check)
}

func (h *ServicesHandler) History(c *gin.Context) {
	id, err := serviceID(c)
	if err != nil {
		return
	}
	if _, err := h.Store.GetService(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}
	checks, err := h.Store.CheckHistory(id, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"checks": checks})
}

func (h *ServicesHandler) Metrics(c *gin.Context) {
	id, err := serviceID(c)
	if err != nil {
		return
	}
	metrics, err := h.Store.ServiceMetrics(id, c.DefaultQuery("period", "24h"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, metrics)
}

func (h *ServicesHandler) ClearHistory(c *gin.Context) {
	id, err := serviceID(c)
	if err != nil {
		return
	}
	if _, err := h.Store.GetService(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -30)
	if value := c.Query("before"); value != "" {
		parsed, parseErr := time.Parse(time.RFC3339, value)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "before must be RFC3339"})
			return
		}
		cutoff = parsed
	}
	if err := h.Store.ClearChecksBefore(id, cutoff); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ServicesHandler) serviceFromRequest(req serviceRequest, id int64) (model.Service, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return model.Service{}, errors.New("name is required")
	}
	parsed, err := url.ParseRequestURI(strings.TrimSpace(req.URL))
	if err != nil || parsed.Host == "" {
		return model.Service{}, errors.New("url must be an absolute protocol URL")
	}
	checkType := strings.ToLower(strings.TrimSpace(req.CheckType))
	if checkType == "" {
		checkType = canonicalCheckType(strings.ToLower(parsed.Scheme))
	}
	checkType = canonicalCheckType(checkType)
	if !supportedCheckType(checkType) {
		return model.Service{}, errors.New("unsupported check_type")
	}
	if !checkTypeMatchesScheme(checkType, strings.ToLower(parsed.Scheme)) {
		return model.Service{}, errors.New("URL scheme does not match check_type")
	}
	environment := strings.TrimSpace(req.Environment)
	if environment == "" {
		environment = "production"
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	checkInterval := strings.TrimSpace(req.CheckInterval)
	if checkInterval == "" {
		checkInterval = "30s"
	}
	parsedInterval, intervalErr := time.ParseDuration(checkInterval)
	if intervalErr != nil || parsedInterval <= 0 {
		return model.Service{}, errors.New("check_interval must be a positive duration, for example 30s")
	}
	repeatInterval := strings.TrimSpace(req.RepeatNotificationInterval)
	if repeatInterval == "" {
		repeatInterval = "5m"
	}
	parsedRepeat, repeatErr := time.ParseDuration(repeatInterval)
	if repeatErr != nil || parsedRepeat <= 0 {
		return model.Service{}, errors.New("repeat_notification_interval must be a positive duration, for example 5m")
	}
	return model.Service{ID: id, Name: name, URL: parsed.String(), CheckType: checkType, Environment: environment,
		Description: strings.TrimSpace(req.Description), Tags: normalizeTags(req.Tags), Enabled: enabled, CheckInterval: checkInterval, RepeatNotificationsEnabled: req.RepeatNotificationsEnabled, RepeatNotificationInterval: repeatInterval}, nil
}

func supportedCheckType(checkType string) bool {
	switch checkType {
	case "http", "smtp", "imap", "pop3", "mysql", "redis", "rabbitmq", "postgres":
		return true
	default:
		return false
	}
}

func canonicalCheckType(checkType string) string {
	switch checkType {
	case "https":
		return "http"
	case "smtps":
		return "smtp"
	case "imaps":
		return "imap"
	case "pop3s":
		return "pop3"
	case "rediss":
		return "redis"
	case "amqp", "amqps":
		return "rabbitmq"
	case "postgresql":
		return "postgres"
	default:
		return checkType
	}
}

func checkTypeMatchesScheme(checkType, scheme string) bool {
	switch checkType {
	case "http":
		return scheme == "http" || scheme == "https"
	case "smtp":
		return scheme == "smtp" || scheme == "smtps"
	case "imap":
		return scheme == "imap" || scheme == "imaps"
	case "pop3":
		return scheme == "pop3" || scheme == "pop3s"
	case "mysql", "redis":
		return scheme == checkType || (checkType == "redis" && scheme == "rediss")
	case "rabbitmq":
		return scheme == "rabbitmq" || scheme == "amqp" || scheme == "amqps"
	case "postgres":
		return scheme == "postgres" || scheme == "postgresql"
	default:
		return false
	}
}

func serviceID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad service id"})
		return 0, errors.New("bad service id")
	}
	return id, nil
}

func normalizeTags(tags []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag != "" && !seen[tag] {
			seen[tag] = true
			result = append(result, tag)
		}
	}
	return result
}

func containsTag(tags []string, wanted string) bool {
	for _, tag := range tags {
		if strings.ToLower(tag) == wanted {
			return true
		}
	}
	return false
}

func serviceStatus(service model.Service) string {
	if service.LastCheck == nil {
		return "offline"
	}
	return service.LastCheck.Status
}

func responseTime(service model.Service) int64 {
	if service.LastCheck == nil {
		return 1<<63 - 1
	}
	return service.LastCheck.ResponseTimeMS
}
