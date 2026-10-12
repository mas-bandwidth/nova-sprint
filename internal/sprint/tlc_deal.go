package sprint

import (
	"strings"
)

// A card that writes a TLA+ model must record a TLC run, so it is dealt only to a
// member that reports it can run TLC (java on its PATH and a tla2tools.jar it names,
// checked once at start and on each restart; docs/SPEC-SPRINT.md section 5, the deal).
// A card needs TLC when its PATHS name a tla/*.tla module or a tla/*.cfg config
// (ModelPaths: the same lines that tier it frontier), or its brief carries a
// `NEEDS: tlc` line. The requirement is a predicate on the deal, like a card's bench:
// while no member that reports TLC is up the card waits in ready and the no-stall rule
// says "needs TLC: no member with TLC is up" (held.go), so a TLA+ card is never dealt
// to a member that cannot run its checker. tla/RouteIndex.tla models the filter
// (Requires), with the reversed witness tlcblind dealing such a card to a member
// without TLC.

// KeyNeedsTLC is the brief header key whose value names TLC as a card's need.
const KeyNeedsTLC = "NEEDS"

// NeedsTLC says the card's work must be able to run TLC: its brief's PATHS line names a
// tla/ model (a .tla module or a tla/ MC config), or its brief carries a `NEEDS: tlc`
// line. Such a card is dealt only to a member whose beat reports TLC. It scans the
// brief's header block itself, without splitting the brief, because the deal asks it of
// every ready card and the deal's allocation bound is a gate
// (TestTickDealAllocatesNoPerCardRouteMapAtScale).
func NeedsTLC(c *Card) bool {
	if c == nil {
		return false
	}
	return briefNeedsTLC(c.F("brief"))
}

// briefNeedsTLC is NeedsTLC over a brief: its header block (the `key: value` lines
// under line 1, as cardhdr.ReadWho and BenchOfBrief read it), key and value in any case.
func briefNeedsTLC(brief string) bool {
	rest := brief
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[i+1:]
	} else {
		return false
	}
	for rest != "" {
		var l string
		l, rest, _ = strings.Cut(rest, "\n")
		k, v, ok := headerKeyValue(l)
		if !ok {
			break
		}
		switch {
		case strings.EqualFold(k, KeyNeedsTLC) && strings.EqualFold(strings.TrimSpace(v), "tlc"):
			return true
		case strings.EqualFold(k, "PATHS") && pathsNeedTLC(v):
			return true
		}
	}
	return false
}

// headerKeyValue reads a `KEY: value` header line by the header's shape (a letter, then
// letters, digits and -, then a colon), without cardhdr.KeyValue's regexp allocation: the
// deal asks it of every ready card.
func headerKeyValue(line string) (key, value string, ok bool) {
	line = strings.TrimRight(line, "\r")
	i := strings.IndexByte(line, ':')
	if i <= 0 {
		return "", "", false
	}
	c := line[0]
	if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
		return "", "", false
	}
	for j := 0; j < i; j++ {
		c := line[j]
		if !(c == '-' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
			return "", "", false
		}
	}
	return line[:i], strings.TrimSpace(line[i+1:]), true
}

// pathsNeedTLC says a PATHS value names tla/ model work: an entry that is a .tla module
// anywhere, or a config, module glob or the directory itself under a tla/ directory
// (ModelPaths). It splits the value itself (no allocation) because the deal asks it of
// every ready card.
func pathsNeedTLC(v string) bool {
	for v != "" {
		var e string
		if i := strings.IndexByte(v, ','); i >= 0 {
			e, v = v[:i], v[i+1:]
		} else {
			e, v = v, ""
		}
		if modelPathEntry(e) {
			return true
		}
	}
	return false
}

// modelPathEntry says one PATHS entry names model work (ModelPaths' rule): a .tla
// anywhere; or, under a tla/ directory, a .cfg, a *, ** or */.tla glob, or the tla
// directory itself.
func modelPathEntry(e string) bool {
	e = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(e), "./"), "/")
	if e == "" {
		return false
	}
	if strings.HasSuffix(e, ".tla") {
		return true
	}
	if e == "tla" {
		return true
	}
	i := strings.LastIndexByte(e, '/')
	if i < 0 {
		return false
	}
	base := e[i+1:]
	return pathElement(e[:i], "tla") &&
		(strings.HasSuffix(base, ".cfg") || base == "*" || base == "**")
}

// pathElement says dirs, a slash-separated path, names the directory dir.
func pathElement(dirs, dir string) bool {
	for dirs != "" {
		var e string
		if i := strings.IndexByte(dirs, '/'); i >= 0 {
			e, dirs = dirs[:i], dirs[i+1:]
		} else {
			e, dirs = dirs, ""
		}
		if e == dir {
			return true
		}
	}
	return false
}

// RunsTLC says the member's fresh beat reports it can run TLC: it names the tla2tools
// jar it runs (Beat.TLCJar) and says so (Beat.TLC). A member that never checked, or
// whose last check failed, reports none and is dealt no TLC card.
func (s *Snapshot) RunsTLC(member string) bool {
	return s != nil && s.TLC[member]
}

// onlyTLC is the members of up that report TLC, in up's order: the pool a card that
// needs TLC is dealt from (docs/SPEC-SPRINT.md section 5, the deal).
func onlyTLC(s *Snapshot, up []string) []string {
	var out []string
	for _, m := range up {
		if s.RunsTLC(m) {
			out = append(out, m)
		}
	}
	return out
}

// withMemberTLC records, from the members' beats, which of them report TLC, so the
// deal's requirement predicate reads them (Snapshot.TLC). A member whose fresh beat
// names a tla2tools.jar and says TLC can run it; a member with no beat, a stale beat,
// or a beat that reports none is not. A nil beats map leaves the set as it is (a step
// that does not read beats), never clearing a set a caller set.
func withMemberTLC(s *Snapshot, beats map[string]Beat) {
	if s == nil || beats == nil {
		return
	}
	set := map[string]bool{}
	for m, b := range beats {
		if b.Fresh(s.Now) && b.TLC && b.TLCJar != "" {
			set[m] = true
		}
	}
	s.TLC = set
}

// needsTLCWaits is why a card that needs TLC waits in ready while no member reporting
// TLC is up: the one line the no-stall rule and the deal's refusal carry
// (docs/SPEC-SPRINT.md section 5, the deal).
func needsTLCWaits() string {
	return "needs TLC: no member with TLC is up"
}

// needsTLCRefusal is the deal's refusal of such a card, with the remedy.
func needsTLCRefusal() string {
	return needsTLCWaits() + "; run: nova-sprint fleet beat <member> on a machine with java and a tla2tools.jar, or take the tla/ paths out of the card's brief"
}
