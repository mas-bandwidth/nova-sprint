package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus/bustest"
	"github.com/stretchr/testify/require"
)

func senderFixture(t *testing.T, name string, roster ...string) (*testApp, *store.Store, *bustest.Fake, *bus.Bus, map[string]string) {
	t.Helper()
	ta, _, env := coordSprint(t, name)
	f := bustest.NewFake(ta.a.now(), append([]string{name, "next-seat", "server-one", "server-two"}, roster...)...)
	b := &bus.Bus{Store: f}
	ta.a.busOpen = func(context.Context, string, string) (*bus.Bus, func(), error) { return b, func() {}, nil }
	ta.a.bus = func(ctx context.Context, m bus.Message, _ func(string)) error { _, err := b.Send(ctx, m); return err }
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now, Actor: name}
	_, err := st.Run(context.Background(), store.Step{Verb: "fixture", Plan: func(*sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Notes: []sprint.Note{
			{Kind: sprint.Judgment, Type: sprint.NMemberEnvFault, Stream: sprint.MemberSubject("m1"), StreamLevel: true, What: "member m1 environment fault", At: ta.a.now()},
			{Kind: sprint.Judgment, Type: sprint.NStatus, Primaries: []string{"m1"}, What: "member m1 is held (was up)", At: ta.a.now()},
		}}
	}})
	require.NoError(t, err)
	return ta, st, f, b, env
}

func legacyFixture(t *testing.T, ta *testApp, st *store.Store) sprint.CoordinatorDelivery {
	t.Helper()
	ctx := context.Background()
	look, err := ta.a.readCoordLook(ctx)
	require.NoError(t, err)
	candidates := map[string]sprint.CoordinatorDelivery{}
	for _, d := range coordFresh(map[string]bool{}, look, ta.a.now()) {
		candidates[d.note] = sprint.CoordinatorDelivery{To: d.to, Token: d.token, Subject: d.subject, Body: d.text, Episode: d.episode, CoveredBy: d.coveredBy, RetryBefore: ta.a.now().Add(bus.DefaultTokenLife)}
	}
	ds, err := st.CoordinatorDeliveries(ctx, candidates, false, look.epoch)
	require.NoError(t, err)
	for _, d := range ds {
		if d.CoveredBy == "" {
			return d
		}
	}
	t.Fatal("fixture lacks causal delivery")
	return sprint.CoordinatorDelivery{}
}

func TestLegacyCoordinatorSenderMigration(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"absent", "found", "known-absent", "unreadable", "expired", "found-wrong-login", "found-other-args"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			name := "sender-" + mode
			var roster []string
			if mode == "found" || mode == "known-absent" || mode == "found-wrong-login" || mode == "found-other-args" {
				roster = []string{prog}
			}
			ta, st, f, b, env := senderFixture(t, name, roster...)
			ctx := context.Background()
			old := legacyFixture(t, ta, st)
			committed := mode == "found" || mode == "found-wrong-login" || mode == "found-other-args"
			if committed {
				f.Lose = errors.New("committed but reply lost")
				body := old.Body
				if mode == "found-other-args" {
					body += "different committed message"
				}
				_, err := b.Send(ctx, bus.Message{From: prog, To: []string{old.To}, Kind: bus.KindJudgment, Subject: old.Subject, Body: body, Token: old.Token})
				require.Error(t, err)
			}
			if mode == "unreadable" {
				f.Fail = errors.New("receipt unavailable")
			}
			if mode == "expired" {
				ta.step(bus.DefaultTokenLife + time.Hour)
			}
			if mode == "found-wrong-login" {
				env[busUserEnv], env[busPasswordEnvEnv], env["TEST_PASSWORD"] = "server-two", "TEST_PASSWORD", "fixture"
			}
			ta.a.coordinatorPass(ctx)
			want := 0
			success := mode == "absent" || mode == "found"
			if success || committed {
				want = 1
			}
			require.Equal(t, want, f.Len(bus.StreamOf(name)))
			open, err := ta.m.OpenNotes(ctx)
			require.NoError(t, err)
			require.Len(t, open, 2)
			for _, o := range open {
				require.Equal(t, success, o.Note.CoordinatorDelivery.Sent)
				if success {
					expected := name
					if mode == "found" {
						expected = prog
					}
					require.Equal(t, expected, o.Note.CoordinatorDelivery.From, "the alias must freeze the same sender")
				}
			}
			require.Equal(t, !success, ta.a.coord.failed)
		})
	}
}

