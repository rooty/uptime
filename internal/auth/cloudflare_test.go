package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCloudflareAccessIsDisabledWithoutConfiguration(t *testing.T) {
	t.Setenv(teamDomainEnv, "")
	t.Setenv(audienceEnv, "")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CloudflareAccess())
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestCloudflareAccessRejectsPartialConfiguration(t *testing.T) {
	t.Setenv(teamDomainEnv, "https://team.cloudflareaccess.com")
	t.Setenv(audienceEnv, "")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CloudflareAccess())
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestCloudflareAccessRejectsMissingToken(t *testing.T) {
	t.Setenv(teamDomainEnv, "https://team.cloudflareaccess.com")
	t.Setenv(audienceEnv, "audience")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CloudflareAccess())
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
