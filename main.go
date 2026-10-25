package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"snippetbook/internal/config"
	"snippetbook/internal/crypto"
	"snippetbook/internal/geo"
	"snippetbook/internal/handler"
	"snippetbook/internal/monitor"
	"snippetbook/internal/notify"
	"snippetbook/internal/store"
	"snippetbook/internal/version"
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
	geoDone := make(chan struct{})
	go func() {
		defer close(geoDone)
		geo.New(st).Run(ctx)
	}()
	defer func() {
		stop()
		<-geoDone
	}()

	addr := ":" + cfg.Port
	server := &http.Server{Addr: addr, Handler: r}
	if err := serve(ctx, server, func(readyCtx context.Context, address string) {
		log.Printf("snippetbook listening on %s (db=%s)", address, cfg.DBPath)
		if err := notify.Startup(readyCtx, st, notify.StartupInfo{
			Version: version.Version, Commit: version.Commit, BuildTime: version.BuildTime,
			StartedAt: version.StartedAt, Address: address,
		}); err != nil && readyCtx.Err() == nil {
			log.Printf("startup notification: %v", err)
		}
	}); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
