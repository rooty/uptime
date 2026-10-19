package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"snippetbook/internal/model"
	"snippetbook/internal/notify"
)

type runbookRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type reminderRequest struct {
	ServiceID   int64  `json:"service_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	DueAt       string `json:"due_at"`
	Completed   bool   `json:"completed"`
}

func (h *ServicesHandler) Detail(c *gin.Context) {
	id, err := serviceID(c)
	if err != nil {
		return
	}
	service, err := h.Store.GetService(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}
	history, err := h.Store.CheckHistory(id, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	runbooks, err := h.Store.ListRunbooks(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for index := range runbooks {
		runbooks[index].RenderedHTML = renderMarkdown(runbooks[index].Content)
	}
	reminders, err := h.Store.ListReminders(true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	serviceReminders := make([]model.Reminder, 0)
	for _, reminder := range reminders {
		if reminder.ServiceID == id {
			serviceReminders = append(serviceReminders, reminder)
		}
	}
	incidents, err := h.Store.ListIncidents(id, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"service": service, "checks": history, "runbooks": runbooks, "reminders": serviceReminders, "incidents": incidents})
}

func (h *ServicesHandler) Runbooks(c *gin.Context) {
	id, err := serviceID(c)
	if err != nil {
		return
	}
	if _, err := h.Store.GetService(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}
	items, err := h.Store.ListRunbooks(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for index := range items {
		items[index].RenderedHTML = renderMarkdown(items[index].Content)
	}
	c.JSON(http.StatusOK, gin.H{"runbooks": items})
}

func (h *ServicesHandler) CreateRunbook(c *gin.Context) {
	id, err := serviceID(c)
	if err != nil {
		return
	}
	if _, err := h.Store.GetService(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}
	var req runbookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item := model.Runbook{ServiceID: id, Title: strings.TrimSpace(req.Title), Content: req.Content}
	if err := validateRunbook(item); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.Store.CreateRunbook(&item); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	item.RenderedHTML = renderMarkdown(item.Content)
	c.JSON(http.StatusCreated, item)
}

func (h *ServicesHandler) UpdateRunbook(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad runbook id"})
		return
	}
	item, err := h.Store.GetRunbook(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "runbook not found"})
		return
	}
	var req runbookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item.Title, item.Content = strings.TrimSpace(req.Title), req.Content
	if err := validateRunbook(item); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.Store.UpdateRunbook(&item); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	item.RenderedHTML = renderMarkdown(item.Content)
	c.JSON(http.StatusOK, item)
}

func (h *ServicesHandler) DeleteRunbook(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad runbook id"})
		return
	}
	if err := h.Store.DeleteRunbook(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ServicesHandler) PreviewRunbook(c *gin.Context) {
	var req runbookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Content) > 100000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content is too large"})
		return
	}
	preview := renderMarkdown(req.Content)
	c.JSON(http.StatusOK, gin.H{"html": preview})
}

func (h *ServicesHandler) Reminders(c *gin.Context) {
	items, err := h.Store.ListReminders(c.Query("all") == "true")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"reminders": items})
}

func (h *ServicesHandler) CreateReminder(c *gin.Context) {
	var req reminderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	due, err := time.Parse(time.RFC3339, req.DueAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "due_at must be RFC3339"})
		return
	}
	if strings.TrimSpace(req.Title) == "" || req.ServiceID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service_id and title are required"})
		return
	}
	item := model.Reminder{ServiceID: req.ServiceID, Title: strings.TrimSpace(req.Title), Description: strings.TrimSpace(req.Description), DueAt: due, Completed: req.Completed}
	if err := h.Store.CreateReminder(&item); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *ServicesHandler) UpdateReminder(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad reminder id"})
		return
	}
	var req reminderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	due, err := time.Parse(time.RFC3339, req.DueAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "due_at must be RFC3339"})
		return
	}
	item := model.Reminder{ID: id, ServiceID: req.ServiceID, Title: strings.TrimSpace(req.Title), Description: strings.TrimSpace(req.Description), DueAt: due, Completed: req.Completed}
	if item.ServiceID <= 0 || item.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service_id and title are required"})
		return
	}
	if err := h.Store.UpdateReminder(&item); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *ServicesHandler) DeleteReminder(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad reminder id"})
		return
	}
	if err := h.Store.DeleteReminder(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ServicesHandler) Notifications(c *gin.Context) {
	items, err := h.Store.ListNotifications(50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"notifications": items})
}

func (h *ServicesHandler) ReadNotifications(c *gin.Context) {
	if err := h.Store.MarkNotificationsRead(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ServicesHandler) GetNotificationSettings(c *gin.Context) {
	settings, err := h.Store.NotificationSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, settings)
}

func (h *ServicesHandler) GetAISettings(c *gin.Context) {
	settings, err := h.Store.AISettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	settings.APIKeyConfigured = strings.TrimSpace(os.Getenv("AI_API_KEY")) != ""
	c.JSON(http.StatusOK, settings)
}

func (h *ServicesHandler) UpdateAISettings(c *gin.Context) {
	var settings model.AISettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if settings.Provider == "" {
		settings.Provider = "openai-compatible"
	}
	if settings.Provider != "openai-compatible" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "поддерживается только openai-compatible provider"})
		return
	}
	if settings.BaseURL == "" || settings.Model == "" || settings.MaxTokens < 256 || settings.MaxTokens > 16000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "укажите base URL, model и max tokens от 256 до 16000"})
		return
	}
	if err := h.Store.UpdateAISettings(settings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	settings.APIKeyConfigured = strings.TrimSpace(os.Getenv("AI_API_KEY")) != ""
	c.JSON(http.StatusOK, settings)
}

func (h *ServicesHandler) UpdateNotificationSettings(c *gin.Context) {
	var settings model.NotificationSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateNotificationSettings(settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.Store.UpdateNotificationSettings(settings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, settings)
}

type notificationTestRequest struct {
	Channel string `json:"channel"`
	All     bool   `json:"all"`
}

func (h *ServicesHandler) TestNotification(c *gin.Context) {
	var request notificationTestRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	settings, err := h.Store.NotificationSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if request.All || request.Channel == "all" {
		if !settings.TelegramEnabled && !settings.SlackEnabled && !settings.EmailEnabled {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no notification channels are enabled"})
			return
		}
		if err := validateNotificationSettings(settings); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		tests := []model.Notification{
			{Kind: "test_started", Title: "Тест: проблема началась", Message: "Сервис: TEST\nСтатус: offline\nОшибка: тестовое сообщение об ошибке"},
			{Kind: "test_resolved", Title: "Тест: проблема решена", Message: "Сервис: TEST\nСтатус: online\nВосстановление: тестовое сообщение о восстановлении"},
		}
		var failures []string
		for _, item := range tests {
			if dispatchErr := notify.DispatchSettings(c.Request.Context(), settings, item); dispatchErr != nil {
				failures = append(failures, item.Kind+": "+dispatchErr.Error())
			}
		}
		if len(failures) > 0 {
			c.JSON(http.StatusBadGateway, gin.H{"error": strings.Join(failures, "; ")})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "sent", "messages": 2})
		return
	}
	testSettings := settings
	switch request.Channel {
	case "telegram":
		testSettings.TelegramEnabled, testSettings.SlackEnabled, testSettings.EmailEnabled = true, false, false
	case "slack":
		testSettings.TelegramEnabled, testSettings.SlackEnabled, testSettings.EmailEnabled = false, true, false
	case "email":
		testSettings.TelegramEnabled, testSettings.SlackEnabled, testSettings.EmailEnabled = false, false, true
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel must be telegram, slack or email"})
		return
	}
	if err := validateNotificationSettings(testSettings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item := model.Notification{Kind: "test", Title: "Local Ops Board test notification", Message: "Канал уведомлений работает."}
	if err := notify.DispatchSettings(c.Request.Context(), testSettings, item); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "sent", "channel": request.Channel})
}

func validateNotificationSettings(settings model.NotificationSettings) error {
	if settings.TelegramEnabled && (settings.TelegramBotToken == "" || settings.TelegramChatID == "") {
		return fmt.Errorf("Telegram requires bot token and chat ID")
	}
	if settings.SlackEnabled {
		parsed, err := url.Parse(settings.SlackWebhookURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("Slack webhook URL must be HTTPS")
		}
	}
	if settings.EmailEnabled && (settings.SMTPHost == "" || settings.EmailFrom == "" || settings.EmailTo == "" || settings.SMTPPort < 1 || settings.SMTPPort > 65535) {
		return fmt.Errorf("Email requires SMTP host, valid port, from and to")
	}
	return nil
}

func validateRunbook(item model.Runbook) error {
	if item.Title == "" {
		return &validationError{"title is required"}
	}
	if len(item.Content) > 100000 {
		return &validationError{"content is too large"}
	}
	return nil
}

type validationError struct{ message string }

func (e *validationError) Error() string { return e.message }

func parsePositiveID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, strconv.ErrSyntax
	}
	return id, nil
}
