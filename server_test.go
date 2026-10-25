package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeDoesNotNotifyWhenPortIsOccupied(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
	}()
	err = serve(context.Background(), &http.Server{Addr: listener.Addr().String()}, func(context.Context, string) { t.Error("startup notification emitted for failed bind") })
	if err == nil {
		t.Fatal("occupied port did not fail")
	}
}

func TestServeHandlesRequestsWhileStartupNotificationIsPending(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 1)
	notifierStopped := make(chan struct{})
	result := make(chan error, 1)
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
		if _, err := io.WriteString(rw, "ok"); err != nil {
			t.Error(err)
		}
	})}
	go func() {
		result <- serve(ctx, server, func(ctx context.Context, address string) {
			ready <- address
			<-ctx.Done() // Simulate an unavailable Telegram API.
			close(notifierStopped)
		})
	}()
	var address string
	select {
	case address = <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("startup callback did not run")
	}
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + address + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Error(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Error(err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("HTTP blocked by notification: %d %q", response.StatusCode, body)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("unexpected shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
	select {
	case <-notifierStopped:
	default:
		t.Fatal("startup notifier was not stopped before serve returned")
	}
}

func TestServeContinuesAfterStartupCallbackReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 1)
	result := make(chan error, 1)
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) { rw.WriteHeader(http.StatusOK) })}
	go func() { result <- serve(ctx, server, func(ctx context.Context, address string) { ready <- address }) }()
	var address string
	select {
	case address = <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("not ready")
	}
	response, err := (&http.Client{Timeout: time.Second}).Get("http://" + address)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Error(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatal(response.StatusCode)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Error(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown timed out")
	}
}
