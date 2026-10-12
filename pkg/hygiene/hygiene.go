// Package hygiene is the one place that answers "is this diff clean", for the accept
// gate, for the merge lane and for a hand.
//
// SPEC-TOOLWORK.md, Hygiene. Four mechanical questions, none of
// which reads prose and none of which needs a model:
//
//	identity     every commit in the range is the pool's own, and none is a merge
//	out-of-path  every changed path matches one of the card's declared globs
//	stray-file   nothing was added that does not belong in a repository
//	secret       no added line has the SHAPE of a key
//
// One implementation and three callers -- `nova-pulse accept` at harvest, `nova-merge
// batch` on every member, and `nova-check hygiene` for a person before they ask for a
// read -- so the lane and the harvest cannot disagree about what clean means. A second
// implementation is a second definition, and the day they drift is the day a card
// passes one and fails the other with nobody able to say which is right.
//
// What it does not do: it never opens RESULT.md or any other prose the worker wrote,
// it never prints a matched secret, and it never decides anything. It returns findings;
// the caller decides what a finding costs.
package hygiene

import (
	"regexp"
)

// Identity is one line at a keyboard: the name and email a commit in this range must
// carry, as the pool's identity.tsv (or the lane's identities.tsv) spells it.
type Identity struct {
	Name  string
	Email string
}

// Finding is one thing wrong, in the form the gate's REJECT line prints: a token, and
// where. Why is one line for a person and never carries matched secret text.
type Finding struct {
	Token string // identity | out-of-path | stray-file | secret
	At    string // <sha12>, <path>, or <path>:<line>
	Why   string
}

type Options struct {
	Repo string
	Base string
	Head string

	// Paths is the card's PATHS: globs. ABSENT (nil or empty) means the diff is not
	// bounded -- a friend's batch member has no card and no declared paths -- and
	// out-of-path is skipped and said to be skipped (`paths=-`), never silently
	// passed.
	Paths []string

	// Identities is a set: at harvest the pool's one row, at batch every friend who
	// commits to this repository. An empty set is a refusal, not a pass: a range
	// checked against nobody would admit anybody.
	Identities []Identity

	// Kind is the card's KIND:, used only to honour an allowlisted stray exception
	// that names the kind it is for. Empty means no exception applies.
	Kind string
}

// Check runs the four checks over base..head and returns everything wrong, in a stable
// order. An error is a could-not-run (a bad ref, a repo that is not a working copy) and
// is never a finding: a check that could not run has found nothing, and reporting it as
// clean is the one answer that must not be possible.
type entry struct {
	oldMode string
	newMode string
	newBlob string
	status  string // A, D, M, T
	path    string
}

// rawDiff reads the range with renames OFF, which is what makes a rename count on BOTH
// sides: with `--find-renames` git reports one `R` row naming the destination and the
// source is never listed, so a card that moved a file OUT of its declared paths would
// be judged only on where it landed. Off, the same move is a `D` and an `A`, and both
// are checked.
//
// `--no-abbrev` because the blob id on a `--raw` row is what `git cat-file -s` is then
// asked to size, and an abbreviation is ambiguous sooner or later -- at which point the
// whole Check fails over a range that was fine.
//
// `--ignore-submodules=none` because the checked repository's own `.git/config` can set
// `diff.ignoreSubmodules=all`, which makes this read omit an added mode-160000 gitlink
// entirely: modeFinding's explicit submodule refusal and checkPaths never see the entry
// and the range reports clean over a gitlink it added. configOpts blanks the global and
// system config, not the local one, so the visibility is forced here as it is in
// diffOpts. The subject does not choose which changes the check is shown.
const oneMiB = 1024 * 1024

// checkStray: nothing was added that does not belong in a repository, nothing carries a
// mode or a type git should not be asked to keep, and no changed file holds a conflict
// marker.
var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// conflictMarker is git's own spelling of one: exactly seven of the character, then a
// whitespace or the end of the line. Eight is not a marker and neither is `"<<<<<<<"` inside
// a string, which is why this is anchored rather than a substring search.
var conflictMarker = regexp.MustCompile(`^(<{7}|={7}|>{7})( |$)`)

// checkAddedLines is the one pass over what this range ADDED, and it answers two of the
// four questions: does any added line have the SHAPE of a key, and did anybody leave a
// conflict marker behind.
//
// A range is judged by what it added: a shape or a marker already in the base is
// somebody else's finding, and charging it here would make every card in a repository
// that once carried one unfixable by anybody.
//
// The matched text is not printed, ever, and that is not politeness. A finding travels:
// into a gate's stdout, into a PR body, into a harvest log, into whatever a coordinator
// pastes into a chat. A finding that quotes the key has copied the key into every one
// of those places, and the check meant to contain a leak has published it.
//
// The markers are read off the added lines, not asked of `git diff --check`, which the SUBJECT
// could switch off. `--check` honours a `-diff` attribute whatever `--text` says, and
// the attribute reaches git from three places: a committed `.gitattributes`,
// `.git/info/attributes`, and a local `core.attributesFile`. `--attr-source` covers the
// first and is git 2.42 and later; it covers neither of the others on any version, and
// the gate's worktree shares the job clone's `.git`, so both are the
// worker's to write. Reading the lines this function already holds has no version to
// depend on, no skip, and nothing for the subject to turn off: `--text` is what makes
// the lines appear, and `--text` is not optional here.
//
// card-16 left `<<<<<<< HEAD` inside a fenced block and it reached a pull request,
// because nothing between the worker and the forge ever looked at the bytes.
var configOpts = []string{"--no-replace-objects", "-c", "core.quotePath=false"}

// diffOpts is the shape of every textual diff this package reads, and every one of
// them is here because the repository being CHECKED controls what git prints.
//
// gitCmd already blanks the global and system config -- a bench's configuration is not
// evidence -- but a job clone's own `.git/config`, its own `.git/info/attributes` and
// its own committed `.gitattributes` all belong to the worker, and every one of them
// changes the output:
//
//	--src-prefix/--dst-prefix  `diff.noprefix` in the subject repo drops the `a/` and
//	                           `b/` and the parser never learns a file name;
//	                           `diff.mnemonicPrefix` and `diff.dstPrefix` rename them.
//	--text                     a `-diff` attribute -- from any of the three places one
//	                           can be written, and a NUL byte needs no attribute at
//	                           all -- makes git print "Binary files differ" instead of
//	                           the lines, and nothing then reaches the shapes or the
//	                           marker scan. This is the option the whole of
//	                           checkAddedLines rests on.
//	--no-textconv              a `diff` driver in the same file would run a program of
//	                           the worker's choosing and diff ITS output.
//	--ignore-submodules=none   `diff.ignoreSubmodules=all` in that same file makes git
//	                           omit an added gitlink from every diff, so the raw read
//	                           loses the modeFinding refusal and checkPaths never sees
//	                           the path, and this read loses the gitlink's own lines.
//
// A check whose subject can choose what it is shown is not a check. There is
// deliberately no `--attr-source` here: it reads only the committed `.gitattributes`,
// it is git 2.42 and later, and the one check that needed it -- `git diff --check` --
// is gone, because `--check` honours the attribute whatever `--text` says and the
// markers are now read off these same lines.
var diffOpts = []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--text", "--src-prefix=a/", "--dst-prefix=b/", "--ignore-submodules=none"}

// diffPath reads the file name off a `+++ ` header: the unquoting first, because git
// wraps the PREFIX inside the quotes, then the `b/` off the result. A `/dev/null`
// destination and anything else unrecognised give "", which skips the hunk rather than
// charging its lines to the wrong file.
