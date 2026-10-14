package sprintdash

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// THE PAGE LOCK: THE PAGE CHANGES ONLY WITH THE OWNER'S ASK, QUOTED AND DATED.
//
// The owner, 2026-10-11: "don't add things to the dashboard unless I ask for them", and
// "NEVER REMOVE THINGS FROM THE DASHBOARD UNLESS I ASK". The page's files (page/) and the
// code that decides what it draws (pageview.go) are held by a digest in pagelock.txt, one
// line per change, newest last:
//
//	<YYYY-MM-DD> <sha256 digest> "<the owner's exact words asking for the change>"
//
// TestThePageIsLocked computes the digest of the files as they are and holds it, with
// docs/SPEC-SPRINT-DASHBOARD.md, to CheckPageLock: any change to those files (a byte added,
// removed, hidden or filtered, a file added or taken away) changes the digest and is red,
// until a new line of pagelock.txt carries the new digest and the owner's words for it, and
// the spec's page lock section carries the same words on a line of the same date. Each
// change wants its own ask: no two lines quote the same words.
//
// A COMMENT-ONLY CHANGE IS THE ONE CHANGE WITHOUT AN ASK, AND IT IS PROVEN TO RENDER NOTHING
// NEW. A line may read
//
//	<YYYY-MM-DD> <sha256 digest> comment-only "<the reason, quoted in the spec as an ask is>"
//
// for a change that touches only comments (the first being the owner's privacy: his name
// out of the page's comments in this public repository). Its reason is held to the spec as
// an ask is; and the lock's growth check (TestThePageLockOnlyGrows, run in CI against the
// change's base) holds it to its claim: when every line the change adds is comment-only, the
// files' rendered form (RenderedForm: the bytes a browser runs, every comment taken out)
// must digest the same at the base and at the head. A comment-only line over a change that
// renders differently is red.

// PageLockSection is the heading of the spec's section the lock's quotes are read from.
const PageLockSection = "## The page lock"

// PageLockFiles is the files outside page/ the digest covers, relative to the package.
var PageLockFiles = []string{"pageview.go"}

// PageDigest is the digest of the locked files: each name with its content's sha256, in
// name order, so a file added, removed or changed changes it.
func PageDigest(files map[string][]byte) string {
	names := slices.Sorted(maps.Keys(files))
	h := sha256.New()
	for _, n := range names {
		sum := sha256.Sum256(files[n])
		fmt.Fprintf(h, "%s\x00%s\n", n, hex.EncodeToString(sum[:]))
	}
	return hex.EncodeToString(h.Sum(nil))
}

var (
	lockLine = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}) ([0-9a-f]{64}) (comment-only )?"(.+)"$`)
	specDate = regexp.MustCompile(`^- (\d{4}-\d{2}-\d{2}) `)
)

// lockEntry is one line of pagelock.txt; commentOnly marks a comment-only change.
type lockEntry struct {
	date, digest, quote string
	commentOnly         bool
}

// parseLock is pagelock.txt's lines.
func parseLock(lock string) ([]lockEntry, error) {
	var entries []lockEntry
	for i, l := range strings.Split(lock, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		m := lockLine.FindStringSubmatch(l)
		if m == nil {
			return nil, fmt.Errorf("pagelock.txt line %d is not `<YYYY-MM-DD> <sha256> \"<the owner's words>\"` (or `<YYYY-MM-DD> <sha256> comment-only \"<the reason>\"`): %s", i+1, l)
		}
		entries = append(entries, lockEntry{m[1], m[2], m[4], m[3] != ""})
	}
	return entries, nil
}

