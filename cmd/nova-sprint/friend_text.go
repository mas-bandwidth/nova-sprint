package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/friend"
)

// friendSays selects the explicit synced row for a text about this friend. A
// roster read failure leaves the diagnostic's facts intact and names the friend
// instead of guessing pronouns. Format arguments are never rendered as templates.
func friendSays(ctx context.Context, st *store.Store, name string) func(string, ...any) string {
	spec, err := st.FriendSpecOf(ctx, name)
	if err == nil {
		p, _ := friend.ParsePronouns(spec.Pronouns)
		return p.Sprintf
	}
	return func(format string, args ...any) string {
		rendered := strings.NewReplacer("{their}", name+"'s", "{Their}", name+"'s", "{them}", name, "{Them}", name).Replace(format)
		return fmt.Sprintf(rendered, args...)
	}
}
