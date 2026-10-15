package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/bus/bustest"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ciStream drops one XREAD response without removing the durable stream row.
// The sweep must recover it without another push (CIReceiptTell.tla Sweep).
type ciStream struct {
	*bustest.Fake
	drop          bool
	reads, sweeps int
}

func (f *ciStream) BlockRead(ctx context.Context, stream, after string, block time.Duration, count int) ([]bus.Entry, error) {
	f.reads++
	if f.drop {
		f.drop = false
		return nil, nil
	}
	return f.Fake.BlockRead(ctx, stream, after, block, count)
}
func (f *ciStream) Range(ctx context.Context, stream, from, to string, count int) ([]bus.Entry, error) {
	f.sweeps++
	return f.Fake.Range(ctx, stream, from, to, count)
}

func (f *ciStream) End(ctx context.Context, stream string) (string, error) {
	rows, err := f.Fake.Range(ctx, stream, "-", "+", 1<<20)
	if err != nil || len(rows) == 0 {
		return "0-0", err
	}
	return rows[len(rows)-1].Entry, nil
}

// ciFixture keeps the real store and bus rules, replacing only transport and time.
func ciFixture(t *testing.T) (*testApp, *ciStream, *bustest.Fake) {
	t.Helper()
	ta, _ := friendCardApp(t, "friend ada", "ada")
	ta.ok("tick")
	ta.ok("take --as friend.ada s1-1.w1@1")
	ta.ok("finish --as friend.ada s1-1.w1@1 --head " + landHead + " --branch sprint/test")
	ta.a.serveAddr = "mem:0"
	previousEnv := ta.a.getenv
	ta.a.getenv = func(k string) string {
		if k == busRedisEnv {
			return "bus.test:6379"
		}
		return previousEnv(k)
	}
	transport := bustest.NewFake(ta.now, "coordinator", "ada")
	b := &bus.Bus{Store: transport}
	ta.a.bus = func(ctx context.Context, m bus.Message, _ func(string)) error { _, err := b.Send(ctx, m); return err }
	stream := &ciStream{Fake: bustest.NewFake(ta.now)}
	return ta, stream, transport
}
func ciRow() map[string]string {
	return map[string]string{"kind": "workflow_run", "status": "completed", "repo": "mas-bandwidth/nova-tools", "head": landHead, "number": "42", "run_id": "9001", "workflow": "test", "conclusion": "failure", "sender": "runner"}
}

func TestCIReceiptTellsTheOwnerAndSeat(t *testing.T) {
	t.Parallel()
	for _, lost := range []bool{false, true} {
		name := "one event"
		if lost {
			name = "lost push recovered by sweep"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ta, stream, transport := ciFixture(t)
			assert.Equal(t, "ev:github", ciReceiptStream, "pinned to nova-tools internal/ghevent/wire.Stream")
			ctx := context.Background()
			tell, err := newCITell(ta.a, ta.m, stream)
			require.NoError(t, err)
			require.NoError(t, stream.AddAll(ctx, []string{ciReceiptStream}, ciRow()))
			start := ta.a.now()
			stream.drop = lost
			require.NoError(t, tell.step(ctx))
			assert.Equal(t, 1, stream.reads, "one transport event, no cadence")
			assert.Equal(t, start, ta.a.now(), "push never advances the fake clock")
			if lost {
				assert.Zero(t, transport.Len(bus.StreamOf("coordinator")))
				ta.step(ciReceiptSweep + time.Second)
				require.NoError(t, tell.step(ctx))
				assert.Equal(t, 1, stream.sweeps)
				assert.Equal(t, 1, stream.reads, "the backstop recovers without another push")
			}
			for _, to := range []string{"coordinator", "ada"} {
				rows, err := transport.Range(ctx, bus.StreamOf(to), "-", "+", 0)
				require.NoError(t, err)
				require.Len(t, rows, 1)
				assert.Equal(t, "CI failure mas-bandwidth/nova-tools@"+landHead[:12]+" pr=42 run=9001 card=s1-1.w1", rows[0].Fields["subject"])
			}
			// A repeated receipt has another stream ID but the same repo/head/run key.
			require.NoError(t, stream.AddAll(ctx, []string{ciReceiptStream}, ciRow()))
			require.NoError(t, tell.step(ctx))
			assert.Equal(t, 1, transport.Len(bus.StreamOf("coordinator")))
			assert.Equal(t, 1, transport.Len(bus.StreamOf("ada")))
		})
	}
}