func TestCoordinatorSenderStaysFrozenAcrossHandoffAndLoginChange(t *testing.T) {
	t.Parallel()
	for _, named := range []bool{false, true} {
		name := "sender-handoff"
		if named {
			name = "sender-login-change"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ta, st, f, _, env := senderFixture(t, name)
			ctx := context.Background()
			if named {
				env[busUserEnv], env[busPasswordEnvEnv], env["TEST_PASSWORD"] = "server-one", "TEST_PASSWORD", "fixture"
			} else {
				legacyFixture(t, ta, st)
			}
			f.Lose = errors.New("committed but reply lost")
			ta.a.coordinatorPass(ctx)
			require.Equal(t, 1, f.Len(bus.StreamOf(name)))
			// The shared status must carry the pending send after its cause closes.
			_, err := st.Run(ctx, store.Step{Verb: "fixture close cause", Plan: func(s *sprint.Snapshot) sprint.Plan {
				var p sprint.Plan
				for _, o := range s.Open {
					if o.Note.Type == sprint.NMemberEnvFault {
						p.Closes = append(p.Closes, o)
					}
				}
				return p
			}})
			require.NoError(t, err)
			ta.a.coord = coordPush{}
			if named {
				env[busUserEnv] = "server-two"
			} else {
				require.NoError(t, ta.m.SetCoordinator(ctx, "next-seat"))
			}
			ta.a.coordinatorPass(ctx)
			require.Equal(t, 1, f.Len(bus.StreamOf(name)), "a retry must not switch sender namespaces")
			open, err := ta.m.OpenNotes(ctx)
			require.NoError(t, err)
			require.Len(t, open, 1)
			d := open[0].Note.CoordinatorDelivery
			if named {
				require.Equal(t, "server-one", d.From)
				require.False(t, d.Sent)
				require.True(t, ta.a.coord.failed)
				env[busUserEnv] = "server-one"
				ta.a.coordinatorPass(ctx)
			} else {
				require.Equal(t, name, d.From)
				require.True(t, d.Sent)
			}
			require.Equal(t, 1, f.Len(bus.StreamOf(name)))
		})
	}
}

func TestCoordinatorRecoverySenderDoesNotChangeWithLogin(t *testing.T) {
	t.Parallel()
	const name = "sender-recovery"
	ta, st, f, _, env := senderFixture(t, name)
	ctx := context.Background()
	env[busUserEnv], env[busPasswordEnvEnv], env["TEST_PASSWORD"] = "server-one", "TEST_PASSWORD", "fixture"
	f.Fail = errors.New("bus unavailable")
	ta.a.coordinatorPass(ctx)
	_, err := st.Run(ctx, store.Step{Verb: "fixture resolve", Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.Plan{Closes: s.Open} }})
	require.NoError(t, err)
	f.Fail = nil
	f.Lose = errors.New("recovery committed but reply lost")
	ta.a.coordinatorPass(ctx)
	require.Equal(t, 1, f.Len(bus.StreamOf(name)))
	require.Equal(t, "server-one", ta.a.coord.recoveryFrom)
	env[busUserEnv] = "server-two"
	ta.a.coordinatorPass(ctx)
	require.Equal(t, 1, f.Len(bus.StreamOf(name)))
	require.True(t, ta.a.coord.failed)
	env[busUserEnv] = "server-one"
	ta.a.coordinatorPass(ctx)
	require.Equal(t, 1, f.Len(bus.StreamOf(name)))
	require.False(t, ta.a.coord.failed)
}
