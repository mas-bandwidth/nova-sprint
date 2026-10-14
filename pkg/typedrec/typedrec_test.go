package typedrec

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CardPaths is PATHS then NEW, each split on commas, blanks trimmed; an absent field, an
// empty entry and `none` contribute nothing.
func TestCardPathsIsPathsThenNewWithNoneAndBlanksDropped(t *testing.T) {
	t.Parallel()
	read := func(m map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
	}
	assert.Equal(t, []string{"a.go", "b/*.go", "c.go"}, CardPaths(read(map[string]string{"PATHS": " a.go , b/*.go", "NEW": "c.go"})))
	assert.Equal(t, []string{"c.go"}, CardPaths(read(map[string]string{"PATHS": "none", "NEW": ",, c.go ,"})))
	assert.Nil(t, CardPaths(read(map[string]string{})))
	assert.Nil(t, CardPaths(read(map[string]string{"PATHS": "none", "NEW": "none"})))
	assert.Equal(t, []string{"p.go", "n.go"}, CardPaths(read(map[string]string{"NEW": "n.go", "PATHS": "p.go"})), "order is PATHS then NEW")
}

// IsSha is 7 to 64 lowercase hex digits; IsFullSha is exactly 40 or 64 of them.
func TestIsShaAndIsFullShaBounds(t *testing.T) {
	t.Parallel()
	hex := func(n int) string { return strings.Repeat("0123456789abcdef", 4)[:n] }
	for _, n := range []int{7, 8, 12, 40, 64} {
		assert.True(t, IsSha(hex(n)), n)
	}
	for _, n := range []int{0, 1, 6} {
		assert.False(t, IsSha(hex(n)), n)
	}
	assert.False(t, IsSha(hex(64)+"0"))
	assert.False(t, IsSha(strings.ToUpper(hex(12))))
	assert.False(t, IsSha("abcdefg"))
	assert.False(t, IsSha("-"))
	for _, n := range []int{40, 64} {
		assert.True(t, IsFullSha(hex(n)), n)
	}
	for _, n := range []int{7, 39, 41, 63} {
		assert.False(t, IsFullSha(hex(n)), n)
	}
}

// ParseCardResult reads the first `key: value` of each key (keys in any case, a key with a
// blank is no key), the text under `## Body`, and is Shaped only when all six keys are
// present, head is a sha or `-`, report is non-empty and the verdict is one of the four.
func TestParseCardResultShapeRules(t *testing.T) {
	t.Parallel()
	full := "head: ABC1234\nbranch: sprint/x\nverdict: OK\ngate: go test ./...\noutput: out.txt\nreport: did it\ntitle: a title\n## body\n\nhead: not-a-key\nline two\n\n"
	r := ParseCardResult([]byte(full))
	assert.Equal(t, CardResult{Shaped: true, Head: "abc1234", Branch: "sprint/x", Verdict: "ok", Gate: "go test ./...", Output: "out.txt", Report: "did it", Title: "a title", Body: "head: not-a-key\nline two"}, r)

	for _, tc := range []struct {
		name, in string
		shaped   bool
	}{
		{"head dash is none and shaped", strings.Replace(full, "head: ABC1234", "head: -", 1), true},
		{"head that is no sha", strings.Replace(full, "head: ABC1234", "head: xyz", 1), false},
		{"missing gate", strings.Replace(full, "gate: go test ./...\n", "", 1), false},
		{"empty report", strings.Replace(full, "report: did it", "report:", 1), false},
		{"unknown verdict", strings.Replace(full, "verdict: OK", "verdict: maybe", 1), false},
		{"empty", "", false},
		{"key with a blank is no key", strings.Replace(full, "verdict: OK", "ver dict: ok", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.shaped, ParseCardResult([]byte(tc.in)).Shaped)
		})
	}
	r = ParseCardResult([]byte("head: -\nbranch: b\nverdict: not-done\ngate: -\noutput: -\nreport: r\nreport: second\n"))
	assert.True(t, r.Shaped)
	assert.Equal(t, "", r.Head, "a head of - is none")
	assert.Equal(t, "r", r.Report, "the first value of a key wins")
	assert.Equal(t, "", r.Body)
	for _, v := range CardVerdicts {
		assert.True(t, ParseCardResult([]byte("head: -\nbranch: b\nverdict: "+v+"\ngate: -\noutput: -\nreport: r\n")).Shaped, v)
	}
	assert.Equal(t, []string{"head", "branch", "verdict", "gate", "output", "report"}, CardResultKeys)
	assert.Equal(t, []string{"ok", "not-done", "nothing", "broken"}, CardVerdicts)
}