func TestCIReceiptRetriesAnAmbiguousSendAfterRestart(t *testing.T) {
	t.Parallel()
	ta, stream, transport := ciFixture(t)
	ctx := context.Background()
	tell, err := newCITell(ta.a, ta.m, stream)
	require.NoError(t, err)
	require.NoError(t, stream.AddAll(ctx, []string{ciReceiptStream}, ciRow()))
	transport.Lose = errors.New("send committed but response lost")
	require.ErrorContains(t, tell.step(ctx), "response lost")
	assert.Equal(t, "0-0", tell.cursor)
	assert.Equal(t, 1, transport.Len(bus.StreamOf("ada")))
	restarted, err := newCITell(ta.a, ta.m, stream)
	require.NoError(t, err)
	require.NoError(t, restarted.step(ctx))
	assert.NotEqual(t, "0-0", restarted.cursor)
	assert.Equal(t, 1, transport.Len(bus.StreamOf("ada")))
}

// ciFailWrite fails exactly one durable write, keeping the mem twin's semantics.
type ciFailWrite struct {
	store.KV
	calls, fail int
}

func (k *ciFailWrite) SetKey(ctx context.Context, name, value string) error {
	k.calls++
	if k.calls == k.fail {
		return errors.New("durable write lost")
	}
	return k.KV.SetKey(ctx, name, value)
}

func TestCIReceiptFailedWritesNeverSkipOrDuplicate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		fail, want int
	}{
		{"pending before send", 1, 0}, {"receipt after send", 2, 1}, {"cursor after receipt", 3, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ta, stream, transport := ciFixture(t)
			kv := &ciFailWrite{KV: ta.m, fail: tc.fail}
			tell, err := newCITell(ta.a, ta.m, stream) // the first start writes its cursor; the faults count from the first event
			require.NoError(t, err)
			tell.kv = kv
			ctx := context.Background()
			require.NoError(t, stream.AddAll(ctx, []string{ciReceiptStream}, ciRow()))
			require.ErrorContains(t, tell.step(ctx), "durable write lost")
			assert.Equal(t, "0-0", tell.cursor)
			assert.Equal(t, tc.want, transport.Len(bus.StreamOf("ada")))
			restarted, err := newCITell(ta.a, kv, stream)
			require.NoError(t, err)
			require.NoError(t, restarted.step(ctx))
			assert.NotEqual(t, "0-0", restarted.cursor)
			assert.Equal(t, 1, transport.Len(bus.StreamOf("ada")))
		})
	}
}

func TestCIReceiptRoutingUsesObservedPRsAndFreezesUnmatched(t *testing.T) {
	t.Parallel()
	for _, priorPR := range []bool{true, false} {
		name := "CI before PR"
		if priorPR {
			name = "PR replay survives restart"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ta, stream, transport := ciFixture(t)
			ctx := context.Background()
			tell, err := newCITell(ta.a, ta.m, stream)
			require.NoError(t, err)
			pr := map[string]string{"kind": "pull_request", "repo": "mas-bandwidth/nova-tools", "number": "42", "branch": "sprint/test"}
			if priorPR {
				require.NoError(t, stream.AddAll(ctx, []string{ciReceiptStream}, pr))
				require.NoError(t, tell.step(ctx))
				assert.Zero(t, transport.Len(bus.StreamOf("coordinator")), "PR rows do not notify")
				tell, err = newCITell(ta.a, ta.m, stream)
				require.NoError(t, err)
			}
			row := ciRow()
			row["head"] = strings.Repeat("b", 40)
			require.NoError(t, stream.AddAll(ctx, []string{ciReceiptStream}, row))
			require.NoError(t, tell.step(ctx))
			rows, err := transport.Range(ctx, bus.StreamOf("coordinator"), "-", "+", 0)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			if priorPR {
				assert.Equal(t, 1, transport.Len(bus.StreamOf("ada")))
				assert.Contains(t, rows[0].Fields["subject"], "card=s1-1.w1")
			} else {
				assert.Zero(t, transport.Len(bus.StreamOf("ada")))
				assert.Contains(t, rows[0].Fields["subject"], "card=-")
				require.NoError(t, stream.AddAll(ctx, []string{ciReceiptStream}, pr))
				require.NoError(t, stream.AddAll(ctx, []string{ciReceiptStream}, row))
				require.NoError(t, tell.step(ctx))
				assert.Zero(t, transport.Len(bus.StreamOf("ada")), "a later mapping cannot alter a sent token")
				assert.Equal(t, 1, transport.Len(bus.StreamOf("coordinator")))
			}
		})
	}
}

