//go:build functional

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
)

// TestProviderFallbackFunctional: one provider failing never stalls the fleet, it falls
// back to the next route in line (docs/SPEC-SPRINT.md section 5, provider fallback;
// tla/ProviderFallback.tla). A private store (a redis-server of its own, the function library
// loaded, explicit --redis, a scrubbed environment) with three flash routes on three
// providers, two fake members and three readers; the first provider answers every take and
// every read with a 429 that asks for a second (its words, `try again in 1s`, are the
// cooling's window, so its routes come back into line and are faulted again). Every card
// and every read is finished on the other two providers, at its first attempt, with no
// redeal, no ended take and no re-ask of a reader spent, and no card is left behind. The
// readers table's reads (set --read-cards off): a reader's return there falls back in
// steps_review.go's Read, which needs the read step to load the fleet table and the routes
// on a return (internal/sprint/store/steps.go ReadStep, which now loads Routes).
func TestProviderFallbackFunctional(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	addr := testutil.Start(t)
	admin := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, fn.Load(ctx, admin))
	for _, x := range [][2]string{{"flash-a", "pa"}, {"flash-b", "pb"}, {"flash-c", "pc"}} {
		require.NoError(t, admin.SAdd(ctx, config.RoutesKey, x[0]).Err())
		require.NoError(t, admin.HSet(ctx, config.RouteKey(x[0]), "name", x[0], "tier", "flash", "provider", x[1], "model", "m",
			"tokens", "100000", "deadline", "900", "enabled", "true").Err())
	}
	ta := newFunctionalTestApp(t, addr)
	run := func(args ...string) string { t.Helper(); return ta.runVerb(args...) }
	const n = 4
	const limit429 = "provider failure: provider: class=rate-limited status=429 msg=Too many requests, try again in 1s"
	const read429 = "no verdict: provider: class=rate-limited status=429 msg=Too many requests, try again in 1s"
	members, readers := []string{"m1", "m2"}, []string{"reader-a", "reader-b", "reader-c"}

	run("init", "--readers", "reader-a,reader-b,reader-c", "--members", "m1:4,m2:4")
	run("set", "--read-cards", "off") // the readers table's reads (steps_review.go Read)
	run("add", "--stream", "s1", "--count", strconv.Itoa(n))
	beat := func() {
		for _, m := range members {
			run("fleet", "beat", m)
		}
		for _, rd := range readers {
			run("queue", "--as", rd) // a reader asking for its queue is up
		}
	}
	beat()
	run("start")
	beat()
	run("tick") // the members come up
	cardOf := func(id string) cardView {
		t.Helper()
		var v cardView
		require.NoError(t, json.Unmarshal([]byte(run("card", id, "--json")), &v), id)
		return v
	}
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("s1-%d", i+1)
	}
	faults, readFaults := 0, 0
	done := func() bool {
		for _, id := range ids {
			if c := cardOf(id).Column; c != sprint.Merging && c != sprint.Landed {
				return false
			}
		}
		return true
	}
	for round := 0; round < 40 && !done(); round++ {
		// the provider asks for a second: its cooling ends by itself, so the reads are drawn
		// onto it again and its 429 answers them too
		time.Sleep(1100 * time.Millisecond)
		beat()
		run("tick")
		for _, m := range members {
			var q struct {
				Epoch uint64 `json:"epoch"`
				Cards []struct {
					ID      string `json:"id"`
					Primary string `json:"primary"`
					Gen     int    `json:"gen"`
				} `json:"cards"`
			}
			require.NoError(t, json.Unmarshal([]byte(run("queue", "--as", m, "--json")), &q))
			if len(q.Cards) == 0 {
				continue
			}
			epoch := strconv.FormatUint(q.Epoch, 10)
			run("take", "--as", m, "--limit", "100", "--epoch", epoch)
			for _, c := range q.Cards {
				route := ""
				for _, wc := range cardOf(c.Primary).Work {
					if wc.ID == c.ID {
						route = wc.F(sprint.FieldRoute)
					}
				}
				at := c.ID + "@" + strconv.Itoa(c.Gen)
				if route == "flash-a" {
					faults++
					run("finish", "--as", m, at, "--epoch", epoch, "--failed", "--report", limit429)
					continue
				}
				run("finish", "--as", m, at, "--epoch", epoch, "--head", c.ID)
			}
		}
		beat()
		run("tick") // the finishes drain; a fault falls back in the same step
		for _, id := range ids {
			for _, rc := range cardOf(id).Reads {
				if rc.Col != sprint.Asked && rc.Col != sprint.Reading || rc.F(sprint.FieldReturned) != "" {
					continue
				}
				if rc.F(sprint.FieldRoute) == "flash-a" {
					readFaults++
					run("read", "--as", rc.Row, "--return", rc.ID, "--reason", read429, "--epoch", "0")
					continue
				}
				run("read", "--as", rc.Row, "--ok", rc.ID, "--epoch", "0", "--usage", "input=1000 cache_read=0 cache_write=0 output=100 model="+rc.F(sprint.FieldModel))
			}
		}
	}
	require.True(t, done(), "every card finished on the other providers and accepted")
	assert.NotZero(t, faults, "the first provider faulted takes")
	t.Logf("take faults on flash-a: %d, read faults on flash-a: %d", faults, readFaults)
	for _, id := range ids {
		v := cardOf(id)
		assert.Equal(t, "1", v.Primary.Fields["attempt"], "%s: no attempt spent", id)
		for _, wc := range v.Work {
			assert.Empty(t, wc.Fields[sprint.FieldTakeEnded], "%s: no take ended", wc.ID)
			assert.Empty(t, wc.Fields["redeals"], "%s: no redeal spent", wc.ID)
			assert.NotEqual(t, "flash-a", wc.F(sprint.FieldRoute), "%s: finished on another provider", wc.ID)
		}
		for _, rc := range v.Reads {
			assert.Empty(t, rc.Fields[sprint.FieldReasked], "%s: no reader spent", rc.ID)
			if rc.F("verdict") != "" {
				assert.NotEqual(t, "flash-a", rc.F(sprint.FieldRoute), "%s: read on another provider", rc.ID)
			}
		}
	}
}