// IsKind is exactly the six kinds, in their own case.
func TestIsKindIsTheSixKinds(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"fix", "recut", "port", "docs-guard", "report", "read"}, Kinds)
	for _, k := range Kinds {
		assert.True(t, IsKind(k), k)
	}
	for _, k := range []string{"", "Fix", "ledger", "read ", "docs_guard"} {
		assert.False(t, IsKind(k), "%q", k)
	}
}

// The contract table: 16 typed field keys; RequirementFor reads the kind's column and
// answers ReqUnknown for a field or a kind it does not know; RequiredSections per kind.
func TestContractTableReads(t *testing.T) {
	t.Parallel()
	keys := Contract.FieldKeys()
	assert.Len(t, keys, 16)
	assert.Equal(t, []string{"SCHEMA", "KIND", "ATTEMPT", "CHECK", "REPO", "BRANCH", "PATHS", "RED", "GREEN", "PRIOR", "PR", "HEAD", "FINDINGS", "FLOOR", "SUGGEST", "PROBES"}, keys)
	for _, tc := range []struct{ field, kind, want string }{
		{"SCHEMA", KindFix, ReqRequired},
		{"BRANCH", KindFix, ReqDone},
		{"BRANCH", KindRead, ReqUnknown},
		{"BRANCH", KindReport, "O"},
		{"RED", KindPort, ReqDone},
		{"RED", KindDocsGuard, ReqUnknown},
		{"GREEN", KindRecut, ReqPass},
		{"PRIOR", KindRecut, ReqDone},
		{"PRIOR", KindFix, ReqUnknown},
		{"PR", KindRead, ReqDone},
		{"HEAD", KindRead, ReqDone},
		{"FINDINGS", KindRead, ReqDone},
		{"FLOOR", KindRead, ReqDone},
		{"SUGGEST", KindRead, ReqDone},
		{"PROBES", KindReport, ReqDone},
		{"PROBES", KindRead, ReqUnknown},
		{"NOPE", KindFix, ReqUnknown},
		{"SCHEMA", "ledger", ReqUnknown},
		{"SCHEMA", "", ReqUnknown},
	} {
		assert.Equal(t, tc.want, Contract.RequirementFor(tc.field, tc.kind), "%s/%s", tc.field, tc.kind)
	}
	for _, k := range Kinds {
		for _, f := range []string{"SCHEMA", "KIND", "ATTEMPT", "CHECK", "REPO"} {
			assert.Equal(t, ReqRequired, Contract.RequirementFor(f, k), "%s is required for every kind", f)
		}
	}
	assert.Equal(t, []string{"Gates", "Left owed"}, RequiredSections(KindFix))
	assert.Equal(t, []string{"Gates", "Left owed"}, RequiredSections(KindRecut))
	assert.Equal(t, []string{"Gates", "Left owed"}, RequiredSections(KindPort))
	assert.Equal(t, []string{"Verification", "Gates"}, RequiredSections(KindDocsGuard))
	assert.Equal(t, []string{"Probes", "Summary"}, RequiredSections(KindReport))
	assert.Equal(t, []string{"Findings"}, RequiredSections(KindRead))
	assert.Nil(t, RequiredSections("ledger"))
	assert.Nil(t, RequiredSections(""))
}

