package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"snippetbook/internal/config"
	"snippetbook/internal/crypto"
	"snippetbook/internal/handler"
	"snippetbook/internal/monitor"
	"snippetbook/internal/store"
)

func main() {
	cfg := config.Load()

	st, err := store.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()
	if err := st.EnsureSettings(); err != nil {
		log.Fatalf("settings: %v", err)
	}

	v := crypto.NewVault(st)
	r := handler.SetupRouter(st, v)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	watcher := monitor.New(st)
	go watcher.Run(ctx)

	addr := ":" + cfg.Port
	log.Printf("snippetbook listening on %s (db=%s)", addr, cfg.DBPath)
	server := &http.Server{Addr: addr, Handler: r}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("server shutdown: %v", err)
		}
	}()
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
