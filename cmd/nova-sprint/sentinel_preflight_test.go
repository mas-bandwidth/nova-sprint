package main

import (
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestSentinelProducerPreflightPreservesDropAndNestedExceptions(t *testing.T) {
	t.Parallel()
	live := sprint.SentinelCard{ID: "p", State: string(sprint.Landed), Head: strings.Repeat("a", 40), Brief: "fixture\nREPO: /fixture/repo\n", Placed: true}
	nested := sprint.SentinelCard{ID: "upstream", Kind: "sentinel", State: string(sprint.Landed), Placed: true}
	drop := sprint.SentinelCard{ID: "d", Outcome: "dropped"}
	require.True(t, sentinelProducersComplete(sprint.SentinelInput{}))
	require.True(t, sentinelProducersComplete(sprint.SentinelInput{Producers: []sprint.SentinelCard{drop}}))
	require.True(t, sentinelProducersComplete(sprint.SentinelInput{Producers: []sprint.SentinelCard{live, nested, drop}}))
	for _, mutate := range []func(*sprint.SentinelCard){
		func(c *sprint.SentinelCard) { c.State = string(sprint.Working) },
		func(c *sprint.SentinelCard) { c.State = "" },
		func(c *sprint.SentinelCard) { c.Head = "short" },
		func(c *sprint.SentinelCard) { c.Brief = "fixture" },
	} {
		invalid := live
		mutate(&invalid)
		require.False(t, sentinelProducersComplete(sprint.SentinelInput{Producers: []sprint.SentinelCard{live, invalid, drop}}))
	}
	drop.Placed = true
	drop.State = string(sprint.Ready)
	require.False(t, sentinelProducersComplete(sprint.SentinelInput{Producers: []sprint.SentinelCard{drop}}), "a placed historical drop is current work")
	nested.State = string(sprint.Waiting)
	require.False(t, sentinelProducersComplete(sprint.SentinelInput{Producers: []sprint.SentinelCard{live, nested}}), "named sentinel must actually be released")
}