// JudgementFields are the kind's required fields the wrapper does not own, and its required
// sections other than Gates and Left owed; ResultFormat names them on the DONE line and is
// "" for a kind outside Kinds.
func TestJudgementFieldsAndResultFormatPerKind(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind     string
		fields   []string
		sections []string
	}{
		{KindFix, nil, nil},
		{KindPort, nil, nil},
		{KindRecut, []string{"PRIOR"}, nil},
		{KindDocsGuard, nil, []string{"Verification"}},
		{KindReport, []string{"PROBES"}, []string{"Probes", "Summary"}},
		{KindRead, []string{"PR", "HEAD", "FINDINGS", "FLOOR", "SUGGEST"}, []string{"Findings"}},
	} {
		fields, sections := JudgementFields(tc.kind)
		assert.Equal(t, tc.fields, fields, tc.kind)
		assert.Equal(t, tc.sections, sections, tc.kind)
		text := ResultFormat(tc.kind)
		assert.True(t, strings.HasPrefix(text, "RESULT-FORMAT (KIND "+tc.kind+"). When the work is done, write RESULT.md:\n"), tc.kind)
		assert.Contains(t, text, "- line 1: this card's line 1, verbatim.\n")
		assert.Contains(t, text, "- line 2: `DONE`, `ABSTAIN <why>` or `BLOCKED <why>`.\n")
		assert.True(t, strings.HasSuffix(text, "Nothing else: the wrapper writes every other field from the card, the branch, the diff and its own run of the card's TEST line.\n"), tc.kind)
		if len(tc.fields)+len(tc.sections) == 0 {
			assert.NotContains(t, text, "on DONE", tc.kind)
			continue
		}
		assert.Contains(t, text, "- on DONE, then: ", tc.kind)
		for _, f := range tc.fields {
			assert.Contains(t, text, "`"+f+": <value>`", tc.kind)
		}
		for _, s := range tc.sections {
			assert.Contains(t, text, "`## "+s+"` with `- ` rows", tc.kind)
		}
	}
	assert.Equal(t, "- on DONE, then: `PR: <value>`, `HEAD: <value>`, `FINDINGS: <value>`, `FLOOR: <value>`, `SUGGEST: <value>`, `## Findings` with `- ` rows.\n", resultFormatDoneLine(t, ResultFormat(KindRead)))
	assert.Equal(t, "", ResultFormat("ledger"))
	assert.Equal(t, "", ResultFormat(""))
	for _, f := range WrapperOwned {
		fields, _ := JudgementFields(KindFix)
		assert.NotContains(t, fields, f, "a wrapper-owned field is never the model's to write")
	}
}

func resultFormatDoneLine(t *testing.T, text string) string {
	t.Helper()
	for _, l := range strings.SplitAfter(text, "\n") {
		if strings.HasPrefix(l, "- on DONE") {
			return l
		}
	}
	assert.FailNow(t, "no DONE line in "+text)
	return ""
}

