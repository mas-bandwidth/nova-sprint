//go:build functional

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-sprint/pkg/secrets"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// A real bus rejects the program name: only configured friends and machines
// exist. Exercise sendBus, Courier, Redis token writes and the actual hook store.
func TestCoordinatorSenderDeliversThroughRedisAndProvesSprint(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"anonymous", "named", "recorded"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			sprintAddr, busAddr := testutil.Start(t), testutil.Start(t)
			sc := redis.NewClient(&redis.Options{Addr: sprintAddr})
			bc := redis.NewClient(&redis.Options{Addr: busAddr})
			t.Cleanup(func() { _ = sc.Close(); _ = bc.Close() })
			require.NoError(t, fn.Load(ctx, sc))
			const seat = "sender-seat"
			const login = "server-machine"
			require.NoError(t, bc.SAdd(ctx, "friends", seat).Err())
			require.NoError(t, bc.SAdd(ctx, "machines", login).Err())
			ta := newFunctionalTestApp(t, sprintAddr)
			ta.Env["NOVA_SPRINT_ACTOR"], ta.Env[busRedisEnv] = seat, busAddr
			ta.runVerb("init", "--members", "m1", "--readers", "reader-a")
			a := ta.app()
			a.serveAddr = sprintAddr
			expected := seat
			if mode != "anonymous" {
				expected = login
				require.NoError(t, bc.Do(ctx, "ACL", "SETUSER", login, "on", ">fixture-password", "~*", "&*", "+@all").Err())
				if mode == "named" {
					ta.Env[busUserEnv], ta.Env[busPasswordEnvEnv], ta.Env["FIXTURE_BUS_PASSWORD"] = login, "FIXTURE_BUS_PASSWORD", "fixture-password"
				} else {
					a.seatLoginOn()
					path, err := a.defaultLoginFile()
					require.NoError(t, err)
					require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
					raw, err := json.Marshal(storeLogin{Redis: busAddr, User: login, Store: "fixture", As: seat, Key: "fixture.key", Sops: "sops", Secret: "BUS_PASSWORD"})
					require.NoError(t, err)
					require.NoError(t, os.WriteFile(path, raw, 0600))
					a.loginSecret = func(secrets.Login) (secrets.Secret, error) { return secrets.NewSecret("fixture-password"), nil }
				}
			}
			hs := hook.Store{C: bc}
			_, _, err := hs.HookAs(ctx, seat, hook.Coordinator, "fixture")
			require.NoError(t, err)
			h := hook.Holder{Store: hs, Name: seat, Role: hook.Coordinator, Pushes: []hook.Push{hook.Bus}, By: "fixture"}
			require.NoError(t, h.Start(ctx))
			require.NoError(t, h.Beat(ctx, map[hook.Push]bool{hook.Bus: true}))
			now, err := bc.Time(ctx).Result()
			require.NoError(t, err)
			require.NoError(t, bc.HSet(ctx, hook.ServeKey, "at", now.UnixMilli(), "every", hook.BeatEvery.Milliseconds(), "by", "fixture").Err())
			require.NoError(t, bc.HSet(ctx, hook.RecordKey(seat), "answered_at", now.UnixMilli()).Err())
			status, err := a.hookStatus(ctx, seat)
			require.NoError(t, err)
			require.NotEmpty(t, sprint.CoordinatorGate(seat, status))
			require.NoError(t, a.onLine(ctx, func(st *store.Store) error {
				_, err := st.Run(ctx, store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, Type: sprint.NMemberEnvFault, Stream: sprint.MemberSubject("m1"), StreamLevel: true, What: "member m1 environment fault", At: a.now()}))
				return err
			}))
			a.coordinatorPass(ctx)
			entries, err := bc.XRange(ctx, bus.StreamOf(seat), "-", "+").Result()
			require.NoError(t, err)
			require.Len(t, entries, 1, "the configured bus must accept the real sender")
			require.Equal(t, expected, entries[0].Values["from"])
			status, err = a.hookStatus(ctx, seat)
			require.NoError(t, err)
			require.Empty(t, sprint.CoordinatorGate(seat, status), "successful delivery proves the sprint push")
			a.coord = coordPush{}
			a.coordinatorPass(ctx)
			count, err := bc.XLen(ctx, bus.StreamOf(seat)).Result()
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
		})
	}
}
