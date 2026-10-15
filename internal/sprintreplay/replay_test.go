package sprintreplay

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// secretShapes are the shapes TestReplayTestdataHoldsNoSecrets refuses in any testdata byte:
// token prefixes, PEM private keys, emails, IPv4 addresses and home paths. A login at a host
// (user@host, the shape of an ssh target) is refused by userAtHost below.
var secretShapes = map[string]*regexp.Regexp{
	"github token":     regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{8,}|github_pat_`),
	"openai-style key": regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}`),
	"slack token":      regexp.MustCompile(`\bxox[abprs]-`),
	"aws key":          regexp.MustCompile(`\bAKIA[0-9A-Z]{12,}`),
	"bearer":           regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}`),
	"pem private key":  regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	"email":            regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}\b`),
	"ipv4":             regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`),
	"home path":        regexp.MustCompile(`/Users/|/home/`),
}

// loginAtHost is a login name at a host name, standing as its own word: an ssh target such
// as name@bench. A card id at a ref (card@grade.<sha>, card.w1@<sha>, module@v1.2.3) is no
// login at a host: its left side is not a bare word, or its right side is a sha or goes on
// with a dot.
var loginAtHost = regexp.MustCompile("(?:^|[\\s`'\"(=\\[,;:])([a-z][a-z0-9_]{1,31})@([a-z][a-z0-9-]*[a-z0-9])\\b")

var shaWord = regexp.MustCompile(`^[0-9a-f]+$`)

// impersonalLogins are the logins a testdata user@host may name: the scrub's placeholder
// ("user") and the fleet's shared service account ("nova"), neither a person's.
var impersonalLogins = map[string]bool{"user": true, "nova": true}

// userAtHost is every login at a host in b whose login is a person's (not impersonalLogins).
func userAtHost(b []byte) []string {
	var out []string
	for _, ix := range loginAtHost.FindAllSubmatchIndex(b, -1) {
		if ix[1] < len(b) && (b[ix[1]] == '.' || b[ix[1]] == '-') {
			continue
		}
		login, host := string(b[ix[2]:ix[3]]), string(b[ix[4]:ix[5]])
		if shaWord.MatchString(host) || impersonalLogins[login] {
			continue
		}
		out = append(out, login+"@"+host)
	}
	return out
}

func TestUserAtHostShape(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]int{
		"ran on bench `alice@vision`, at": 1, "ssh bob@hetzner9 'ls'": 1,
		"user@bench": 0, "nova@hetzner2": 0, "card@grade.2b7643bb4d36": 0, "x.w1.g35.e20@a5896381c3cd": 0,
		"schema@v2.5.1": 0, "w5@3b0be5f": 0, "mail eve@example.com": 0,
	} {
		if got := len(userAtHost([]byte(in))); got != want {
			t.Errorf("userAtHost(%q) found %d, want %d", in, got, want)
		}
	}
}

