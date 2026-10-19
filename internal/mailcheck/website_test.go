package mailcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckWebsiteEndpointCollectsResponseDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("same page"))
	}))
	defer server.Close()

	result := checkWebsiteEndpoint(context.Background(), server.URL, time.Second)
	if result.Status != "ok" || result.HTTPStatus != http.StatusOK || result.ContentLength != 9 || result.ContentFingerprint == "" {
		t.Fatalf("unexpected website result: %+v", result)
	}
}

func TestCheckWebsiteEndpointReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	result := checkWebsiteEndpoint(context.Background(), server.URL, time.Second)
	if result.Status != "warning" || result.HTTPStatus != http.StatusNotFound || result.Error == "" {
		t.Fatalf("unexpected website failure result: %+v", result)
	}
}
