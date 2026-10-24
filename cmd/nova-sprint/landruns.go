package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The doubled run record (docs/SPEC-SPRINT.md section 7, the shared ledgers). A repository
// that marks tla/RUNS.tsv merge=union (rocketnet does) merges two cards that each re-measure
// one case without a conflict, and the union keeps both cards' records for that config, so
// resolveRecords never sees it and dedupeMerge refused the landing ("record tla/RUNS.tsv
// appears twice for MCClientConnection.cfg"). The record whose input_sha256 is the case's
// fingerprint at the merged tree (sprint.CaseFingerprints: the runner the tree's records are
// held to) is the one measured on the merged inputs: it is kept and the other dropped, the
// join `tlacheck merge --keep` makes by hand. Two such records keep the newer started_utc;
// none is refused as before, since either would land stale.

// keepCurrentRuns is merged (a union-merged tla/RUNS.tsv) with each config that appears more
// than once reduced to the record whose input_sha256 is fingerprint(config). kept names each
// doubled config resolved. A doubled config with no such record, or whose fingerprint cannot
// be taken, is an error (the landing is refused). A file with no doubled config is returned as
// it was, and fingerprint is not called.
func keepCurrentRuns(merged []byte, fingerprint func(config string) (string, error)) (out []byte, kept []string, err error) {
	shaIdx, startedIdx := -1, -1
	return keepOnePerKey(runsFile, merged, func(head []string) {
		shaIdx, startedIdx = slices.Index(head, "input_sha256"), slices.Index(head, "started_utc")
	}, func(k string, rows []string) (int, error) {
		if shaIdx < 0 {
			return -1, fmt.Errorf("record %s appears twice for %s and has no input_sha256 column to choose by", runsFile, k)
		}
		want, ferr := fingerprint(k)
		if ferr != nil {
			return -1, fmt.Errorf("record %s appears twice for %s and its fingerprint at the merge cannot be taken: %v", runsFile, k, ferr)
		}
		best := -1
		for i, row := range rows {
			if extractStartedRaw(row, shaIdx) != want {
				continue
			}
			if best < 0 || extractStartedRaw(row, startedIdx) > extractStartedRaw(rows[best], startedIdx) {
				best = i
			}
		}
		if best < 0 {
			return -1, fmt.Errorf("record %s appears twice for %s and no record of it was measured on the merged inputs (fingerprint %s); re-measure %s on the merged tree and join it with tlacheck merge --keep tla/RUNS.tsv", runsFile, k, want[:min(12, len(want))], k)
		}
		return best, nil
	})
}

// keepOnePerKey is merged (a union-merged keyed table at path) with each key that appears
// more than once reduced to the one row choose picks (an index into that key's rows, in file
// order); the kept row stays where it was and the others are dropped. header is called with
// the header's columns before any choice. kept names each doubled key resolved; an error from
// choose refuses the file. A file with no doubled key is returned as it was, and choose is not
// called.
func keepOnePerKey(path string, merged []byte, header func(head []string), choose func(key string, rows []string) (int, error)) (out []byte, kept []string, err error) {
	lines, trailing := unionLines(merged)
	if len(lines) == 0 || !strings.HasPrefix(lines[0], keyedHeaders[path]) {
		return merged, nil, nil
	}
	if header != nil {
		header(strings.Split(lines[0], "\t"))
	}
	at := map[string][]int{}
	var order []string
	for i, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		k := configKey(line)
		if at[k] == nil {
			order = append(order, k)
		}
		at[k] = append(at[k], i+1)
	}
	drop := map[int]bool{}
	for _, k := range order {
		if len(at[k]) < 2 {
			continue
		}
		rows := make([]string, len(at[k]))
		for j, i := range at[k] {
			rows[j] = lines[i]
		}
		pick, cerr := choose(k, rows)
		if cerr != nil {
			return nil, nil, cerr
		}
		for j, i := range at[k] {
			if j != pick {
				drop[i] = true
			}
		}
		kept = append(kept, k)
	}
	if len(kept) == 0 {
		return merged, nil, nil
	}
	var keep []string
	for i, line := range lines {
		if !drop[i] {
			keep = append(keep, line)
		}
	}
	s := strings.Join(keep, "\n")
	if trailing {
		s += "\n"
	}
	return []byte(s), kept, nil
}

// resolveDoubledRuns is keepCurrentRuns on the merged tree at dir, its fingerprints read only
// when a config appears twice.
func resolveDoubledRuns(dir string, merged []byte) ([]byte, []string, error) {
	var fp func(string) (string, error)
	return keepCurrentRuns(merged, func(config string) (string, error) {
		if fp == nil {
			f, err := sprint.CaseFingerprints(dir)
			if err != nil {
				return "", err
			}
			fp = f
		}
		return fp(config)
	})
}