// TestProviderFallbackReadCardsFunctional is the read-cards variant: the same three
// providers and two fake members, but the reads are cards on the members' rows
// (set --read-cards on). The first provider answers every take and every read with a 429;
// a member's read card returned with that cause is asked again at once on the next route
// in line, the same read card and the same reader, and finishes there with no re-ask
// counted; every work card finishes on the other providers, at its first attempt.
// docs/SPEC-SPRINT.md section 5, provider fallback; tla/ProviderFallback.tla Fallback,
// FaultSpendsNothing.
func TestProviderFallbackReadCardsFunctional(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	addr := testutil.Start(t)
	admin := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, fn.Load(ctx, admin))
	for _, x := range [][2]string{{"flash-a", "pa"}, {"flash-b", "pb"}, {"flash-c", "pc"}} {
		require.NoError(t, admin.SAdd(ctx, config.RoutesKey, x[0]).Err())
		require.NoError(t, admin.HSet(ctx, config.RouteKey(x[0]), "name", x[0], "tier", "flash", "provider", x[1], "model", "m",
			"tokens", "100000", "deadline", "900", "enabled", "true").Err())
	}
	ta := newFunctionalTestApp(t, addr)
	run := func(args ...string) string { t.Helper(); return ta.runVerb(args...) }
	const n = 3
	const limit429 = "provider failure: provider: class=rate-limited status=429 msg=Too many requests, try again in 1s"
	const read429 = "no verdict: provider: class=rate-limited status=429 msg=Too many requests, try again in 1s"
	members, readers := []string{"m1", "m2"}, []string{"reader-m1", "reader-m2"}
	run("init", "--readers", "reader-m1,reader-m2", "--members", "m1:4,m2:4")
	run("set", "--read-cards", "on")
	for _, rd := range readers {
		run("reader", "set", rd, "--tiers", "all")
	}
	run("add", "--stream", "s1", "--count", strconv.Itoa(n))
	beat := func() {
		for _, m := range members {
			run("fleet", "beat", m)
		}
		for _, rd := range readers {
			run("queue", "--as", rd)
		}
	}
	beat()
	run("start")
	beat()
	run("tick")
	cardOf := func(id string) cardView {
		t.Helper()
		var v cardView
		require.NoError(t, json.Unmarshal([]byte(run("card", id, "--json")), &v), id)
		return v
	}
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("s1-%d", i+1)
	}
	faults, readFaults := 0, 0
	done := func() bool {
		for _, id := range ids {
			if c := cardOf(id).Column; c != sprint.Merging && c != sprint.Landed {
				return false
			}
		}
		return true
	}
	for round := 0; round < 60 && !done(); round++ {
		time.Sleep(1100 * time.Millisecond)
		beat()
		run("tick")
		for _, m := range members {
			var q struct {
				Epoch uint64 `json:"epoch"`
				Cards []struct {
					ID      string `json:"id"`
					Primary string `json:"primary"`
					Gen     int    `json:"gen"`
				} `json:"cards"`
			}
			require.NoError(t, json.Unmarshal([]byte(run("queue", "--as", m, "--json")), &q))
			if len(q.Cards) == 0 {
				continue
			}
			epoch := strconv.FormatUint(q.Epoch, 10)
			run("take", "--as", m, "--limit", "100", "--epoch", epoch)
			for _, c := range q.Cards {
				v := cardOf(c.Primary)
				route, read := "", false
				for _, wc := range v.Work {
					if wc.ID == c.ID {
						route = wc.F(sprint.FieldRoute)
					}
				}
				for _, rc := range v.Reads {
					if rc.ID == c.ID {
						route, read = rc.F(sprint.FieldRoute), true
					}
				}
				at := c.ID + "@" + strconv.Itoa(c.Gen)
				if route == "flash-a" {
					if read {
						readFaults++
						run("read", "--as", m, "--return", c.ID, "--reason", read429, "--epoch", "0")
						continue
					}
					faults++
					run("finish", "--as", m, at, "--epoch", epoch, "--failed", "--report", limit429)
					continue
				}
				if read {
					model := ""
					v = cardOf(c.Primary)
					for _, rc := range v.Reads {
						if rc.ID == c.ID {
							route, model = rc.F(sprint.FieldRoute), rc.F(sprint.FieldModel)
						}
					}
					run("read", "--as", m, "--ok", c.ID, "--epoch", "0", "--usage", "input=1000 cache_read=0 cache_write=0 output=100 model="+model)
					continue
				}
				run("finish", "--as", m, at, "--epoch", epoch, "--head", c.ID)
			}
		}
		beat()
		run("tick") // the finishes drain; a fault falls back in the same step
	}
	require.True(t, done(), "every card finished on the other providers and accepted")
	assert.NotZero(t, faults, "the first provider faulted takes")
	assert.NotZero(t, readFaults, "the first provider faulted read cards")
	for _, id := range ids {
		v := cardOf(id)
		assert.Equal(t, "1", v.Primary.Fields["attempt"], "%s: no attempt spent", id)
		for _, wc := range v.Work {
			assert.Empty(t, wc.Fields[sprint.FieldTakeEnded], "%s: no take ended", wc.ID)
			assert.Empty(t, wc.Fields["redeals"], "%s: no redeal spent", wc.ID)
			assert.NotEqual(t, "flash-a", wc.F(sprint.FieldRoute), "%s: finished on another provider", wc.ID)
		}
		for _, rc := range v.Reads {
			assert.Empty(t, rc.Fields[sprint.FieldReasked], "%s: no reader spent", rc.ID)
			if rc.F("verdict") != "" {
				assert.NotEqual(t, "flash-a", rc.F(sprint.FieldRoute), "%s: read on another provider", rc.ID)
			}
		}
	}
}
