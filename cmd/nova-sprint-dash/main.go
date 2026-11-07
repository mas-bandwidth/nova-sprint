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
	for _, arg := range os.Args[1:] {
		if arg == "--pull" || len(arg) > 7 && arg[:7] == "--pull=" {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if err := dash.Command(ctx, os.Args[1:]); err != nil {
				log.Print(err)
				os.Exit(1)
			}
			return
		}
	}
	pg := flag.String("pg", os.Getenv("NOVA_PG_DSN"), "PostgreSQL DSN")
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()
	if *pg == "" {
		log.Fatal("--pg or NOVA_PG_DSN is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	openCtx, cancelOpen := context.WithTimeout(ctx, 30*time.Second)
	password := os.Getenv("NOVA_SPRINT_PG_PASSWORD")
	if password == "" {
		password = os.Getenv("NOVA_PG_PASSWORD")
	}
	s, err := store.Open(openCtx, *pg, password)
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