// testdataBytes is every testdata file's bytes, a gzip file's inflated.
func testdataBytes(t *testing.T) map[string][]byte {
	t.Helper()
	ents, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join("testdata", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Ext(e.Name()) == ".gz" {
			zr, err := gzip.NewReader(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			if b, err = io.ReadAll(zr); err != nil {
				t.Fatal(err)
			}
		}
		out[e.Name()] = b
	}
	return out
}

func TestReplayTestdataHoldsNoSecrets(t *testing.T) {
	t.Parallel()
	files := testdataBytes(t)
	if len(files) < 2 {
		t.Fatalf("testdata holds %d files, want the log and the attempts table", len(files))
	}
	for name, b := range files {
		for shape, re := range secretShapes {
			if m := re.Find(b); m != nil {
				t.Errorf("testdata/%s holds a %s: %q", name, shape, m)
			}
		}
		for _, m := range userAtHost(b) {
			t.Errorf("testdata/%s holds a login at a host: %q", name, m)
		}
	}
}

// replayed is epoch 21's testdata replayed once.
func replayed(t *testing.T) *Result {
	t.Helper()
	in, err := LoadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	return Replay(in)
}

func TestReplayIsDeterministic(t *testing.T) {
	t.Parallel()
	a, b := replayed(t), replayed(t)
	if len(a.Attempts) == 0 {
		t.Fatal("the replay built no attempt records")
	}
	if a.Digest() != b.Digest() {
		t.Fatalf("two replays of the same input differ: %s and %s", a.Digest(), b.Digest())
	}
	if a.Digest() != pinnedDigest {
		t.Errorf("the replay's digest is %s, pinned %s", a.Digest(), pinnedDigest)
	}
	// Every line is stepped once, in At order then file order.
	if a.Lines != pinnedLines {
		t.Errorf("replayed %d lines, pinned %d", a.Lines, pinnedLines)
	}
}

const (
	pinnedDigest = "91806618d1319847da2e09b8dfe198e5891d7570201e96005c30e1e088250829"
	pinnedLines  = 60620
)

// GroupSets writes one move line for multiple cards. The place replay already expands
// Cards; attempt histories must be the same as if those moves were separate lines.
func TestReplayGroupedMovesMatchExpanded(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	primaries := []string{"alpha", "beta"}
	works := []string{"alpha.w1", "beta.w1"}
	grouped := []sprint.Line{
		{Kind: sprint.LineMove, At: base, Table: "work", Card: primaries[0], Cards: primaries,
			Verb: "brief", To: "machine:ready", Text: map[string]string{"brief": "<brief: 15000 bytes>"}},
		{Kind: sprint.LineMove, At: base.Add(time.Second), Table: "fleet", Card: works[0], Cards: works,
			Verb: "deal", To: "member:ready", Set: map[string]string{"member": "member", "tier": "heavy"}},
		{Kind: sprint.LineMove, At: base.Add(2 * time.Second), Table: "fleet", Card: works[0], Cards: works,
			Verb: "take", From: "member:ready", To: "member:working"},
		{Kind: sprint.LineMove, At: base.Add(3 * time.Second), Table: "fleet", Card: works[0], Cards: works,
			Verb: "progress", From: "member:working", To: "member:working"},
	}
	var expanded []sprint.Line
	for _, line := range grouped {
		for _, card := range line.Cards {
			one := line
			one.Card, one.Cards = card, nil
			expanded = append(expanded, one)
		}
	}
	got, want := Replay(Input{Lines: grouped}), Replay(Input{Lines: expanded})
	if len(got.Attempts) != 2 {
		t.Fatalf("grouped replay has %d attempts, want 2", len(got.Attempts))
	}
	if got.Digest() != want.Digest() {
		t.Fatalf("grouped attempts differ from expanded moves:\n grouped %s\nexpanded %s\n grouped records: %+v\nexpanded records: %+v",
			got.Digest(), want.Digest(), got.Attempts, want.Attempts)
	}
	for _, attempt := range got.Attempts {
		if attempt.Deal.BriefBytes != 15000 || len(attempt.Events) != 2 ||
			attempt.Events[0].What != "take" || attempt.Events[1].What != "progress" {
			t.Errorf("%s has brief %d and events %+v, want brief 15000 and take, progress",
				attempt.Primary, attempt.Deal.BriefBytes, attempt.Events)
		}
	}
}

// Two lines can have the same At. An outcome from a later line is not known at
// an earlier deal even when their timestamps are equal.
func TestReplayDealRatesExcludeLaterSameTimestampOutcome(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	deal := func(card string, at time.Time) sprint.Line {
		return sprint.Line{Kind: sprint.LineMove, At: at, Table: "fleet", Card: card,
			Verb: "deal", To: "member:ready", Set: map[string]string{"member": "member", "route": "route"}}
	}
	same := base.Add(time.Second)
	lines := []sprint.Line{
		deal("alpha.w1", base),
		deal("beta.w1", same),  // alpha's failed outcome is still in the future
		deal("alpha.w2", same), // a retry ends alpha.w1 as failed
		deal("gamma.w1", same), // now alpha's outcome is known
	}
	got := Replay(Input{Lines: lines})
	byPrimary := map[string]*Attempt{}
	for _, a := range got.Attempts {
		byPrimary[a.Primary] = a
	}
	for _, tc := range []struct {
		primary string
		ends    int
	}{{"beta", 0}, {"gamma", 1}} {
		a := byPrimary[tc.primary]
		if a == nil {
			t.Fatalf("%s attempt missing", tc.primary)
		}
		if a.Deal.MemberEnds != tc.ends || a.Deal.RouteEnds != tc.ends ||
			a.Deal.MemberFails != tc.ends || a.Deal.RouteFails != tc.ends {
			t.Errorf("%s deal rates are %+v, want %d known failure(s)", tc.primary, a.Deal, tc.ends)
		}
	}
}

// A fleet withdrawal by another verb also ends the active take. Friend take
// withdraws work from its fleet member; it is not a new fleet take.
func TestReplayWithdrawalEndsFleetTake(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	lines := []sprint.Line{
		{Kind: sprint.LineMove, At: base, Table: "fleet", Card: "alpha.w1", Verb: "deal",
			To: "member:ready", Set: map[string]string{"member": "member"}},
		{Kind: sprint.LineMove, At: base.Add(time.Second), Table: "fleet", Card: "alpha.w1", Verb: "take",
			From: "member:ready", To: "member:working"},
		{Kind: sprint.LineMove, At: base.Add(2 * time.Second), Table: "fleet", Card: "alpha.w1", Verb: "friend take",
			From: "member:working", To: "machine:withdrawn"},
	}
	got := Replay(Input{Lines: lines})
	if len(got.Attempts) != 1 {
		t.Fatalf("got %d attempts, want 1", len(got.Attempts))
	}
	events := got.Attempts[0].Events
	if len(events) != 2 || events[0].What != "take" || events[1].What != "take-ended" {
		t.Errorf("friend take emitted %+v, want take then take-ended", events)
	}
}

func TestReplayEmitsStagingRefusalBeforeRedeal(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	lines := []sprint.Line{
		{Kind: sprint.LineMove, At: base, Table: "fleet", Card: "alpha.w1", Verb: "deal",
			To: "member:ready", Set: map[string]string{"member": "member"}},
		{Kind: sprint.LineMove, At: base.Add(time.Second), Table: "fleet", Card: "alpha.w1", Verb: "take",
			From: "member:ready", To: "member:working"},
		{Kind: sprint.LineMove, At: base.Add(2 * time.Second), Table: "fleet", Card: "alpha.w1", Verb: "finish",
			From: "member:working", To: "member:withdrawn", Set: map[string]string{"staging_take_1": "refused"}},
		{Kind: sprint.LineMove, At: base.Add(3 * time.Second), Table: "fleet", Card: "alpha.w1", Verb: "deal",
			To: "member:ready", Set: map[string]string{"member": "member"}},
	}
	got := Replay(Input{Lines: lines})
	if len(got.Attempts) != 1 {
		t.Fatalf("got %d attempts, want 1", len(got.Attempts))
	}
	a := got.Attempts[0]
	if len(a.Events) != 4 || a.Events[0].What != "take" || a.Events[1].What != "staging" ||
		a.Events[2].What != "take-ended" || a.Events[3].What != "redeal" {
		t.Errorf("staging refusal events %+v, want take, staging, take-ended, redeal", a.Events)
	}
}
