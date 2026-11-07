package run

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// HTTPServer serves only the supplied dashboard handler and shuts down on ctx.
type HTTPServer struct {
	Address string
	Ready   chan<- string
}

func (s HTTPServer) Serve(ctx context.Context, handler http.Handler) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.Address)
	if err != nil {
		return fmt.Errorf("run: dashboard listen: %w", err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	if s.Ready != nil {
		select {
		case s.Ready <- listener.Addr().String():
		default:
		}
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err = server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return fmt.Errorf("run: dashboard shutdown: %w", err)
		}
		err = <-done
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
