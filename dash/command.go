package dash

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Command runs either the public pull-only dashboard or the standalone store shim.
func Command(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("dash", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	pull := flags.String("pull", "", "tailnet run URL")
	listen := flags.String("listen", "127.0.0.1:8080", "HTTP listen address")
	pg := flags.String("pg", "", "PostgreSQL DSN")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("dashboard: invalid flags: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("dashboard: unexpected arguments")
	}
	if *pull != "" {
		if *pg != "" {
			return fmt.Errorf("dashboard: --pull and --pg cannot be combined")
		}
		cache, err := NewPullCache(*pull, nil, nil)
		if err != nil {
			return err
		}
		return serveDashboard(ctx, *listen, cache.Handler(), cache.Run)
	}
	dsn := *pg
	if dsn == "" {
		dsn = os.Getenv("NOVA_PG_DSN")
	}
	if dsn == "" {
		return fmt.Errorf("dashboard: --pg or NOVA_PG_DSN is required")
	}
	password := os.Getenv("NOVA_SPRINT_PG_PASSWORD")
	if password == "" {
		return fmt.Errorf("dashboard: NOVA_SPRINT_PG_PASSWORD is required")
	}
	dsn, err := SprintDSN(dsn)
	if err != nil {
		return err
	}
	openCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	s, err := store.Open(openCtx, dsn, password)
	cancel()
	if err != nil {
		return err
	}
	defer s.Close()
	cache, err := NewRecovering(ctx, s)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("dashboard startup: %v; retrying", err)
	}
	return serveDashboard(ctx, *listen, Handler(cache), func(ctx context.Context) error { cache.Run(ctx, s); return ctx.Err() })
}
func serveDashboard(ctx context.Context, address string, handler http.Handler, poll func(context.Context) error) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("dashboard: listen: %w", err)
	}
	return serveDashboardListener(ctx, listener, handler, poll)
}
func serveDashboardListener(ctx context.Context, listener net.Listener, handler http.Handler, poll func(context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 2)
	go func() { done <- poll(ctx) }()
	go func() { done <- server.Serve(listener) }()
	first := <-done
	cancel()
	shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := server.Shutdown(shutdown); err != nil {
		_ = server.Close()
		return fmt.Errorf("dashboard: shutdown: %w", err)
	}
	<-done
	if errors.Is(first, context.Canceled) || errors.Is(first, http.ErrServerClosed) {
		return nil
	}
	return first
}

// ServeTestStore serves the explicitly selected local fake without PostgreSQL.
func ServeTestStore(ctx context.Context, args []string, s store.Store) error {
	flags := flag.NewFlagSet("dash", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	listen := flags.String("listen", "127.0.0.1:8080", "HTTP listen address")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return fmt.Errorf("dashboard: fake mode accepts only --listen")
	}
	cache, err := NewRecovering(ctx, s)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("dashboard startup: %v; retrying", err)
	}
	return serveDashboard(ctx, *listen, Handler(cache), func(ctx context.Context) error { cache.Run(ctx, s); return ctx.Err() })
}
