package main

import (
	"fmt"
	"slices"
	"strings"
)

// The doubled keyed row (docs/SPEC-SPRINT.md section 7, the shared ledgers): landruns.go's
// rule for the keyed tables whose rows carry no fingerprint. A repository that marks
// tla/COVERAGE.tsv (or tla/CASES.tsv) merge=union merges two cards that each touch one
// machine's row without a conflict, and the union keeps both rows for that key, so
// resolveRecords never sees it and dedupeMerge refused the landing ("record tla/COVERAGE.tsv
// appears twice for rn-endpoint"). Each doubled key is resolved as resolveTableByConfig
// resolves it against the merge base: byte-identical rows are one row; a key only the card
// changed keeps the card's row, one only the tip changed keeps the tip's, one both changed
// alike is one row. A key both sides changed differently, or a doubled row that is no side's
// change, is refused as before: no row is dropped that neither side meant to drop.

// keepOneDoubledKey is merged (a union-merged keyed table at path) with each key that
// appears more than once reduced to one row, chosen against that key's row at the merge base
// (base), the tip the merge was made on (tip) and the card's head (card). kept names each
// doubled key resolved. A file with no doubled key is returned as it was.
func keepOneDoubledKey(path string, merged, base, tip, card []byte) (out []byte, kept []string, err error) {
	var bRows, tRows, cRows map[string]string
	return keepOnePerKey(path, merged, nil, func(k string, rows []string) (int, error) {
		if !slices.ContainsFunc(rows[1:], func(r string) bool { return r != rows[0] }) {
			return 0, nil
		}
		if bRows == nil {
			bRows, tRows, cRows = keyedRows(path, base), keyedRows(path, tip), keyedRows(path, card)
		}
		bRow, bOk := bRows[k]
		tRow, tOk := tRows[k]
		cRow, cOk := cRows[k]
		tChanged := tOk != bOk || tRow != bRow
		cChanged := cOk != bOk || cRow != bRow
		var want string
		switch {
		case tChanged && cChanged && (tOk != cOk || tRow != cRow):
			return -1, fmt.Errorf("record %s appears twice for %s and both sides changed the record for %s", path, k, k)
		case cChanged && cOk:
			want = cRow
		case tChanged && tOk:
			want = tRow
		default:
			return -1, fmt.Errorf("record %s appears twice for %s and neither side changed the record for %s to one of them", path, k, k)
		}
		if i := slices.Index(rows, want); i >= 0 {
			return i, nil
		}
		return -1, fmt.Errorf("record %s appears twice for %s and neither row is the one its side wrote", path, k)
	})
}

// keyedRows is each key's first row in a keyed table at path (the header and blank lines skipped).
func keyedRows(path string, b []byte) map[string]string {
	m := map[string]string{}
	lines, _ := unionLines(b)
	for i, l := range lines {
		if (i == 0 && strings.HasPrefix(l, keyedHeaders[path])) || strings.TrimSpace(l) == "" {
			continue
		}
		if _, ok := m[configKey(l)]; !ok {
			m[configKey(l)] = l
		}
	}
	return m
}
