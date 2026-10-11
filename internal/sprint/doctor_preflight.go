package sprint

import (
	"cmp"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The doctor's preflight (the owner, 2026-10-11: "Also consider sprint doctor in this
// process"; docs/SPEC-SPRINT.md section 14, "The server"). `doctor --preflight
// <candidate build>` is the release gate's frame, run before any server switch or
// adopt: it holds the candidate's `version` and its `tick --shadow --json` against
// the store this doctor reads, and it names the checks of the gate whose cards have
// not landed yet, so a preflight always says what it did not measure. This is its
// first check, the store's tables against the candidate's TABLES.lock (the v1.2.10
// server ran on a store whose fleet table lacked a locked column); the member check
// is here and the mirror, link and stable checks are later cards that fill in
// preflightLaterChecks.
//
// The rows are a pure function over the facts the verb read (PreflightInput): the
// candidate's version answer and its shadow tick's code and text, so the rule is
// tested without a store or a binary.

// AreaPreflight is the doctor's preflight area, the release gate's frame.
const AreaPreflight = "preflight"

// The preflight's own checks, by name.
const (
	CheckPreflightVersion = "version"
	CheckPreflightSchema  = "schema"
	CheckPreflightMembers = "members"
	CheckPreflightMirrors = "mirrors"
	CheckPreflightLinks   = "links"
)

// mirrorRemedy is the bench play that makes a member's local mirror of a repository: the
// loops play, nova-tools' fleet/loops.yml, whose mirror-refresh row keeps
// nova-bench/mirror/<name>.git fresh on every bench (the play that names the mirror;
// found with grep -rn 'nova-bench/mirror' fleet in nova-tools). Each repository the
// sprint's outstanding cards land on must have one before a switch or adopt.
func mirrorRemedy(member string) string {
	return "ansible-playbook fleet/loops.yml -l " + member
}

// PendingRepos is the distinct repositories the sprint's not-yet-landed work cards land
// on, and those the streams record, as the name a member names its mirror: the last path
// segment of the repository without .git or -mirror (pkg/swarm FindBenchMirror's name).
// These are the repositories a bench must hold a mirror for before the next switch. A
// card that has landed is left out.
func PendingRepos(s *Snapshot) []string {
	if s == nil || s.Work == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		r := MirrorRepoName(v)
		if r != "" && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	for _, c := range s.Work.Cards() {
		if c.Placed() && c.Col != Landed {
			add(s.cardRepo(c))
		}
	}
	for _, stream := range s.Work.Rows() {
		if ctl := s.StreamCtl(stream); ctl != nil {
			for _, r := range Split(ctl.F(FieldRepo)) {
				add(r)
			}
		}
	}
	sort.Strings(out)
	return out
}

// MirrorRepoName is a repository as the name a member names its bench mirror: the last
// path segment of an owner/name, URL or clone path, without .git or -mirror
// (pkg/swarm FindBenchMirror's repoName). "" for no name.
func MirrorRepoName(v string) string {
	v = RepoName(v)
	v = strings.TrimSuffix(v, ".git")
	v = strings.TrimSuffix(v, "-mirror")
	if i := strings.LastIndex(v, "/"); i >= 0 {
		v = v[i+1:]
	}
	return v
}

// preflightLockedShapeMark is the mark a locked-shape refusal carries
// (internal/sprint/store/locked_shape.go through cmd/nova-sprint/shadow.go): the
// store's tables lack a column TABLES.lock names, and the error names the exact
// nova-table commands that mend it.
const preflightLockedShapeMark = "(internal/sprint/TABLES.lock)"

// PreflightInput is what `doctor --preflight` read: the candidate's version answer
// and its shadow tick's result, all of it facts. A field left zero was not answered
// and its row says so.
type PreflightInput struct {
	// Binary is the candidate build's path, the subject of a version row when the
	// candidate did not answer.
	Binary string
	// Version is the candidate's version answer (its version word), "" when it did
	// not answer; VersionErr says why.
	Version    string
	VersionErr string
	// ShadowCode is `tick --shadow --json`'s exit: 0 made the plan, another number
	// refused it. ShadowText is its error text whole, the locked-shape refusal naming
	// the columns and the nova-table commands.
	ShadowCode int
	ShadowText string
	// Probes is each member's host probe, the link fact read (DoctorMemberEnv); nil when the
	// preflight read no host, and the links check then stays a WARN saying it was not measured.
	Probes map[string]MemberProbe
	// Members is each member host's probe and the facts the gate reads beside it, nil
	// when the member check was not measured and its row says so.
	Members []PreflightMember
	// Repos is the repositories the sprint's not-yet-landed cards land on (PendingRepos):
	// each member's probe holds a mirror of each or is one FAIL row. nil or empty is no
	// repository to check and the mirrors check is one OK row.
	Repos []string
}

// PreflightRouteKey is one enabled route a member launches and the secret name its provider
// reads, by name only and never a value (pkg/secrets RouteKey).
type PreflightRouteKey struct {
	Route string
	Key   string
}

// PreflightMember is one member's facts the preflight's member check reads: its host's probe
// (the package's MemberProbe), the harness programs its enabled routes launch beside
// MemberTools, the provider key names those routes read, and its hold or drain state. Held is
// the hold's cause, "" when it is not held; Draining says its fresh beat drains.
type PreflightMember struct {
	Probe     MemberProbe
	Harnesses []string
	Routes    []PreflightRouteKey
	Held      string
	Draining  bool
}

// DoctorPreflight is the release gate's frame over one read: the candidate's version,
// its shadow tick against the store's schema, and the gate's later checks each WARN
// until its card lands (docs/SPEC-SPRINT.md section 14, "The server").
func DoctorPreflight(in PreflightInput) []DoctorCheck {
	out := []DoctorCheck{preflightVersionRow(in), preflightSchemaRow(in)}
	out = append(out, preflightMemberRows(in)...)
	if in.Members != nil {
		out = append(out, preflightMirrorRows(in)...)
	} else {
		out = append(out, DoctorCheck{Area: AreaPreflight, Check: CheckPreflightMirrors, Subject: "-", Status: DoctorWarn,
			Evidence: "not measured by this build: the member probe was not run", Remedy: "-"})
	}
	for _, later := range preflightLaterChecks {
		if later.check == CheckPreflightLinks && in.Probes != nil {
			out = append(out, preflightLinkRows(in.Probes)...)
			continue
		}
		out = append(out, DoctorCheck{Area: AreaPreflight, Check: later.check, Subject: "-", Status: DoctorWarn,
			Evidence: "not measured by this build: " + later.card, Remedy: "-"})
	}
	return out
}

// preflightMirrorRows is the mirrors check as a gate (docs/SPEC-SPRINT.md, doctor: the
// preflight): for every repository the sprint's not-yet-landed cards land on
// (PendingRepos), a member whose probe found no bench mirror for it is one FAIL row
// (area preflight, check mirrors, subject <member>/<repo>), so a switch or adopt is
// refused while a bench that will be dealt those cards cannot stage them. A member the
// probe did not reach, that a cut probe did not measure, or whose probe did not measure
// mirrors at all (nil Mirrors) is left to the member check's own WARN, never read as every
// mirror missing; a set with every mirror in place is one OK row. The card is
// doctor-preflight-checks-bench-mirrors.
func preflightMirrorRows(in PreflightInput) []DoctorCheck {
	repos := append([]string{}, in.Repos...)
	sort.Strings(repos)
	var out []DoctorCheck
	for _, m := range in.Members {
		p := m.Probe
		if !p.Reached || !p.Complete || p.Mirrors == nil {
			continue
		}
		subject := cmp.Or(p.Member, p.Host, "-")
		for _, repo := range repos {
			if p.Mirrors[repo] == "" {
				out = append(out, DoctorCheck{Area: AreaPreflight, Check: CheckPreflightMirrors, Subject: subject + "/" + repo, Status: DoctorFail,
					Evidence: "no bench mirror for " + repo + " on " + subject + ": a card of that repository cannot be staged there",
					Remedy:   mirrorRemedy(subject)})
			}
		}
	}
	if len(out) == 0 {
		out = append(out, okCheck(AreaPreflight, CheckPreflightMirrors, "all", "every member holds a bench mirror for every repository the outstanding cards land on"))
	}
	return out
}

// preflightLinkRows is the links check as a gate (docs/SPEC-SPRINT.md, doctor: member-env and
// the preflight; the card doctor-names-a-member-whose-link-flapped): a member whose probe counted
// DoctorLinkWarnChanges or more link changes in the window is one FAIL row, so a switch or adopt
// is refused while its link is flapping; a member not measured stays a WARN there; and a fleet
// whose links are all steady or quiet is one OK row.
func preflightLinkRows(probes map[string]MemberProbe) []DoctorCheck {
	names := make([]string, 0, len(probes))
	for m := range probes {
		names = append(names, m)
	}
	sort.Strings(names)
	var out []DoctorCheck
	flapping := false
	for _, m := range names {
		p := probes[m]
		switch {
		case p.LinkChanges < 0:
			why := cmp.Or(p.LinkWhy, "not measured")
			out = append(out, DoctorCheck{Area: AreaPreflight, Check: CheckPreflightLinks, Subject: m, Status: DoctorWarn,
				Evidence: "links not measured on " + m + ": " + why, Remedy: "-"})
		case p.LinkChanges >= DoctorLinkWarnChanges:
			flapping = true
			out = append(out, DoctorCheck{Area: AreaPreflight, Check: CheckPreflightLinks, Subject: m, Status: DoctorFail,
				Evidence: fmt.Sprintf("%d link changes in the last 1h, the last: %s", p.LinkChanges, p.LinkLast),
				Remedy:   "check " + m + "'s cable and switch port; on the host: journalctl -k --since -1h | grep -iE 'link (is )?(up|down)|carrier'"})
		}
	}
	if !flapping && len(out) == 0 {
		out = append(out, okCheck(AreaPreflight, CheckPreflightLinks, "-", "every member's link is steady in the last 1h"))
	}
	return out
}

// preflightVersionRow is `<candidate> version`: OK naming the answer, FAIL when the
// candidate did not answer.
func preflightVersionRow(in PreflightInput) DoctorCheck {
	if in.VersionErr != "" {
		return DoctorCheck{Area: AreaPreflight, Check: CheckPreflightVersion, Subject: cmp.Or(in.Binary, "-"),
			Status: DoctorFail, Evidence: "the candidate did not answer version: " + in.VersionErr,
			Remedy: "build the candidate and run: " + cmp.Or(in.Binary, "<binary>") + " version"}
	}
	return okCheck(AreaPreflight, CheckPreflightVersion, in.Version, "the candidate answered version "+in.Version)
}

// preflightSchemaRow is the candidate's shadow tick against the store: OK when it
// planned the tick; a locked-shape refusal one FAIL whose evidence is the shadow's
// error text whole and whose remedy is the commands it names and the adopt's schema
// step; any other shadow failure one FAIL with its text.
func preflightSchemaRow(in PreflightInput) DoctorCheck {
	subject := cmp.Or(in.Version, "-")
	if in.ShadowCode == 0 {
		return okCheck(AreaPreflight, CheckPreflightSchema, subject, "the shadow tick passed: the store's tables hold every column TABLES.lock names")
	}
	c := DoctorCheck{Area: AreaPreflight, Check: CheckPreflightSchema, Subject: subject, Status: DoctorFail,
		Evidence: cmp.Or(in.ShadowText, "the shadow tick failed with no text"), Remedy: "-"}
	if strings.Contains(in.ShadowText, preflightLockedShapeMark) {
		c.Remedy = "run the commands above on a STOPPED machine, or the adopt's schema step"
	}
	return c
}

// preflightLaterChecks is the release gate's later checks, one row each saying this
// build does not measure it, until the card that measures it fills the check in: links
// and stable. Mirrors is measured (preflightMirrorRows). One table, so --preflight's
// output always says what it did not check.
var preflightLaterChecks = []struct{ check, card string }{
	{"links", "doctor-names-a-member-whose-link-flapped"},
	{"stable", "doctor-stable-checks-the-stable-pair"},
}

// preflightMemberRows is the member check: each member's host as its loop runs there, held
// against what the candidate needs before a switch or adopt (docs/SPEC-SPRINT.md, doctor, the
// preflight's member check). FAIL for a MemberTools tool or a harness its enabled routes launch
// the loop's PATH does not find, a push credential that does not authenticate, a secrets seat
// that does not open, a provider key name its enabled routes read that seat does not hold (the
// route and the name both named), and a member whose installed nova-sprint is older than the
// release before the candidate's or newer than the candidate's; WARN for a host that does not
// answer (a switch with a member away is allowed), a probe the budget cut, java or
// /opt/tla/tla2tools.jar absent (only some benches run TLC), a home under DoctorDiskWarnKB,
// and a member held or draining. A member with no probe at all is the whole check unmeasured.
func preflightMemberRows(in PreflightInput) (out []DoctorCheck) {
	const area, check = AreaPreflight, CheckPreflightMembers
	if in.Members == nil {
		return []DoctorCheck{{Area: area, Check: check, Subject: "-", Status: DoctorWarn,
			Evidence: "not measured by this build: the member probe was not run", Remedy: "bin/nsc.sh doctor (the seat's wrapper reads nova-config, which names each member's login)"}}
	}
	cand, candOK := releaseOrdinal(in.Version)
	ok := 0
	for _, m := range in.Members {
		p := m.Probe
		subject := cmp.Or(p.Member, p.Host, "-")
		if !p.Reached {
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: subject, Status: DoctorWarn,
				Evidence: "host " + cmp.Or(p.Host, subject) + " does not answer: " + cmp.Or(p.Err, "no answer"),
				Remedy:   "ssh " + cmp.Or(p.Host, subject) + " true (or nova-sprint fleet down " + subject + " while it is away)"})
			continue
		}
		if !p.Complete {
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: subject, Status: DoctorWarn,
				Evidence: "host " + cmp.Or(p.Host, subject) + " was not answered in full within the probe's budget: nothing is judged from a cut probe",
				Remedy:   "nova-sprint doctor --area fleet (again, once the host answers in time)"})
			continue
		}
		bad := 0
		fail := func(evidence, remedy string) {
			bad++
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: subject, Status: DoctorFail, Evidence: evidence, Remedy: remedy})
		}
		warn := func(evidence, remedy string) {
			bad++
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: subject, Status: DoctorWarn, Evidence: evidence, Remedy: remedy})
		}
		for _, t := range append(append([]string{}, MemberTools...), m.Harnesses...) {
			if p.Tools[t] == "" {
				fail("tool="+t+" is on no PATH entry of its cards (the loop's PATH from "+p.PathFrom+"): the candidate cannot serve a card that needs it", joinRemedy(subject))
			}
		}
		if !p.PushOK {
			fail("push credential: "+cmp.Or(p.Push, "not probed")+": every finished card is refused at push",
				"ansible-playbook fleet/push-credential.yml -l "+subject+" (rowan-tools), then "+joinRemedy(subject))
		}
		if !p.SecretOK {
			fail("secrets seat: "+cmp.Or(p.Secrets, "not probed")+": its loop cannot open its keys", joinRemedy(subject))
		}
		if p.SecretNames == nil && len(m.Routes) > 0 {
			warn("the seat's key names were not measured: nova-secrets names is a nova-tools candidate (a route's provider key cannot be held against the seat)",
				"nova-sprint doctor --area fleet")
		} else {
			held := map[string]bool{}
			for _, n := range p.SecretNames {
				held[n] = true
			}
			for _, rk := range m.Routes {
				if rk.Key == "" || held[rk.Key] {
					continue
				}
				fail("route "+rk.Route+" reads "+rk.Key+", which the member's secrets seat does not hold: the candidate cannot serve it",
					"nova-secrets seat inject (or nova-config apply) for "+rk.Key+" on "+subject)
			}
		}
		if p.Java == "" || p.TLA == "absent" {
			warn("java="+cmp.Or(p.Java, "-")+" tla2tools.jar="+cmp.Or(p.TLA, "absent")+": only some benches run TLC", joinRemedy(subject))
		}
		if m.Held != "" {
			warn("held: "+m.Held+": the candidate will be dealt nothing here", "nova-sprint fleet up "+subject+" (clears its hold)")
		}
		if m.Draining {
			warn("draining: its fresh beat says it takes no new card until its running cards end",
				"restart its loop (nova-sprint fleet up "+subject+"; a restart bounds the drain)")
		}
		switch {
		case p.FreeKB >= 0 && p.FreeKB < DoctorDiskFailKB:
			fail("free="+strconv.FormatInt(p.FreeKB>>20, 10)+"GB under "+strconv.FormatInt(DoctorDiskFailKB>>20, 10)+"GB", "nova-sprint gc (on "+subject+")")
		case p.FreeKB >= 0 && p.FreeKB < DoctorDiskWarnKB:
			warn("free="+strconv.FormatInt(p.FreeKB>>20, 10)+"GB under "+strconv.FormatInt(DoctorDiskWarnKB>>20, 10)+"GB", "nova-sprint gc (on "+subject+")")
		}
		if p.Version != "" && candOK {
			if mv, okv := releaseOrdinal(p.Version); okv {
				switch {
				case mv > cand:
					fail("installed="+p.Version+" candidate="+in.Version+": a switch backwards past a member", joinRemedy(subject))
				case cand-mv >= 2:
					fail("installed="+p.Version+" candidate="+in.Version+": the candidate serves members one release back", joinRemedy(subject))
				}
			}
		}
		if bad == 0 {
			ok++
		}
	}
	if len(out) == 0 {
		out = append(out, okCheck(area, check, "all", strconv.Itoa(ok)+" members every prerequisite in place"))
	}
	return out
}