func TestCIReceiptFiltersAndRepositoryIsolation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, field, value string
		messages           int
	}{
		{"other kind", "kind", "check_run", 0},
		{"not completed", "status", "in_progress", 0},
		{"same head other repository", "repo", "elsewhere/repo", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ta, stream, transport := ciFixture(t)
			tell, err := newCITell(ta.a, ta.m, stream)
			require.NoError(t, err)
			row := ciRow()
			row[tc.field] = tc.value
			ctx := context.Background()
			require.NoError(t, stream.AddAll(ctx, []string{ciReceiptStream}, row))
			require.NoError(t, tell.step(ctx))
			assert.NotEqual(t, "0-0", tell.cursor)
			assert.Equal(t, tc.messages, transport.Len(bus.StreamOf("coordinator")))
			assert.Zero(t, transport.Len(bus.StreamOf("ada")))
		})
	}
}

func TestCIReceiptRetryKeepsTheOriginalSeatAndArguments(t *testing.T) {
	t.Parallel()
	ta, stream, transport := ciFixture(t)
	tell, err := newCITell(ta.a, ta.m, stream)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, stream.AddAll(ctx, []string{ciReceiptStream}, ciRow()))
	transport.Lose = errors.New("committed response lost")
	require.Error(t, tell.step(ctx))
	require.NoError(t, ta.m.SetCoordinator(ctx, "another-seat"))
	require.NoError(t, ta.a.onLine(ctx, func(st *store.Store) error {
		s, err := st.Load(ctx, []string{sprint.Fleet}, nil)
		if err != nil {
			return err
		}
		w := s.Fleet.Card("s1-1.w1")
		_, err = st.B.Apply(ctx, ntable.BatchManifest{Schema: 1, Table: st.Names.Table(sprint.Fleet), ExpectedTableRevision: fmt.Sprint(s.Fleet.Revision), Epoch: "0", OperationID: "ci-owner-change", Members: []ntable.BatchMemberEntry{{ID: w.ID, Expect: &ntable.MemberExpect{Revision: fmt.Sprint(w.Rev)}, Set: map[string]string{"member": "friend.another-owner"}}}})
		return err
	}))

	restarted, err := newCITell(ta.a, ta.m, stream)
	require.NoError(t, err)
	require.NoError(t, restarted.step(ctx), "an unregistered new seat would refuse any recomputed message")
	rows, err := transport.Range(ctx, bus.StreamOf("coordinator"), "-", "+", 0)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "status", rows[0].Fields["kind"])
	assert.Zero(t, transport.Len(bus.StreamOf("another-seat")))
	assert.Equal(t, 1, transport.Len(bus.StreamOf("ada")))
}

func TestCIReceiptDurableOnceKeyOutlivesBusToken(t *testing.T) {
	t.Parallel()
	for _, unresolved := range []bool{false, true} {
		name := "sent receipt survives token cleanup"
		if unresolved {
			name = "ambiguous expired send stops"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ta, stream, transport := ciFixture(t)
			tell, err := newCITell(ta.a, ta.m, stream)
			require.NoError(t, err)
			ctx := context.Background()
			require.NoError(t, stream.AddAll(ctx, []string{ciReceiptStream}, ciRow()))
			if unresolved {
				transport.Lose = errors.New("response lost")
			}
			err = tell.step(ctx)
			if unresolved {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			ta.step(8 * 24 * time.Hour)
			transport.Advance(8 * 24 * time.Hour)
			require.NoError(t, stream.AddAll(ctx, []string{ciReceiptStream}, ciRow()))
			restarted, err := newCITell(ta.a, ta.m, stream)
			require.NoError(t, err)
			err = restarted.step(ctx)
			if unresolved {
				require.ErrorContains(t, err, "expired unresolved send")
				assert.Equal(t, "0-0", restarted.cursor)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, 1, transport.Len(bus.StreamOf("ada")))
		})
	}
}

// TestACITellFirstStartBeginsAtTheStreamEnd pins the v1.3.17 fix: no stored cursor
// means the stream's current end, so past CI events are never replayed.
func TestACITellFirstStartBeginsAtTheStreamEnd(t *testing.T) {
	t.Parallel()
	ta, stream, transport := ciFixture(t)
	ctx := context.Background()
	require.NoError(t, stream.AddAll(ctx, []string{ciReceiptStream}, ciRow()))
	tell, err := newCITell(ta.a, ta.m, stream)
	require.NoError(t, err)
	end, err := stream.End(ctx, ciReceiptStream)
	require.NoError(t, err)
	assert.Equal(t, end, tell.cursor)
	assert.NotEqual(t, "0-0", tell.cursor)
	stream.drop = false
	ta.step(ciReceiptSweep + time.Second)
	require.NoError(t, tell.step(ctx))
	assert.Zero(t, transport.Len(bus.StreamOf("coordinator")), "history before the first start is never told")
}
