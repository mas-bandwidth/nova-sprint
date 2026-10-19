//go:build functional

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

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
// every read with a 429. Every card and every read is finished on the other two providers,
// at its first attempt, with no redeal, no ended take and no re-ask of a reader spent, and
// no card is left behind.
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
	const limit429 = "provider failure: provider: class=rate-limited status=429 msg=Too many requests"
	const read429 = "no verdict: provider: class=rate-limited status=429 msg=Too many requests"
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
				run("read", "--as", rc.Row, "--ok", rc.ID, "--epoch", "0")
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
