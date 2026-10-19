package domaincheck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"snippetbook/internal/httpua"
	"snippetbook/internal/model"
)

type Client struct {
	HTTP      *http.Client
	Endpoints []string
}

func (c Client) Check(ctx context.Context, domain string) (model.DomainRegistration, error) {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 5 * time.Second}
	}
	if len(c.Endpoints) == 0 {
		c.Endpoints = rdapEndpoints(domain)
	}
	baseContext := ctx
	if ctx.Err() != nil {
		baseContext = context.WithoutCancel(ctx)
	}
	var failures []string
	for _, endpoint := range c.Endpoints {
		for attempt := 1; attempt <= 2; attempt++ {
			result, statusCode, err := c.request(baseContext, endpoint, domain)
			if err == nil && statusCode >= 200 && statusCode < 300 {
				return parseRegistration(result)
			}
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s (attempt %d): %v", endpoint, attempt, err))
			} else {
				failures = append(failures, fmt.Sprintf("%s (attempt %d): HTTP %d", endpoint, attempt, statusCode))
				if statusCode >= 400 && statusCode < 500 {
					break
				}
			}
		}
	}
	message := "RDAP lookup failed: " + strings.Join(failures, "; ")
	return model.DomainRegistration{Status: "error", Message: message}, fmt.Errorf("%s", message)
}

func rdapEndpoints(domain string) []string {
	if strings.HasSuffix(strings.ToLower(strings.TrimSuffix(domain, ".")), ".ua") {
		return []string{"https://rdap.eunic.net.ua/domain/", "https://rdap.org/domain/"}
	}
	return []string{"https://rdap.org/domain/"}
}

func (c Client) request(ctx context.Context, endpoint, domain string) ([]byte, int, error) {
	requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestContext, http.MethodGet, endpoint+domain, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/rdap+json, application/json")
	req.Header.Set("User-Agent", httpua.UserAgent)
	response, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, response.StatusCode, err
	}
	return body, response.StatusCode, nil
}

func parseRegistration(data []byte) (model.DomainRegistration, error) {
	var payload struct {
		Handle  string `json:"handle"`
		LdhName string `json:"ldhName"`
		Events  []struct {
			EventAction string `json:"eventAction"`
			EventDate   string `json:"eventDate"`
		} `json:"events"`
		Entities []struct {
			Roles      []string `json:"roles"`
			VCardArray []any    `json:"vcardArray"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return model.DomainRegistration{Status: "error", Message: err.Error()}, err
	}
	result := model.DomainRegistration{Status: "ok"}
	for _, event := range payload.Events {
		if event.EventAction != "expiration" && event.EventAction != "registration expiration" {
			continue
		}
		if value, err := time.Parse(time.RFC3339, event.EventDate); err == nil {
			result.ExpiresAt = &value
			result.DaysRemaining = int(time.Until(value).Hours() / 24)
		}
	}
	for _, entity := range payload.Entities {
		for _, role := range entity.Roles {
			if role == "registrar" {
				result.Registrar = entityName(entity.VCardArray)
			}
		}
	}
	if result.ExpiresAt == nil {
		result.Message = "RDAP did not provide a domain expiration date"
	}
	fingerprintInput, _ := json.Marshal(payload)
	hash := sha256.Sum256(fingerprintInput)
	result.Fingerprint = hex.EncodeToString(hash[:])
	return result, nil
}

func entityName(value []any) string {
	if len(value) >= 2 {
		if properties, ok := value[1].([]any); ok {
			for _, rawProperty := range properties {
				property, ok := rawProperty.([]any)
				if !ok || len(property) < 4 {
					continue
				}
				name, ok := property[0].(string)
				if !ok || (name != "fn" && name != "org") {
					continue
				}
				if text, ok := property[3].(string); ok && text != "" {
					return text
				}
				if values, ok := property[3].([]any); ok && len(values) > 0 {
					if text, ok := values[0].(string); ok && text != "" {
						return text
					}
				}
			}
		}
	}
	return "registrar"
}
