package mailcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"snippetbook/internal/model"
)

const mxtoolboxAPIBaseURL = "https://api.mxtoolbox.com/api/v1"

type mxtoolboxBlacklistResolver struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewMxToolboxBlacklistResolver(apiKey string, timeout time.Duration) MxToolboxBlacklistResolver {
	if apiKey == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &mxtoolboxBlacklistResolver{
		apiKey:  apiKey,
		baseURL: mxtoolboxAPIBaseURL,
		client:  &http.Client{Timeout: timeout},
	}
}

type mxtoolboxResponse struct {
	Failed   []mxtoolboxItem `json:"Failed"`
	Passed   []mxtoolboxItem `json:"Passed"`
	Timeouts []mxtoolboxItem `json:"Timeouts"`
	Errors   []mxtoolboxItem `json:"Errors"`
}

type mxtoolboxItem struct {
	Name                       string `json:"Name"`
	Info                       string `json:"Info"`
	URL                        string `json:"Url"`
	BlacklistTTL               string `json:"BlacklistTTL"`
	BlacklistResponseTime      string `json:"BlacklistResponseTime"`
	BlacklistReasonCode        string `json:"BlacklistReasonCode"`
	BlacklistReasonDescription string `json:"BlacklistReasonDescription"`
	DelistURL                  string `json:"DelistUrl"`
}

func (r *mxtoolboxBlacklistResolver) LookupBlacklist(ctx context.Context, address string) (model.BlacklistResult, error) {
	endpoint := r.baseURL + "/Lookup/blacklist/" + "?argument=" + url.QueryEscape(address)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return model.BlacklistResult{}, err
	}
	request.Header.Set("Authorization", r.apiKey)
	request.Header.Set("Accept", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		return model.BlacklistResult{}, fmt.Errorf("mxtoolbox request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if response.StatusCode == http.StatusUnauthorized {
			return model.BlacklistResult{}, errors.New("mxtoolbox API rejected MXTOOLBOX_API_KEY")
		}
		if response.StatusCode == http.StatusTooManyRequests {
			return model.BlacklistResult{}, errors.New("mxtoolbox API request quota exceeded")
		}
		return model.BlacklistResult{}, fmt.Errorf("mxtoolbox API returned HTTP %d", response.StatusCode)
	}
	var payload mxtoolboxResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return model.BlacklistResult{}, fmt.Errorf("decode mxtoolbox response: %w", err)
	}
	return convertMxToolboxResult(address, payload), nil
}

func convertMxToolboxResult(address string, payload mxtoolboxResponse) model.BlacklistResult {
	result := model.BlacklistResult{Status: "clean", Provider: "mxtoolbox"}
	appendItems := func(items []mxtoolboxItem, status string) {
		for _, item := range items {
			entry := model.BlacklistEntry{
				IP:             address,
				List:           item.Name,
				Source:         "mxtoolbox",
				Status:         status,
				Listed:         status == "listed",
				Reason:         blacklistReason(item, status),
				TTL:            parseUint(item.BlacklistTTL),
				ResponseTimeMS: parseInt64(item.BlacklistResponseTime),
				InfoURL:        item.URL,
				DelistURL:      item.DelistURL,
			}
			if status == "timeout" {
				entry.Error = entry.Reason
			}
			result.Results = append(result.Results, entry)
			result.Checked++
			if entry.Listed {
				result.Listed++
			}
			if status == "timeout" {
				result.Timeout++
			}
		}
	}
	appendItems(payload.Failed, "listed")
	appendItems(payload.Passed, "ok")
	appendItems(payload.Timeouts, "timeout")
	appendItems(payload.Errors, "timeout")
	if result.Listed > 0 || result.Timeout > 0 {
		result.Status = "warning"
	}
	return result
}

func blacklistReason(item mxtoolboxItem, status string) string {
	if status == "listed" {
		if item.BlacklistReasonDescription != "" {
			return item.BlacklistReasonDescription
		}
		if item.BlacklistReasonCode != "" {
			return item.BlacklistReasonCode
		}
	}
	if strings.TrimSpace(item.Info) != "" {
		return strings.TrimSpace(item.Info)
	}
	if status == "timeout" {
		return "MxToolbox did not receive a response"
	}
	return "Not listed"
}

func parseUint(value string) uint32 {
	parsed, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	return uint32(parsed)
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}