// CheckPageLock holds the locked files' digest now to the lock and the spec (above); nil when
// it holds, else why, in words that say what to do.
func CheckPageLock(lock, spec, digest string) error {
	entries, err := parseLock(lock)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("pagelock.txt holds no line: the page is locked by its first line")
	}
	asks := specAsks(spec)
	seen := map[string]bool{}
	for i, e := range entries {
		if i > 0 && e.date < entries[i-1].date {
			return fmt.Errorf("pagelock.txt line for %s is dated before the line above it (%s): lines are newest last", e.date, entries[i-1].date)
		}
		if i > 0 && e.digest == entries[i-1].digest {
			return fmt.Errorf("pagelock.txt's line of %s holds the digest of the line above it: a line is a change", e.date)
		}
		if seen[e.quote] {
			return fmt.Errorf("pagelock.txt quotes %q twice: each change of the page wants its own ask of the owner's", e.quote)
		}
		seen[e.quote] = true
		if !slices.Contains(asks[e.date], e.quote) {
			return fmt.Errorf("pagelock.txt's line of %s quotes %q, and the spec's %q section has no line of %s quoting the owner's words \"%s\": write the ask there, `- %s the owner: \"<his exact words>\"`, in the same commit", e.date, e.quote, PageLockSection, e.date, e.quote, e.date)
		}
	}
	if last := entries[len(entries)-1]; last.digest != digest {
		return fmt.Errorf("the dashboard page changed: its files (internal/sprintdash/page/ and %s) digest to %s, and pagelock.txt's last line holds %s. The page gains, loses, hides or filters nothing the owner did not ask for (the owner, 2026-10-11: \"NEVER REMOVE THINGS FROM THE DASHBOARD UNLESS I ASK\"): put the change back, or, with his ask, add to the spec's %q section a line `- <date> the owner: \"<his exact words>\"` and to pagelock.txt the line `<date> %s \"<the same words>\"`", strings.Join(PageLockFiles, ", "), digest, last.digest, PageLockSection, digest)
	}
	return nil
}

// specAsks is the quoted asks of the spec's page lock section, by date: every double-quoted
// span on a line `- <date> ...` of that section.
func specAsks(spec string) map[string][]string {
	out := map[string][]string{}
	in := false
	for _, l := range strings.Split(spec, "\n") {
		if strings.HasPrefix(l, "## ") {
			in = strings.HasPrefix(l, PageLockSection)
			continue
		}
		if !in {
			continue
		}
		m := specDate.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		for _, q := range quoted(l) {
			out[m[1]] = append(out[m[1]], q)
		}
	}
	return out
}

// quoted is every span between two straight double quotes of a line.
func quoted(l string) []string {
	parts := strings.Split(l, `"`)
	var out []string
	for i := 1; i+1 < len(parts); i += 2 {
		out = append(out, parts[i])
	}
	return out
}

// LockGrows is why the lock now does not keep every line of the lock it changes from (base),
// in order, at its head; nil when it does. Comments and blank lines are not lines of the lock.
func LockGrows(base, now string) error {
	lines := func(s string) []string {
		var out []string
		for _, l := range strings.Split(s, "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
				out = append(out, l)
			}
		}
		return out
	}
	was, is := lines(base), lines(now)
	for i, l := range was {
		if i >= len(is) || is[i] != l {
			return fmt.Errorf("pagelock.txt line %d of the base, %q, is not kept as it was: the lock only grows, and a change of the page is a new line with the owner's new ask, never an old line rewritten", i+1, l)
		}
	}
	return nil
}

// CommentOnlyHolds is why a change from base to head (their locks and their locked files) is
// not what its comment-only lines claim, nil when it is: when every line head's lock adds is
// comment-only, the files must render the same (RenderDigest) at both; a change any of whose
// new lines quotes an ask of the owner's is held by that ask instead.
func CommentOnlyHolds(baseLock, headLock string, baseFiles, headFiles map[string][]byte) error {
	was, err := parseLock(baseLock)
	if err != nil {
		return err
	}
	is, err := parseLock(headLock)
	if err != nil {
		return err
	}
	if len(is) <= len(was) {
		return nil
	}
	for _, e := range is[len(was):] {
		if !e.commentOnly {
			return nil
		}
	}
	if b, h := RenderDigest(baseFiles), RenderDigest(headFiles); b != h {
		return fmt.Errorf("pagelock.txt's new lines say comment-only, and the page renders differently: its files' rendered form (comments taken out) digests to %s at the base and %s here. A change a browser sees wants the owner's ask, quoted (the owner, 2026-10-11: \"NEVER REMOVE THINGS FROM THE DASHBOARD UNLESS I ASK\")", b, h)
	}
	return nil
}
