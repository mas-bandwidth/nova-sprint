package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	busauth "github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/redisconn"
)

// The server has no separate machine identity. A named bus login is its sender;
// an unauthenticated bus uses the coordinator on whose behalf the server sends.
// Resolve the same options as openBus, including a recorded seat login.
func (a *app) coordinatorBusLogin() (string, error) {
	o, getenv, err := a.busConnectionOptions(a.getenv(busRedisEnv), a.getenv(busUserEnv))
	if err != nil {
		return "", err
	}
	resolved, err := redisconn.Resolve(o, getenv)
	if err != nil {
		return "", err
	}
	return resolved.User, nil
}

func coordinatorSenderRefusal(from, login string) string {
	_, why := busauth.Identity(from, login)
	return why
}

// Older pending notes lack From. Never move a possibly committed token to a new
// sender namespace without checking the old namespace first. A found token keeps
// its old sender: the ordinary send then validates identity, roster and exact
// arguments before recording success. An unreadable receipt fails closed.
func (a *app) coordinatorLegacySender(ctx context.Context, d sprint.CoordinatorDelivery, sender string) (string, error) {
	b, closeBus, err := a.busOpen(ctx, a.getenv(busRedisEnv), a.getenv(busUserEnv))
	if err != nil {
		return "", err
	}
	defer closeBus()
	ctx, cancel := context.WithTimeout(ctx, hookReadBound)
	defer cancel()
	_, found, err := b.Store.Sent(ctx, bus.SentOf(prog, d.Token))
	switch {
	case err != nil:
		return "", fmt.Errorf("old sender token could not be read: %w", err)
	case !found:
		friends, machines, _, err := b.Store.Members(ctx)
		if err != nil {
			return "", err
		}
		if slices.Contains(friends, prog) || slices.Contains(machines, prog) {
			return "", errors.New("old sender is registered but its receipt is absent; migration is ambiguous")
		}
		return sender, nil
	default:
		return prog, nil
	}
}
