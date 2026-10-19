package auth

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
)

const (
	teamDomainEnv = "CLOUDFLARE_ACCESS_TEAM_DOMAIN"
	audienceEnv   = "CLOUDFLARE_ACCESS_AUDIENCE"
)

// CloudflareAccess returns middleware that validates Cloudflare Access JWTs.
// Authentication is disabled when both configuration variables are empty, which
// keeps local development convenient. A partially configured production setup
// fails closed with HTTP 503 instead of silently accepting requests.
func CloudflareAccess() gin.HandlerFunc {
	teamDomain := strings.TrimRight(strings.TrimSpace(os.Getenv(teamDomainEnv)), "/")
	if teamDomain != "" && !strings.HasPrefix(teamDomain, "https://") {
		teamDomain = "https://" + teamDomain
	}
	audience := strings.TrimSpace(os.Getenv(audienceEnv))
	if teamDomain == "" && audience == "" {
		return func(c *gin.Context) { c.Next() }
	}
	if teamDomain == "" || audience == "" {
		return func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "Cloudflare Access is incompletely configured"})
		}
	}

	keySet := oidc.NewRemoteKeySet(context.Background(), teamDomain+"/cdn-cgi/access/certs")
	verifier := oidc.NewVerifier(teamDomain, keySet, &oidc.Config{
		ClientID: audience,
	})

	return func(c *gin.Context) {
		token := strings.TrimSpace(c.GetHeader("Cf-Access-Jwt-Assertion"))
		if token == "" {
			if cookieToken, cookieErr := c.Cookie("CF_Authorization"); cookieErr == nil {
				token = strings.TrimSpace(cookieToken)
			}
		}
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Cloudflare Access token is required"})
			return
		}
		claims, err := verifier.Verify(c.Request.Context(), token)
		if err != nil {
			log.Printf("cloudflare access token verification failed: %v", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid Cloudflare Access token"})
			return
		}
		var identity struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		}
		if err := claims.Claims(&identity); err == nil {
			c.Set("access_email", identity.Email)
			c.Set("access_name", identity.Name)
		}
		c.Next()
	}
}
