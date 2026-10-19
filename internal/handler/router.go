package handler

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"snippetbook/internal/auth"
	"snippetbook/internal/crypto"
	"snippetbook/internal/store"
	"snippetbook/internal/version"
)

func SetupRouter(st *store.DB, v *crypto.Vault) *gin.Engine {
	r := gin.Default()
	r.Use(func(c *gin.Context) {
		c.Header("X-App-Version", version.Version)
		c.Header("X-App-Commit", version.Commit)
		c.Next()
	})
	health := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		c.JSON(200, gin.H{
			"status":         "ok",
			"version":        version.Version,
			"commit":         version.Commit,
			"build_time":     version.BuildTime,
			"started_at":     version.StartedAt.Format(time.RFC3339),
			"uptime_seconds": int64(time.Since(version.StartedAt).Seconds()),
		})
	}
	// The public probe lets an external uptime checker verify the process.
	r.GET("/healthz", health)
	access := auth.CloudflareAccess()
	r.Use(access)
	api := r.Group("/api/v1")
	{
		ih := NewItemsHandler(st, v)
		serviceHandler := NewServicesHandler(st)
		mailHandler := NewMailHandler(st)

		api.GET("/health", health)

		api.GET("/items", ih.List)
		api.POST("/items", ih.Create)
		api.GET("/items/:id", ih.Get)
		api.PUT("/items/:id", ih.Update)
		api.DELETE("/items/:id", ih.Delete)
		api.POST("/items/:id/reveal", ih.Reveal)
		api.GET("/tags", ih.Tags)
		api.GET("/dashboard", access, serviceHandler.Dashboard)
		api.GET("/settings", access, serviceHandler.Settings)
		api.PUT("/settings", access, serviceHandler.UpdateSettings)
		api.GET("/services", access, serviceHandler.List)
		api.POST("/services", access, serviceHandler.Create)
		api.GET("/services/:id", access, serviceHandler.Detail)
		api.PUT("/services/:id", access, serviceHandler.Update)
		api.DELETE("/services/:id", access, serviceHandler.Delete)
		api.POST("/services/:id/check", access, serviceHandler.Check)
		api.GET("/services/:id/checks", access, serviceHandler.History)
		api.GET("/services/:id/metrics", access, serviceHandler.Metrics)
		api.POST("/services/:id/checks/clear", access, serviceHandler.ClearHistory)
		api.GET("/services/:id/runbooks", access, serviceHandler.Runbooks)
		api.POST("/services/:id/runbooks", access, serviceHandler.CreateRunbook)
		api.PUT("/runbooks/:id", access, serviceHandler.UpdateRunbook)
		api.DELETE("/runbooks/:id", access, serviceHandler.DeleteRunbook)
		api.POST("/runbooks/preview", access, serviceHandler.PreviewRunbook)
		api.GET("/reminders", access, serviceHandler.Reminders)
		api.POST("/reminders", access, serviceHandler.CreateReminder)
		api.PUT("/reminders/:id", access, serviceHandler.UpdateReminder)
		api.DELETE("/reminders/:id", access, serviceHandler.DeleteReminder)
		api.GET("/notifications", access, serviceHandler.Notifications)
		api.POST("/notifications/read", access, serviceHandler.ReadNotifications)
		api.GET("/notification-settings", access, serviceHandler.GetNotificationSettings)
		api.PUT("/notification-settings", access, serviceHandler.UpdateNotificationSettings)
		api.GET("/ai-settings", access, serviceHandler.GetAISettings)
		api.PUT("/ai-settings", access, serviceHandler.UpdateAISettings)
		api.POST("/notification-settings/test", access, serviceHandler.TestNotification)
		api.POST("/domain/check", access, mailHandler.Check)
		api.GET("/domain/checks", access, mailHandler.List)
		api.GET("/domain/checks/:id", access, mailHandler.Get)
		api.POST("/domain/checks/:id/ai-report", access, mailHandler.AIReport)
		api.DELETE("/domain/checks/:id", access, mailHandler.Delete)
		api.GET("/domain/monitors", access, mailHandler.ListMonitors)
		api.POST("/domain/monitors", access, mailHandler.CreateMonitor)
		api.PUT("/domain/monitors/:id", access, mailHandler.UpdateMonitor)
		api.DELETE("/domain/monitors/:id", access, mailHandler.DeleteMonitor)

	}

	// SPA assets (explicit, to avoid clashing with /api routes)
	r.GET("/index.html", noCacheFile("./web/index.html"))
	r.GET("/styles.css", noCacheFile("./web/styles.css"))
	r.GET("/app.js", noCacheFile("./web/app.js"))
	r.GET("/ops.js", noCacheFile("./web/ops.js"))
	r.GET("/ops.css", noCacheFile("./web/ops.css"))
	r.GET("/settings.js", noCacheFile("./web/settings.js"))
	r.GET("/mail.css", noCacheFile("./web/mail.css"))
	r.GET("/mail.js", noCacheFile("./web/mail.js"))
	r.GET("/uptime", access, noCacheFile("./web/ops.html"))
	r.GET("/uptime/settings", access, noCacheFile("./web/settings.html"))
	r.GET("/uptime/domain", access, noCacheFile("./web/mail.html"))
	r.GET("/uptime/domain/:id", access, noCacheFile("./web/mail.html"))
	r.GET("/", noCacheFile("./web/index.html"))
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(404, gin.H{"error": "API endpoint not found"})
			return
		}
		if c.Request.URL.Path == "/ops" || c.Request.URL.Path == "/ops.html" {
			c.Status(404)
			return
		}
		noCacheFile("./web/index.html")(c)
	})
	return r
}

func noCacheFile(path string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.File(path)
	}
}
