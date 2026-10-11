package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sectionFix is one fix form on one line.
func sectionFix(id string) string { return `(fix "` + id + `" :title "` + id + `")` }

// sectionsFile is a ledger whose :items list carries section comments between its members,
// as docs/roadmap.sexp does ("; The sprint machine", "; Setup, release and operations").
func sectionsFile(body string) string {
	return "(fixes \"v1\"\n :title \"t\"\n :items\n (" + body + "))\n"
}

// The merge by form splices only the members it changes: every comment between the tip's
// members is kept byte for byte, none lost (the cold read of PR #100: the rebuilt list
// dropped the section comments between members).
func TestMergeByFormKeepsTheCommentsBetweenMembers(t *testing.T) {
	t.Parallel()
	base := sectionFix("a") + "\n\n  ; The first section\n  " + sectionFix("b") + "\n\n  ; The second section\n  " + sectionFix("c")
	ours := base + "\n\n  " + sectionFix("one")
	theirs := base + "\n\n  " + sectionFix("two")
	out, err := mergeByForm("f.sexp", []byte(sectionsFile(base)), []byte(sectionsFile(ours)), []byte(sectionsFile(theirs)))
	require.NoError(t, err)
	got := string(out)
	for _, c := range []string{"; The first section", "; The second section"} {
		assert.Equal(t, 1, strings.Count(got, c), c+" once in:\n"+got)
	}
	assert.True(t, strings.HasPrefix(got, strings.TrimSuffix(sectionsFile(ours), "))\n")), "the tip's text is kept byte for byte:\n"+got)
	assert.Contains(t, got, sectionFix("two"))
}

// A comment in the tip's last gap (a section the tip opened with its last member) stays
// once: it is never taken as the separator of every member.
func TestMergeByFormDoesNotRepeatALastGapComment(t *testing.T) {
	t.Parallel()
	base := sectionFix("a") + "\n  " + sectionFix("b")
	ours := base + "\n\n  ; A new section\n  " + sectionFix("c")
	theirs := base + "\n  " + sectionFix("d")
	out, err := mergeByForm("f.sexp", []byte(sectionsFile(base)), []byte(sectionsFile(ours)), []byte(sectionsFile(theirs)))
	require.NoError(t, err)
	got := string(out)
	assert.Equal(t, 1, strings.Count(got, "; A new section"), got)
	assert.Contains(t, got, sectionFix("d"))
}

// A member the card drops goes with its own line; the comments around it stay.
func TestMergeByFormDropKeepsTheSectionComments(t *testing.T) {
	t.Parallel()
	base := sectionFix("a") + "\n\n  ; The first section\n  " + sectionFix("b") + "\n\n  ; The second section\n  " + sectionFix("c")
	ours := base + "\n  " + sectionFix("one")
	theirs := strings.Replace(base, sectionFix("b")+"\n", "", 1)
	out, err := mergeByForm("f.sexp", []byte(sectionsFile(base)), []byte(sectionsFile(ours)), []byte(sectionsFile(theirs)))
	require.NoError(t, err)
	got := string(out)
	assert.NotContains(t, got, `"b"`)
	for _, c := range []string{"; The first section", "; The second section"} {
		assert.Equal(t, 1, strings.Count(got, c), c+" once in:\n"+got)
	}
}

// The readback refuses a merge that loses a comment of the tip outside the spans it rewrote.
func TestLostCommentIsNamed(t *testing.T) {
	t.Parallel()
	tip := []byte("(x\n ; keep me\n (a))\n")
	assert.Equal(t, "", lostComment(tip, tip, nil))
	assert.Equal(t, "; keep me", lostComment(tip, []byte("(x\n (a))\n"), nil))
	assert.Equal(t, "", lostComment(tip, []byte("(x\n (a))\n"), [][2]int{{3, 13}}), "inside a rewritten span")
}
