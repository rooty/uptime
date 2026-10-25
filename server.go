package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
)

// serve starts the ready callback only after a successful bind. Notification
// delivery runs separately from HTTP serving and is cancelled on shutdown.
func serve(ctx context.Context, server *http.Server, ready func(context.Context, string)) error {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", server.Addr, err)
	}
	serveCtx, cancel := context.WithCancel(ctx)
	readyDone := make(chan struct{})
	go func() {
		defer close(readyDone)
		if serveCtx.Err() == nil {
			ready(serveCtx, listener.Addr().String())
		}
	}()
	defer func() {
		cancel()
		<-readyDone
	}()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-serveCtx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("server shutdown: %v", err)
		}
	}()
	err = server.Serve(listener)
	cancel()
	<-shutdownDone
	return err
}
