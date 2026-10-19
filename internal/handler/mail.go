package handler

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"snippetbook/internal/ai"
	"snippetbook/internal/domaincheck"
	"snippetbook/internal/mailcheck"
	"snippetbook/internal/model"
	"snippetbook/internal/store"
)

type MailHandler struct {
	Store *store.DB
}

func (h *MailHandler) AIReport(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad mail check id"})
		return
	}
	check, err := h.Store.GetMailCheck(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "mail check not found"})
		return
	}
	settings, err := h.Store.AISettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	report, err := (ai.Client{}).GenerateMailReport(c.Request.Context(), settings, check)
	aiUpdatedAt := time.Now().UTC()
	if err != nil {
		log.Printf("AI mail report failed: check_id=%d domain=%s error=%v", check.ID, check.Domain, err)
		if saveErr := h.Store.UpdateMailCheckAI(check.ID, "", err.Error(), settings.Model, aiUpdatedAt); saveErr != nil {
			log.Printf("AI mail report error could not be saved: check_id=%d error=%v", check.ID, saveErr)
		}
		c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
		return
	}
	if saveErr := h.Store.UpdateMailCheckAI(check.ID, report, "", settings.Model, aiUpdatedAt); saveErr != nil {
		log.Printf("AI mail report could not be saved: check_id=%d error=%v", check.ID, saveErr)
	}
	c.JSON(http.StatusOK, gin.H{"report": report, "model": settings.Model, "generated_at": time.Now().UTC()})
}

type mailCheckRequest struct {
	Domain       string `json:"domain"`
	DKIMSelector string `json:"dkim_selector"`
}

type mailMonitorRequest struct {
	Domain        string `json:"domain"`
	DKIMSelector  string `json:"dkim_selector"`
	CheckInterval string `json:"check_interval"`
	Enabled       *bool  `json:"enabled"`
}

func NewMailHandler(db *store.DB) *MailHandler { return &MailHandler{Store: db} }

func (h *MailHandler) Check(c *gin.Context) {
	var request mailCheckRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(request.Domain), "."))
	checker := mailcheck.New(10 * time.Second)
	result, err := checker.Check(c.Request.Context(), domain, strings.TrimSpace(request.DKIMSelector))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	previous, err := h.Store.ListMailChecks(domain, 1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(previous) > 0 {
		result.Registration = domaincheck.CompareRegistration(previous[0].Result.Registration, result.Registration)
	}
	status, score := mailcheck.Overall(result)
	check := model.MailCheck{Domain: domain, DKIMSelector: strings.TrimSpace(request.DKIMSelector), Status: status, Score: score, Result: result}
	if err := h.Store.AddMailCheck(&check); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.Store.PurgeMailChecks(check.Domain, model.MailCheckHistoryLimit); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, check)
}

func (h *MailHandler) List(c *gin.Context) {
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(c.Query("domain")), "."))
	if domain == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "domain is required"})
		return
	}
	if err := h.Store.PurgeMailChecks(domain, model.MailCheckHistoryLimit); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	checks, err := h.Store.ListMailChecks(domain, model.MailCheckHistoryLimit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"checks": checks})
}

func (h *MailHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad mail check id"})
		return
	}
	check, err := h.Store.GetMailCheck(id)
	if err != nil {
		if store.IsMailCheckNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "mail check not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, check)
}

func (h *MailHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad mail check id"})
		return
	}
	if err := h.Store.DeleteMailCheck(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *MailHandler) ListMonitors(c *gin.Context) {
	monitors, err := h.Store.ListMailMonitors(false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"monitors": monitors})
}

func (h *MailHandler) CreateMonitor(c *gin.Context) {
	var request mailMonitorRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	domain := mailcheck.NormalizeDomain(request.Domain)
	if err := mailcheck.ValidateDomain(domain); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	interval := strings.TrimSpace(request.CheckInterval)
	if interval == "" {
		interval = "1h"
	}
	parsed, err := time.ParseDuration(interval)
	if err != nil || parsed < time.Minute {
		c.JSON(http.StatusBadRequest, gin.H{"error": "check_interval must be at least 1m"})
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	monitor := model.MailMonitor{Domain: domain, DKIMSelector: strings.TrimSpace(request.DKIMSelector), CheckInterval: interval, Enabled: enabled}
	if err := h.Store.CreateMailMonitor(&monitor); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, monitor)
}

func (h *MailHandler) UpdateMonitor(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad mail monitor id"})
		return
	}
	monitor, err := h.Store.GetMailMonitor(id)
	if err != nil {
		if store.IsMailCheckNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "mail monitor not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	var request mailMonitorRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if request.DKIMSelector != "" {
		monitor.DKIMSelector = strings.TrimSpace(request.DKIMSelector)
	}
	if request.Domain != "" {
		domain := mailcheck.NormalizeDomain(request.Domain)
		if err := mailcheck.ValidateDomain(domain); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		monitor.Domain = domain
	}
	if request.CheckInterval != "" {
		parsed, parseErr := time.ParseDuration(request.CheckInterval)
		if parseErr != nil || parsed < time.Minute {
			c.JSON(http.StatusBadRequest, gin.H{"error": "check_interval must be at least 1m"})
			return
		}
		monitor.CheckInterval = strings.TrimSpace(request.CheckInterval)
	}
	if request.Enabled != nil {
		monitor.Enabled = *request.Enabled
	}
	if err := h.Store.UpdateMailMonitor(&monitor); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, monitor)
}

func (h *MailHandler) DeleteMonitor(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad mail monitor id"})
		return
	}
	if err := h.Store.DeleteMailMonitor(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
