//go:build functional

package main

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- done (#94) ---

// TestStallDoneCountsEveryCompletionOnTheStore is #94 on a real store: with reads on read
// cards of the fleet, the readers' verdict counted only the readers table's reads, so a work
// card read ok on the fleet never left finished and its row read done 0. Two members work a
// card each and read each other's; the accept takes both, both work cards reach done, and each
// row's done is its work card and its read card.
func TestStallDoneCountsEveryCompletionOnTheStore(t *testing.T) {
	t.Parallel()
	r := newStallRig(t)
	r.do("init", "--readers", "reader-m1,reader-m2", "--members", "m1:2,m2:2") // each member reads on its reader row (reader-<m>)
	c := redis.NewClient(&redis.Options{Addr: r.addr})
	t.Cleanup(func() { _ = c.Close() })
	declareFunctionalRoutes(t, c, "flash")
	r.do("set", "--read-cards", "on", "--reads", "1")
	r.do("add", "--stream", "s1", "--count", "2")
	r.beat("m1", "m2")
	r.do("start")
	r.tick() // presence: the members up
	r.tick() // the deal
	worked := map[string]string{}
	work := func(who string) []string {
		var done []string
		for _, p := range r.take(who) {
			if p.Kind == "read" {
				r.cliOK(who, "read", "--as", who, "--ok", p.Card+"@"+strconv.Itoa(p.Gen), "--epoch", "0", "--usage", "input=1000 output=100")
				continue
			}
			r.cliOK(who, "finish", "--as", who, p.Card+"@"+strconv.Itoa(p.Gen), "--epoch", "0", "--head", stallHead(p.Primary), "--report", "done", "--usage", "input=1000 output=100")
			done = append(done, p.Primary)
		}
		return done
	}
	for _, m := range []string{"m1", "m2"} {
		for _, id := range work(m) {
			worked[id] = m
		}
	}
	require.Len(t, worked, 2, "each member worked a card: %v", worked)
	for i := 0; i < 6 && !(r.col("s1-1") == "merging" && r.col("s1-2") == "merging"); i++ {
		r.tick()
		r.beat("m1", "m2")
		work("m1")
		work("m2")
	}
	for _, id := range []string{"s1-1", "s1-2"} {
		v := r.card(id)
		assert.Equal(t, "merging", v.Primary.Col, "%s is read ok on a fleet read card and accepted", id)
		require.NotEmpty(t, v.Work, id)
		assert.Equal(t, "ok", v.Work[len(v.Work)-1].Col, "%s's work card leaves finished for ok: %+v", id, v.Work[len(v.Work)-1])
	}
	var w whereView
	require.NoError(t, json.Unmarshal([]byte(r.do("where", "--json")), &w))
	for _, m := range []string{"m1", "m2"} {
		row := withoutRowCards(w.Tables["fleet"][m])
		assert.Equal(t, "2", row["done"], "%s's done is its work card and the read it completed: %v", m, row)
	}
}
