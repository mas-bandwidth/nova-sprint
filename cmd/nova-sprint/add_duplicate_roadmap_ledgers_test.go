package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// roadmapLedgerBrief is one side of the dogfood pair: its own PATHS, the two
// roadmap ledger paths every card carries, and the boilerplate words both cards
// share.
func roadmapLedgerBrief(id, paths, task string) string {
	return "1. " + id + "\n" +
		"REPO: mas-bandwidth/nova-sprint\n" +
		"BASE: sprint/v17\n" +
		"PATHS: " + paths + "\n" +
		"\nTHE TASK. " + task + "\n\nSTEP 1. nothing.\n"
}

// 2026-10-19 (v1.7 dogfood): the two cards share only the roadmap ledgers
// (ROADMAP.md and docs/roadmap.sexp) that the lander owns inside every card's
// PATHS, so they are not one change. Before the roadmap-ledger rule the pair
// scored 0.30 >= 0.20 and add refused a whole brief over it.
func TestTwoCardsSharingOnlyRoadmapLedgersAreNotDuplicates(t *testing.T) {
	t.Parallel()
	a := dupCardOf("js-card-facade-rows-close-on-the-gates-that-run-them", roadmapLedgerBrief(
		"js-card-facade-rows-close-on-the-gates-that-run-them",
		"cmd/nova-sprint/facade.go,cmd/nova-sprint/facade_test.go,ROADMAP.md,docs/roadmap.sexp",
		"Conformance checkout evidence negative revision sentence surfaces control across the facade rows."))
	b := dupCardOf("cs-cards-row-closes-on-a-green-bench-run", roadmapLedgerBrief(
		"cs-cards-row-closes-on-a-green-bench-run",
		"cmd/nova-sprint/bench.go,cmd/nova-sprint/bench_test.go,ROADMAP.md,docs/roadmap.sexp",
		"Conformance checkout evidence negative revision sentence surfaces control across the green bench."))

	f, ok := probableDuplicate(a, b)
	assert.False(t, ok, "the shared roadmap ledgers alone are not a probable duplicate: %s", f.String())
	held, _ := dupHeld([]dupCard{a, b}, nil, nil)
	assert.Empty(t, held, "nothing is held when the cards share only roadmap ledgers and boilerplate words")
}

// The roadmap-ledger rule drops only the ledgers: the same pair with one real
// source file in both PATHS is still one change carded twice.
func TestTwoCardsSharingASourceFileAndRoadmapLedgersAreStillDuplicates(t *testing.T) {
	t.Parallel()
	a := dupCardOf("js-card-facade-rows-close-on-the-gates-that-run-them", roadmapLedgerBrief(
		"js-card-facade-rows-close-on-the-gates-that-run-them",
		"cmd/nova-sprint/facade.go,cmd/nova-sprint/facade_test.go,cmd/nova-sprint/shared.go,ROADMAP.md,docs/roadmap.sexp",
		"Conformance checkout evidence negative revision sentence surfaces control across the facade rows."))
	b := dupCardOf("cs-cards-row-closes-on-a-green-bench-run", roadmapLedgerBrief(
		"cs-cards-row-closes-on-a-green-bench-run",
		"cmd/nova-sprint/bench.go,cmd/nova-sprint/bench_test.go,cmd/nova-sprint/shared.go,ROADMAP.md,docs/roadmap.sexp",
		"Conformance checkout evidence negative revision sentence surfaces control across the green bench."))

	f, ok := probableDuplicate(a, b)
	require.True(t, ok, "a shared real source file still makes the pair duplicates")
	assert.Equal(t, "cs-cards-row-closes-on-a-green-bench-run", f.Of)
	assert.GreaterOrEqual(t, f.Score, duplicateThreshold)
	assert.Equal(t, []string{"cmd/nova-sprint/shared.go"}, f.Files, "only the real source file is named, not the roadmap ledgers")
	held, _ := dupHeld([]dupCard{a, b}, nil, nil)
	assert.Contains(t, held, "cs-cards-row-closes-on-a-green-bench-run", "the DUPLICATE is still held")
}
