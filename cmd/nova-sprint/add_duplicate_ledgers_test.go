package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ledgerDuplicateBrief is one side of the dogfood pair: its own PATHS, the two
// TLA ledgers every model card names, and the boilerplate words both cards
// carry.
func ledgerDuplicateBrief(id, paths, task string) string {
	return "1. " + id + "\n" +
		"REPO: mas-bandwidth/nova-sprint\n" +
		"BASE: sprint/v17\n" +
		"PATHS: " + paths + "\n" +
		"\nTHE TASK. " + task + "\n\nSTEP 1. nothing.\n"
}

// 2026-10-18 (v1.7 dogfood D10 repro 2): the two cards share only the TLA
// ledgers and the words every brief carries, so they are not one change. Before
// the ledger rule the pair scored 0.23 >= 0.20 and add refused a whole brief
// directory over it.
func TestTwoCardsSharingOnlyLedgerFilesAreNotDuplicates(t *testing.T) {
	t.Parallel()
	a := dupCardOf("v17-rework-clears-the-server-default-hold", ledgerDuplicateBrief(
		"v17-rework-clears-the-server-default-hold",
		"cmd/nova-sprint/add_hold.go,cmd/nova-sprint/add_hold_test.go,tla/CASES.tsv,tla/RUNS.tsv",
		"Rework the server default hold so a cleared hold records vision, task and tlc rows in v17 holdover."))
	b := dupCardOf("v17-land-gate-benches-include-held-members", ledgerDuplicateBrief(
		"v17-land-gate-benches-include-held-members",
		"cmd/nova-sprint/landgo.go,cmd/nova-sprint/land_gate_bench_test.go,tla/CASES.tsv,tla/RUNS.tsv",
		"Land the gate benches, including held members, and records vision, task and tlc under v17 benchwork."))

	f, ok := probableDuplicate(a, b)
	assert.False(t, ok, "the shared TLA ledgers alone are not a probable duplicate: %s", f.String())
	held, _ := dupHeld([]dupCard{a, b}, nil, nil)
	assert.Empty(t, held, "nothing is held when the cards share only ledger files and boilerplate words")
}

// The ledger rule drops only ledger files: the same pair with one real source
// file in both PATHS is still one change carded twice.
func TestTwoCardsSharingARealPathAndLedgersAreStillDuplicates(t *testing.T) {
	t.Parallel()
	a := dupCardOf("v17-rework-clears-the-server-default-hold", ledgerDuplicateBrief(
		"v17-rework-clears-the-server-default-hold",
		"cmd/nova-sprint/landgo.go,cmd/nova-sprint/add_hold.go,cmd/nova-sprint/add_hold_test.go,tla/CASES.tsv,tla/RUNS.tsv",
		"Rework the server default hold so a cleared hold records vision, task and tlc rows in v17 holdover."))
	b := dupCardOf("v17-land-gate-benches-include-held-members", ledgerDuplicateBrief(
		"v17-land-gate-benches-include-held-members",
		"cmd/nova-sprint/landgo.go,cmd/nova-sprint/land_gate_bench_test.go,tla/CASES.tsv,tla/RUNS.tsv",
		"Land the gate benches, including held members, and records vision, task and tlc under v17 benchwork."))

	f, ok := probableDuplicate(a, b)
	require.True(t, ok, "a shared real source file still makes the pair duplicates")
	assert.Equal(t, "v17-land-gate-benches-include-held-members", f.Of)
	assert.GreaterOrEqual(t, f.Score, duplicateThreshold)
	assert.Equal(t, []string{"cmd/nova-sprint/landgo.go"}, f.Files, "only the real source file is named, not the ledgers")
	held, _ := dupHeld([]dupCard{a, b}, nil, nil)
	assert.Contains(t, held, "v17-land-gate-benches-include-held-members", "the DUPLICATE is still held")
}
