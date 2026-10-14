package sprint_test

import (
	"context"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/hook"
	"time"
)

func setFriendHook(st *store.Store, name, state string) error {
	prev := st.ReadFriendHooks
	st.ReadFriendHooks = func(ctx context.Context, names ...string) ([]hook.Status, time.Time, error) {
		by := map[string]hook.Status{}
		if prev != nil {
			rows, _, err := prev(ctx, names...)
			if err != nil {
				return nil, time.Time{}, err
			}
			for _, r := range rows {
				by[r.Name] = r
			}
		}
		word := hook.StateUnhooked
		if state == sprint.Up {
			word = hook.StateProven
		}
		by[name] = hook.Status{Name: name, State: word}
		var out []hook.Status
		for _, n := range names {
			if r, ok := by[n]; ok {
				out = append(out, r)
			}
		}
		return out, time.Time{}, nil
	}
	return nil
}