// PreflightMembers builds the member check's facts from the sprint and the probes: each
// probed member's harnesses, its enabled routes' provider key names, its hold's cause and
// whether its beat drains. A nil sprint leaves those facts empty, and the member rows still
// gate the probe's own facts. The probes are the same map the links check reads.
func PreflightMembers(s *Snapshot, probes map[string]MemberProbe) []PreflightMember {
	if len(probes) == 0 {
		return nil // no probe is no member measured: the check stays the WARN that says so
	}
	names := make([]string, 0, len(probes))
	for m := range probes {
		names = append(names, m)
	}
	sort.Strings(names)
	out := make([]PreflightMember, 0, len(names))
	for _, m := range names {
		pm := PreflightMember{Probe: probes[m]}
		if s != nil {
			pm.Harnesses = MemberHarnesses(s, m)
			pm.Routes = MemberRouteKeys(s, m)
			if ctl := s.MemberCtl(m); ctl != nil && ctl.F("held") != "" {
				pm.Held = cmp.Or(ctl.F(FieldHeldReason), "held by "+cmp.Or(ctl.F(FieldHeldBy), "the coordinator"))
			}
			pm.Draining = s.Draining(m)
		}
		out = append(out, pm)
	}
	return out
}

// MemberRouteKeys is each enabled route the member launches and the secret name its provider
// reads (<PROVIDER>_API_KEY, pkg/secrets RouteKey's rule), by name only and never a value: the
// key names the member's seat must hold for the candidate to serve those routes.
func MemberRouteKeys(s *Snapshot, member string) []PreflightRouteKey {
	if s == nil {
		return nil
	}
	var out []PreflightRouteKey
	for _, r := range s.Routes {
		if !r.Enabled || !s.Launches(member, r) {
			continue
		}
		if p := strings.TrimSpace(r.Provider); p != "" {
			out = append(out, PreflightRouteKey{Route: r.Name, Key: strings.ToUpper(p) + "_API_KEY"})
		}
	}
	return out
}

// releaseOrdinal is a version's ordering: vMAJOR.MINOR.PATCH as one number, false when it is
// no release.
func releaseOrdinal(v string) (int, bool) {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return 0, false
	}
	n := [3]int{}
	for i, p := range parts {
		x, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return 0, false
		}
		n[i] = x
	}
	return n[0]*1000000 + n[1]*1000 + n[2], true
}
