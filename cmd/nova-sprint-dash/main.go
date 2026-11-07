package main

import (
	"context"
	"errors"
	"flag"
	"github.com/mas-bandwidth/nova-sprint/dash"
	"github.com/mas-bandwidth/nova-sprint/store"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	pg := flag.String("pg", os.Getenv("NOVA_PG_DSN"), "PostgreSQL DSN")
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()
	if *pg == "" {
		log.Fatal("--pg or NOVA_PG_DSN is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	openCtx, cancelOpen := context.WithTimeout(ctx, 30*time.Second)
	s, err := store.Open(openCtx, *pg, os.Getenv("NOVA_PG_PASSWORD"))
	cancelOpen()
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	cache, err := dash.NewRecovering(ctx, s)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		log.Printf("dashboard startup: %v; retrying", err)
	}
	go cache.Run(ctx, s)
	server := &http.Server{Addr: *listen, Handler: dash.Handler(cache), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("dashboard listening on %s", *listen)
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
