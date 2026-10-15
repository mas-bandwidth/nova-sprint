package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serverPass is the server's tick writing every friend's status word (store.PublishFriendStatus)
// from the hook records as they stand, the one writer; a test that turns the fixture's own
// pass off (noServer) takes it when it wants one.
func (ta *testApp) serverPass() {
	ta.t.Helper()
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(ta.t, err)
	require.NoError(ta.t, st.PublishFriendStatus(context.Background(), ta.a.now()))
}

// The dashboard reads each friend's status from the word the server wrote and derives
// nothing: with no bus address (the hook records unreachable, the state that printed "hook
// server never ran" for every friend while where showed them up) it shows exactly what the
// server wrote. Glenn, 2026-10-15: "the dashboard is required to get up/down from the one
// source of friends being up/down, which is the server in redis."
func TestTheDashboardShowsTheStatusTheServerWroteWithNoBusAddress(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy", "bob", "cat", "dee")
	ta.noServer = true
	ta.ok("friend sync")
	// amy is up, bob is up and held, cat is down, dee is down and held
	hooks := map[string]hook.Status{
		"amy": {Name: "amy", Role: hook.Friend, State: hook.StateProven},
		"bob": {Name: "bob", Role: hook.Friend, State: hook.StateProven},
		"dee": {Name: "dee", Role: hook.Friend, State: hook.StateUnhooked, Reason: "missed 2 challenges"},
	}
	ta.a.readFriendHooks = func(_ context.Context, names ...string) ([]hook.Status, time.Time, error) {
		var out []hook.Status
		for _, n := range names {
			if h, ok := hooks[n]; ok {
				out = append(out, h)
			}
		}
		return out, ta.now, nil
	}
	ta.ok("friend down bob")
	ta.ok("friend down dee")
	ta.serverPass()

	// the hook records are gone from this process: no bus address, as the dashboard ran
	ta.a.readFriendHooks = nil
	_, err := ta.a.hookStore(context.Background())
	require.Error(t, err, "this process has no bus address")

	want := map[string]string{"amy": sprint.Up, "bob": sprint.Held, "cat": sprint.Down, "dee": sprint.Down}
	read := func(body []byte) map[string]string {
		var w whereView
		require.NoError(t, json.Unmarshal(body, &w))
		got := map[string]string{}
		for _, f := range w.Friends {
			got[f.Name] = f.Status
		}
		return got
	}
	body, err := ta.a.whereJSONForDashboard("", false)
	require.NoError(t, err, "the dashboard's read does not need the bus")
	assert.Equal(t, want, read(body), "the dashboard shows the word the server wrote, and no other")

	srv := ta.a.dashboardServer("", false, "", time.Second, "", nil)
	srv.Tick()
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/sprint", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var v struct {
		OK   bool `json:"ok"`
		Data struct {
			Friends []store.FriendRow `json:"friends"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v))
	require.True(t, v.OK)
	served := map[string]string{}
	for _, f := range v.Data.Friends {
		served[f.Name] = f.Status
	}
	assert.Equal(t, want, served, "and so does the page it serves")
	assert.Equal(t, want, ta.friendStatusWords(), "and where")
}

// friendStatusWords is each friend's status word as where's table says it (the cell's first word).
func (ta *testApp) friendStatusWords() map[string]string {
	ta.t.Helper()
	out := map[string]string{}
	for f, s := range ta.friendStatus() {
		for i, r := range s {
			if r == ' ' {
				s = s[:i]
				break
			}
		}
		out[f] = s
	}
	return out
}

// A server that cannot read the hook record writes every hooked friend down, with the
// reason, never up: the dashboard then shows down, and it never wrote a guess.
func TestTheServerWritesDownForAFriendWhenItCannotReadTheHookRecord(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy")
	ta.noServer = true
	ta.ok("friend sync")
	ta.a.readFriendHooks = func(context.Context, ...string) ([]hook.Status, time.Time, error) {
		return nil, time.Time{}, errors.New("hook server never ran")
	}
	ta.serverPass()
	assert.Equal(t, map[string]string{"amy": sprint.Down}, ta.friendStatusWords())
	assert.Contains(t, ta.friendStatus()["amy"], "hook server never ran")
}