// ParseTableCheck wants exactly five fields, the tag CHECK, then four canonical decimal
// uint64 strings: a leading zero, a sign, a non-string or an overflow is refused.
func TestParseTableCheckWantsFiveCanonicalFields(t *testing.T) {
	t.Parallel()
	maxU := strconv.FormatUint(math.MaxUint64, 10)
	got, err := ParseTableCheck([]any{"CHECK", "1", "2", "3", maxU})
	require.NoError(t, err)
	assert.Equal(t, TableCheck{Epoch: 1, Revision: 2, Members: 3, Cells: math.MaxUint64}, got)
	got, err = ParseTableCheck([]any{"CHECK", "0", "0", "0", "0"})
	require.NoError(t, err)
	assert.Equal(t, TableCheck{}, got)
	for _, tc := range []struct {
		name  string
		reply []any
		want  string
	}{
		{"four fields", []any{"CHECK", "1", "2", "3"}, "table check wants 5 reply fields, got 4"},
		{"six fields", []any{"CHECK", "1", "2", "3", "4", "5"}, "wants 5 reply fields, got 6"},
		{"nil", nil, "wants 5 reply fields, got 0"},
		{"wrong tag", []any{"CHEK", "1", "2", "3", "4"}, "invalid reply tag"},
		{"tag not a string", []any{1, "1", "2", "3", "4"}, "invalid reply tag"},
		{"field not a string", []any{"CHECK", "1", 2, "3", "4"}, "field 2 wants a decimal string"},
		{"field nil", []any{"CHECK", "1", "2", "3", nil}, "field 4 wants a decimal string"},
		{"leading zero", []any{"CHECK", "01", "2", "3", "4"}, `field 1 has invalid uint64 "01"`},
		{"sign", []any{"CHECK", "1", "-2", "3", "4"}, `field 2 has invalid uint64 "-2"`},
		{"plus sign", []any{"CHECK", "1", "+2", "3", "4"}, `field 2 has invalid uint64 "+2"`},
		{"empty", []any{"CHECK", "1", "2", "", "4"}, `field 3 has invalid uint64 ""`},
		{"overflow", []any{"CHECK", "1", "2", "3", maxU + "0"}, "field 4 has invalid uint64"},
		{"blank", []any{"CHECK", "1", "2", "3", " 4"}, `field 4 has invalid uint64 " 4"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseTableCheck(tc.reply)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Equal(t, TableCheck{}, got, "a refused reply reads as nothing")
		})
	}
}

// ParseTableRefusal maps every reason token the table Lua writes to its own value, each
// token to a distinct value, and anything else (a lowercase or blank token included) to
// TableRefusalUnknown.
func TestParseTableRefusalMapsEveryTokenAndNothingElse(t *testing.T) {
	t.Parallel()
	table := map[string]TableRefusal{
		"STALE": TableRefusalStale, "MEMBEREPOCH": TableRefusalMemberEpoch, "MEMBEREXISTS": TableRefusalMemberExists,
		"PLACED": TableRefusalPlaced, "DRIFT": TableRefusalDrift, "NOTABLE": TableRefusalNoTable, "EXISTS": TableRefusalExists,
		"NOROW": TableRefusalNoRow, "NOCOL": TableRefusalNoCol, "TEXT": TableRefusalText, "NOTMEMBER": TableRefusalNotMember,
		"OCCUPIEDVALUE": TableRefusalOccupiedValue, "OCCUPIED": TableRefusalOccupied, "OCCUPIEDCELLS": TableRefusalOccupiedCells,
		"OWNEDALIAS": TableRefusalOwnedAlias, "NOMEMBER": TableRefusalNoMember, "NOVIEW": TableRefusalNoView,
		"VIEWTABLE": TableRefusalViewTable, "SUMMARY": TableRefusalSummary, "SELF": TableRefusalSelf, "WHERE": TableRefusalWhere,
		"COLEXISTS": TableRefusalColExists, "DEPENDS": TableRefusalDepends, "LASTCOL": TableRefusalLastCol, "SORTED": TableRefusalSorted,
		"SORTKEY": TableRefusalSortKey, "SORTKEEP": TableRefusalSortKeep, "NOTTEXT": TableRefusalNotText, "BOUND": TableRefusalBound,
		"EPOCH": TableRefusalEpoch, "CONDITION": TableRefusalCondition, "ARGS": TableRefusalArgs, "REVISION": TableRefusalRevision,
		"MEMBERREVISION": TableRefusalMemberRevision, "FIELDGUARD": TableRefusalFieldGuard, "OPCONFLICT": TableRefusalOpConflict,
		"LIMIT": TableRefusalLimit, "RESERVEDFIELD": TableRefusalReservedField, "TWICE": TableRefusalTwice, "SCORE": TableRefusalScore,
		"OVERFLOW": TableRefusalOverflow, "MUTATION": TableRefusalMutation, "MANIFEST": TableRefusalManifest,
		"STREAMFULL": TableRefusalStreamFull, "SCHEMA": TableRefusalSchema, "OPERATION": TableRefusalOperation, "MEMBER": TableRefusalMember,
		"WRONGTYPE": TableRefusalWrongType, "STREAMTYPE": TableRefusalStreamType, "EPOCHAHEAD": TableRefusalEpochAhead,
		"PLACEGUARD": TableRefusalPlaceGuard, "FORMULA": TableRefusalFormula, "PROPGUARD": TableRefusalPropGuard, "ORPHAN": TableRefusalOrphan,
		"RESIDUE": TableRefusalResidue,
	}
	assert.Len(t, table, int(TableRefusalResidue), "every value but Unknown has its token")
	seen := map[TableRefusal]string{}
	for token, want := range table {
		assert.Equal(t, want, ParseTableRefusal(token), token)
		assert.NotEqual(t, TableRefusalUnknown, want, token)
		if prev, dup := seen[want]; dup {
			assert.Fail(t, "two tokens share a value", "%s and %s", prev, token)
		}
		seen[want] = token
		assert.Equal(t, TableRefusalUnknown, ParseTableRefusal(strings.ToLower(token)), "case matters: %s", token)
	}
	for _, s := range []string{"", "ERR", "STALE ", " STALE", "NOTMEMBERS"} {
		assert.Equal(t, TableRefusalUnknown, ParseTableRefusal(s), "%q", s)
	}
}

// NamesADefect: a finding names a file (a name with an extension, or any name at
// file:line), a line, a STEP or the RULES; an abbreviation like e.g. is no file, and
// "Request changes." alone names nothing.
func TestNamesADefectWantsAFileALineOrARule(t *testing.T) {
	t.Parallel()
	for _, yes := range []string{
		"foo.go:12 is wrong", "see a.go too", "pkg/x/y.go is wrong", "Makefile:3 wrong", "line 3 is wrong", "Line 40",
		"STEP 2 is broken", "step 1 is not met", "RULES say otherwise", "RULE 3", "rules: no sleep", "Rule: x",
		"the file CHANGELOG.md is stale", "x.py", "a_b-c.txt is missing",
	} {
		assert.True(t, NamesADefect(yes), "%q", yes)
	}
	for _, no := range []string{
		"", "Request changes.", "Approve.", "e.g. this is wrong", "i.e. fine", "looks good to me", "the rule of thumb",
		"ruler", "steps 3", "lines 3-4", "a. b", "v1.2", "1.5x faster",
	} {
		assert.False(t, NamesADefect(no), "%q", no)
	}
}
