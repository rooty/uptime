package mailcheck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"snippetbook/internal/model"
)

const websiteBodyLimit = 2 << 20

func checkWebsite(ctx context.Context, domain string, timeout time.Duration) model.WebsiteResult {
	apex := strings.TrimPrefix(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), "."), "www.")
	if apex == "" {
		return model.WebsiteResult{Status: "not_found", Message: "website domain is empty"}
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	requestContext := context.WithoutCancel(ctx)
	urls := []string{"https://" + apex, "https://www." + apex}
	results := make([]model.WebsiteEndpoint, len(urls))
	var waitGroup sync.WaitGroup
	for index, target := range urls {
		waitGroup.Add(1)
		go func(index int, target string) {
			defer waitGroup.Done()
			results[index] = checkWebsiteEndpoint(requestContext, target, timeout)
		}(index, target)
	}
	waitGroup.Wait()

	result := model.WebsiteResult{Status: "ok", Results: results}
	available := 0
	for _, endpoint := range results {
		if endpoint.Status == "ok" {
			available++
		} else {
			result.Status = "warning"
		}
	}
	if available == 0 {
		result.Status = "error"
		result.Message = "оба варианта сайта недоступны"
		return result
	}
	if available == 2 {
		if results[0].HTTPStatus != results[1].HTTPStatus {
			result.Different = true
			result.Differences = append(result.Differences, "HTTP status отличается")
		}
		if results[0].FinalURL != results[1].FinalURL {
			result.Different = true
			result.Differences = append(result.Differences, "конечный URL отличается")
		}
		if results[0].ContentFingerprint != results[1].ContentFingerprint {
			result.Different = true
			result.Differences = append(result.Differences, "содержимое страниц отличается")
		}
		if result.Different {
			result.Status = "warning"
			result.Message = "домен и www-версия сайта отвечают по-разному"
		}
	}
	return result
}

func checkWebsiteEndpoint(parent context.Context, target string, timeout time.Duration) model.WebsiteEndpoint {
	result := model.WebsiteEndpoint{URL: target}
	requestContext, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	client := &http.Client{Timeout: timeout}
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, target, nil)
	if err != nil {
		result.Status, result.Error = "error", err.Error()
		return result
	}
	request.Header.Set("User-Agent", "SnippetBook-DomainCheck/1.0")
	response, err := client.Do(request)
	if err != nil {
		result.Status, result.Error = "error", fmt.Sprintf("HTTP request: %v", err)
		return result
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, websiteBodyLimit))
	if err != nil {
		result.Status, result.Error = "error", fmt.Sprintf("read response: %v", err)
		return result
	}
	result.FinalURL = response.Request.URL.String()
	result.HTTPStatus = response.StatusCode
	result.ContentLength = int64(len(body))
	hash := sha256.Sum256(body)
	result.ContentFingerprint = hex.EncodeToString(hash[:])
	if response.StatusCode >= 200 && response.StatusCode < 400 {
		result.Status = "ok"
	} else {
		result.Status = "warning"
		result.Error = fmt.Sprintf("HTTP status %d", response.StatusCode)
	}
	return result
}
