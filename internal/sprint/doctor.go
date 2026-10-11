package sprint

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/harness"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// The doctor's checks (the owner, 2026-10-11 ~02:10Z: "I think we need to prioritize sprint
// doctor to find stuff like this"; ~02:12Z: "Think of it like ansible for sprint setup, fleet,
// friends, cards everything"). Every stall of the night of 2026-10-10 was found by the owner
// or the seat digging by hand: ready cards no deal took, a tier with no route that serves,
// reader loops hung after the server restarted, members with no push credential and no
// sqlite3 on their loop's PATH, a lander refusing the same head again, a fleet row left from
// a renamed machine. Each is a check here: a desired state (nova-config's declaration and the
// sprint's own rules) held against the live store and each host, one line each, its status,
// its evidence and the exact verb that fixes it. The checks are pure functions over what the
// verb read (DoctorInput): the doctor writes nothing, and a test seeds a twin with the night's
// case and expects the line.

// The doctor's statuses: OK, WARN (a fault that stalls nothing yet), DRIFT (the live state
// differs from the declared one, ansible's "changed": the remedy converges it), FAIL (a stall:
// the verb exits nonzero).
const (
	DoctorOK    = "OK"
	DoctorWarn  = "WARN"
	DoctorDrift = "DRIFT"
	DoctorFail  = "FAIL"
)

// The doctor's areas, the groups `doctor --area` runs one of.
const (
	AreaSprint  = "sprint"
	AreaFleet   = "fleet"
	AreaFriends = "friends"
	AreaReaders = "readers"
	AreaRoutes  = "routes"
	AreaCards   = "cards"
	AreaSeat    = "seat"
)

// DoctorAreas is every area in the order the doctor runs them.
var DoctorAreas = []string{AreaSprint, AreaFleet, AreaFriends, AreaReaders, AreaRoutes, AreaCards, AreaSeat}

// The checks, by name.
const (
	CheckReadyNotDealt  = "ready-not-dealt"
	CheckRoutesPerTier  = "routes-per-tier"
	CheckReaders        = "readers"
	CheckLoopsRedial    = "loops-redial"
	CheckMemberEnv      = "member-env"
	CheckLanderLoop     = "lander-loop"
	CheckFleetRows      = "fleet-rows"
	CheckProviders      = "providers"
	CheckSeat           = "seat"
	CheckFinishFailures = "finish-failures"
	CheckFriends        = "friends"
)

// DoctorFixSafe is the checks whose remedy is idempotent and safe for a later `doctor --fix`
// to run unattended: a loop re-dialled (restarted through its play), the fleet table synced
// to the inventory (fleet sync), a stale fleet row retired. Every other remedy is the seat's.
var DoctorFixSafe = []string{CheckLoopsRedial, CheckFleetRows}

// DoctorCheck is one line of the doctor: its area, the check, its subject (one word), the
// status, the evidence, and the exact verb that fixes it ("-" for none).
type DoctorCheck struct {
	Area     string `json:"area"`
	Check    string `json:"check"`
	Subject  string `json:"subject"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
	Remedy   string `json:"remedy"`
	// FixSafe says the remedy is one a later doctor --fix may run (DoctorFixSafe).
	FixSafe bool `json:"fix_safe,omitempty"`
}

// Line is the check's one line: DOCTOR <status> <check> <subject> <evidence> remedy=<verb>.
func (c DoctorCheck) Line() string {
	subject := strings.Join(strings.Fields(c.Subject), "_")
	return fmt.Sprintf("DOCTOR %s %s %s %s remedy=%s", c.Status, c.Check, cmp.Or(subject, "-"), cmp.Or(oneLine(c.Evidence), "-"), cmp.Or(oneLine(c.Remedy), "-"))
}

// DoctorMachine is one machine row of nova-config as the doctor reads it: its name, the
// login the fleet reaches it by, its secrets seat and its width (Default: none set).
type DoctorMachine struct {
	Name    string `json:"name"`
	User    string `json:"user,omitempty"`
	Seat    string `json:"seat,omitempty"`
	Width   int    `json:"width"`
	Default bool   `json:"default,omitempty"`
}

// MemberProbe is what one probe of a member's host found, over the path the fleet reaches it
// by (ssh, or the local shell on the seat's own host). Tools is each tool the member needs
// (MemberTools, and the harnesses of its routes) by name: where its loop's PATH finds it, ""
// when it does not. FreeKB is the free space of its home, -1 when not read.
type MemberProbe struct {
	Member   string            `json:"member"`
	Host     string            `json:"host"`
	Reached  bool              `json:"reached"`
	Err      string            `json:"err,omitempty"`
	OS       string            `json:"os,omitempty"`
	PathFrom string            `json:"path_from,omitempty"` // "loop": the loop unit's PATH; "login": the login shell's
	Tools    map[string]string `json:"tools,omitempty"`
	// Near is a tool its loop's PATH does not find but the host holds elsewhere (a toolchain
	// under ~/sdk): still a FAIL, as the loop runs the card on its PATH, with where it is.
	Near     map[string]string `json:"near,omitempty"`
	FreeKB   int64             `json:"free_kb"`
	Push     string            `json:"push,omitempty"` // the push credential probe's line
	PushOK   bool              `json:"push_ok"`
	Secrets  string            `json:"secrets,omitempty"` // nova-secrets check's last line
	SecretOK bool              `json:"secrets_ok"`
	Version  string            `json:"version,omitempty"`
}

// MemberTools is every program a member's loop runs a card with, which its loop's PATH must
// find (the owner, 2026-10-11 ~02:25Z: "lacking the thing that should be installed should be
// picked up by sprint dr"; b1-b3 had no sqlite3 and every take there was refused at once,
// "sqlite3 is on no PATH entry of this bench"). sqlite3 is the native budget's usage source
// (pkg/swarm SQLiteBinary); git and tar stage and push the card; make, gcc and go run the
// gates; jq and rsync the gates' scripts; nova-secrets and nova-sprint the loop itself. A
// route's harness is checked beside these for the routes the member launches (MemberHarnesses).
var MemberTools = []string{"git", swarm.SQLiteBinary, "tar", "make", "gcc", "go", "jq", "rsync", "nova-secrets", "nova-sprint"}

// OpenCode is the default harness's program: a route with no headless harness runs under it.
const OpenCode = "opencode"

// DoctorInput is everything one doctor run read: the sprint as the deal plans on it, the
// beats, the declared state from nova-config, and each member host's probe. A field left zero
// was not measured, and its check says so rather than guess.
type DoctorInput struct {
	Snap        *Snapshot // the four tables with the routes, tiers, reader states and friends
	Req         TickReq   // the members' beats and the friends' seats, as the tick reads them
	ReaderBeats map[string]Beat
	FriendBeats map[string]Beat
	// ServerStarted is when the sprint's server started serving, zero when not measured;
	// ServerHow says how it was measured.
	ServerStarted time.Time
	ServerHow     string
	// Machines is nova-config's machine rows, nil when not read (ConfigErr says why).
	Machines  []DoctorMachine
	ConfigErr string
	// Probes is each member host's probe, nil when not run (ProbeErr says why).
	Probes   map[string]MemberProbe
	ProbeErr string
	// Declared is the nova-sprint build the seat runs: a member's installed one differs is drift.
	Declared string
	Push     PushM
	Seat     SeatWaits
}

// The bounds the checks judge by.
const (
	// DoctorReadyBound is how long a card may sit ready before the doctor asks why no deal took it.
	DoctorReadyBound = time.Minute
	// DoctorBeatBound is how old a beat may be before its loop is called hung.
	DoctorBeatBound = 60 * time.Second
	// DoctorRedialGrace is how long after the server starts a loop has to dial it again.
	DoctorRedialGrace = 60 * time.Second
	// DoctorPushBound is how old the push loop's last proof may be.
	DoctorPushBound = 15 * time.Minute
	// DoctorFinishWindow is the window the finish failures are counted in.
	DoctorFinishWindow = time.Hour
	// DoctorStrandedBound is how long a card may sit in review with nothing open on it and no read live.
	DoctorStrandedBound = 10 * time.Minute
	// DoctorDiskWarnKB and DoctorDiskFailKB are a member home's free space bounds.
	DoctorDiskWarnKB = 20 << 20
	DoctorDiskFailKB = 5 << 20
)

// Doctor is every check of the doctor over one read, in area order.
func Doctor(in DoctorInput) []DoctorCheck {
	var out []DoctorCheck
	out = append(out, DoctorFleetRows(in)...)
	out = append(out, DoctorLoopsRedial(in)...)
	out = append(out, DoctorMemberEnv(in)...)
	out = append(out, DoctorFinishFailures(in)...)
	out = append(out, DoctorFriends(in)...)
	out = append(out, DoctorReaders(in)...)
	out = append(out, DoctorRoutesPerTier(in)...)
	out = append(out, DoctorProviders(in)...)
	out = append(out, DoctorReadyNotDealt(in)...)
	out = append(out, DoctorLanderLoop(in)...)
	out = append(out, DoctorSeat(in)...)
	for i := range out {
		out[i].FixSafe = out[i].Status != DoctorOK && slices.Contains(DoctorFixSafe, out[i].Check)
	}
	return out
}

// ok is a check's one OK line for its whole subject.
func okCheck(area, check, subject, evidence string) DoctorCheck {
	return DoctorCheck{Area: area, Check: check, Subject: subject, Status: DoctorOK, Evidence: evidence, Remedy: "-"}
}

// ---- 1. ready-not-dealt -------------------------------------------------------------------

// readyClasses is how a deal's reason is named: the first whose words it carries.
var readyClasses = []struct{ class, words string }{
	{"fleet-off", "the fleet's work is off"},
	{"not-ticked", "the deal deals it now"},
	{"no-launcher", "can launch no route"},
	{"no-launcher", "runs under"},
	{"no-route", "no enabled route"},
	{"no-route", "no machine route"},
	{"no-route", "no route"},
	{"no-route", "rests"},
	{"no-member", "no fleet member is up"},
	{"no-member", "no member is up"},
	{"dependency", "waits for"},
	{"sentinel", "sentinel"},
	{"hold", "unhold"},
	{"hold", "held (add --held)"},
	{"hold", "is held"},
	{"bench", "its bench"},
	{"bench", "BENCH line"},
	{"no-width", "room"},
	{"friend", "friend"},
	{"bound", "bound"},
}

// ReadyClass is the class of a deal's reason a ready card was not dealt (readyClasses).
func ReadyClass(why string) string {
	for typ, class := range map[string]string{NBound: "bound", NNoRoute: "no-route", NNoMember: "no-member", NStarving: "starving", NProviderFunds: "no-route", NAllOutOfCredit: "no-route"} {
		if strings.HasPrefix(why, "judgment "+typ+":") {
			if class == "no-route" && strings.Contains(why, "can launch") {
				return "no-launcher" // the tier's one judgment says which: no route, or none a member launches
			}
			return class
		}
	}
	for _, c := range readyClasses {
		if strings.Contains(why, c.words) {
			return c.class
		}
	}
	return "other"
}

// remedyRE is a verb in a deal's reason: the deal's sentences end in the command that answers them.
var remedyRE = regexp.MustCompile(`nova-(?:sprint|config) [a-z][^;()"]*`)

// remedyIn is the first verb the reason names, trimmed, or def.
func remedyIn(why, def string) string {
	if m := remedyRE.FindString(why); m != "" {
		m = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m), ","))
		if strings.HasSuffix(m, ".") && !strings.HasSuffix(m, "..") {
			m = strings.TrimSuffix(m, ".")
		}
		if i := strings.Index(m, ", or "); i > 0 {
			m = m[:i]
		}
		if i := strings.Index(m, " or "); i > 0 && !strings.Contains(m[:i], "<") {
			m = m[:i]
		}
		return m
	}
	return def
}

// readyDefaultRemedy is a class's verb when its reason names none.
var readyDefaultRemedy = map[string]string{
	"not-ticked":  "nova-sprint seat check (the run loop is not dealing: its tick line)",
	"no-width":    "nova-sprint where (every member is at its width: the deal deals it as a card finishes)",
	"hold":        "nova-sprint unhold <stream>",
	"no-launcher": "nova-sprint fleet up <member> --harnesses <harness>",
	"no-route":    "nova-sprint routes",
	"friend":      "nova-sprint friends",
}

// dealJudgments are the judgment types the tick's deal raises over ready cards (TickDeal's notify).
var dealJudgments = []string{NNoMember, NStarving, NBound, NNoRoute, NProviderFunds, NAllOutOfCredit}

// DoctorReadyNotDealt is check 1: every card ready longer than DoctorReadyBound, grouped by the
// deal's own reason it was not dealt. The reason is the deal's, never derived again here: the
// tick's deal is planned on the read (TickDeal), and a card it places is one the run loop has not
// dealt; a card a judgment of that deal (or one open) names is that judgment's; any other is
// asked of the deal by name (Deal over its id), whose refusal says why in the deal's words.
// A card waiting for width is WARN (a full fleet deals it as a card finishes); every other
// reason stalls it, FAIL.
func DoctorReadyNotDealt(in DoctorInput) (out []DoctorCheck) {
	const area, check = AreaCards, CheckReadyNotDealt
	s := in.Snap
	if s == nil || s.Work == nil || s.Fleet == nil {
		return []DoctorCheck{{Area: area, Check: check, Subject: "-", Status: DoctorWarn, Evidence: "the sprint was not read", Remedy: "nova-sprint where"}}
	}
	var old []*Card
	for _, c := range s.Work.Column(Ready) {
		at := stampAt(c, FieldReadyAt)
		if at.IsZero() || s.Now.Sub(at) >= DoctorReadyBound {
			old = append(old, c)
		}
	}
	if len(old) == 0 {
		return []DoctorCheck{okCheck(area, check, "all", fmt.Sprintf("ready=%d none ready past %s", len(s.Work.Column(Ready)), DoctorReadyBound))}
	}
	why := readyWhys(s, in.Req, old)
	type group struct {
		class, why string
		cards      []string
		oldest     time.Duration
	}
	groups := map[string]*group{}
	for _, c := range old {
		w := why[c.ID]
		norm := strings.ReplaceAll(w, c.ID, "<card>")
		class := ReadyClass(w)
		g := groups[class+"\x00"+norm]
		if g == nil {
			g = &group{class: class, why: norm}
			groups[class+"\x00"+norm] = g
		}
		g.cards = append(g.cards, c.ID)
		if at := stampAt(c, FieldReadyAt); !at.IsZero() {
			g.oldest = max(g.oldest, s.Now.Sub(at))
		}
	}
	keys := slices.Sorted(maps.Keys(groups))
	slices.SortStableFunc(keys, func(a, b string) int { return len(groups[b].cards) - len(groups[a].cards) })
	for _, k := range keys {
		g := groups[k]
		status := DoctorFail
		if g.class == "no-width" || g.class == "friend" {
			status = DoctorWarn
		}
		out = append(out, DoctorCheck{Area: area, Check: check, Subject: g.class, Status: status,
			Evidence: fmt.Sprintf("n=%d oldest=%s cards=%s why=%q", len(g.cards), g.oldest.Round(time.Second), someIDs(g.cards, 5), cutText(g.why, 400)),
			Remedy:   strings.ReplaceAll(remedyIn(g.why, cmp.Or(readyDefaultRemedy[g.class], "nova-sprint card "+g.cards[0])), "<card>", g.cards[0])})
	}
	return out
}

// readyWhys is the deal's reason for each card of cards: the tick's deal planned on s
// (TickDeal), its judgments and the open ones, and the deal asked by name (Deal).
func readyWhys(s *Snapshot, r TickReq, cards []*Card) (why map[string]string) {
	why = map[string]string{}
	defer func() {
		if p := recover(); p != nil { // a doctor reads; a deal that cannot plan on this read is its finding
			for _, c := range cards {
				if why[c.ID] == "" {
					why[c.ID] = fmt.Sprintf("the deal could not be planned on this read: %v", p)
				}
			}
		}
	}()
	r.WakeFriend = nil
	if r.Who == "" {
		r.Who = MachineActor
	}
	tp, _ := TickDeal(s, r)
	placed := map[string]string{}
	for _, u := range tp.Units {
		placed[u.Key] = u.Moved
	}
	judged := map[string]string{}
	judge := func(n Note) {
		if n.Kind != Judgment || !slices.Contains(dealJudgments, n.Type) {
			return
		}
		for _, id := range n.Primaries {
			if judged[id] == "" {
				judged[id] = "judgment " + n.Type + ": " + n.What
			}
		}
	}
	for _, n := range tp.Notes {
		judge(n)
	}
	for _, n := range tp.Updates {
		judge(n)
	}
	for _, u := range tp.Units {
		for _, n := range u.Notes {
			judge(n)
		}
	}
	for _, o := range s.Open {
		judge(o.Note)
	}
	for _, c := range cards {
		switch {
		case placed[c.ID] != "":
			why[c.ID] = "the deal deals it now (" + placed[c.ID] + "): the run loop's tick has not"
		case judged[c.ID] != "":
			why[c.ID] = judged[c.ID]
		default:
			p := Deal(s, DealReq{Sel: Sel{IDs: []string{c.ID}}, Who: MachineActor})
			for _, rf := range p.Refused {
				if rf.Key == c.ID {
					why[c.ID] = rf.Why
				}
			}
			if why[c.ID] == "" && len(p.Units) > 0 {
				why[c.ID] = "the deal deals it now (" + p.Units[0].Moved + ") though the tick's deal left it: the tick deals at most its room and TickMaxDeal a tick"
			}
			if why[c.ID] == "" {
				why[c.ID] = "the deal neither deals nor refuses it"
			}
		}
	}
	return why
}

// someIDs is up to n ids, and how many more.
func someIDs(ids []string, n int) string {
	if len(ids) <= n {
		return strings.Join(ids, ",")
	}
	return strings.Join(ids[:n], ",") + fmt.Sprintf(",+%d", len(ids)-n)
}

// ---- 2. routes-per-tier -------------------------------------------------------------------

// DoctorRoutesPerTier is check 2: for each tier, its enabled routes not resting and the members
// up that can launch each (Launches). A tier with ready cards and no route a member up can
// launch is FAIL (WARN when a friend up serves it: its cards are the friends'), one such route
// is WARN, more OK.
func DoctorRoutesPerTier(in DoctorInput) (out []DoctorCheck) {
	const area, check = AreaRoutes, CheckRoutesPerTier
	s := in.Snap
	if s == nil || s.Fleet == nil || s.Work == nil {
		return nil
	}
	if len(s.Routes) == 0 {
		return []DoctorCheck{{Area: area, Check: check, Subject: "all", Status: DoctorWarn, Evidence: "no route is applied to the store", Remedy: "nova-config apply"}}
	}
	rs, _ := s.withRests()
	ready := map[string]int{}
	for _, c := range rs.Work.Column(Ready) {
		ready[rs.DealTier(c)]++
	}
	tiers := map[string]bool{}
	for _, r := range rs.Routes {
		tiers[r.Tier] = true
	}
	for t := range ready {
		tiers[t] = true
	}
	up := rs.UpMembers()
	for _, tier := range slices.Sorted(maps.Keys(tiers)) {
		enabled, usable := 0, 0
		var resting, unlaunched, parts []string
		for _, r := range rs.Routes {
			if r.Tier != tier || !r.Enabled {
				continue
			}
			enabled++
			if rest, ok := rs.rests[r.Name]; ok {
				resting = append(resting, r.Name+"("+rest.Cause+")")
				continue
			}
			var ms []string
			for _, m := range up {
				if rs.Launches(m, r) {
					ms = append(ms, m)
				}
			}
			if len(ms) == 0 {
				unlaunched = append(unlaunched, r.Name+"(runs under "+cmp.Or(r.Harness, OpenCode)+")")
				continue
			}
			usable++
			parts = append(parts, fmt.Sprintf("%s:%s", r.Name, someIDs(ms, 4)))
		}
		friends, _ := rs.tierServed(tier, nil)
		ev := fmt.Sprintf("ready=%d enabled=%d resting=%d usable=%d", ready[tier], enabled, len(resting), usable)
		if len(parts) > 0 {
			ev += " routes=" + strings.Join(parts, ";")
		}
		if len(resting) > 0 {
			ev += " rest=" + strings.Join(resting, ",")
		}
		if len(unlaunched) > 0 {
			ev += " no-member-launches=" + strings.Join(unlaunched, ",")
		}
		if len(friends) > 0 {
			ev += " friends=" + strings.Join(friends, ",")
		}
		c := DoctorCheck{Area: area, Check: check, Subject: tier, Status: DoctorOK, Evidence: ev, Remedy: "-"}
		switch {
		case usable == 0 && ready[tier] > 0 && len(friends) == 0:
			c.Status = DoctorFail
		case usable == 0 && ready[tier] > 0:
			c.Status = DoctorWarn
		case usable == 1 && ready[tier] > 0:
			c.Status = DoctorWarn
		}
		if c.Status != DoctorOK {
			switch {
			case len(unlaunched) > 0:
				c.Remedy = "nova-sprint fleet up <member> --harnesses <harness> (a member with the harness on its PATH)"
			case len(resting) > 0:
				c.Remedy = "nova-sprint doctor --area routes (the providers lines say the verb that ends each rest)"
			default:
				c.Remedy = "nova-config route add <name> --tier " + tier + " ... && nova-config tier set " + tier + " --routes <name,...> && nova-config apply"
			}
		}
		out = append(out, c)
	}
	return out
}

// ---- 3. readers ---------------------------------------------------------------------------

// readsWaitTypes are the judgments that say reads wait for want of a reader.
var readsWaitTypes = []string{NCannotAsk, NFewReaders, NReadersBehind, NWaitingForReader}

// DoctorReaders is check 3: each reader row up and beating, reading against its width, and the
// reads waiting (asked and not begun, a primary marked waiting for a reader, a cannot-ask or
// few-readers judgment). A reader the store holds up whose beat is older than DoctorBeatBound is
// FAIL (its loop hangs: after the server restart of 2026-10-10 the Linux reader loops beat no
// more and read nothing); reads waiting while every reader up is idle, or none is up, is FAIL.
func DoctorReaders(in DoctorInput) (out []DoctorCheck) {
	const area, check = AreaReaders, CheckReaders
	s := in.Snap
	if s == nil || s.Readers == nil {
		return nil
	}
	now := s.Now
	waiting, asked := 0, 0
	reading := map[string]int{}
	askedOn := map[string]int{}
	for _, c := range s.Readers.Column(Asked, Reading) {
		if c.Col == Reading {
			reading[c.Row]++
		} else {
			askedOn[c.Row]++
			asked++
		}
	}
	marked := 0
	if s.Work != nil {
		for _, c := range s.Work.Column(Review) {
			if c.F(FieldWaitingReader) != "" {
				marked++
			}
		}
	}
	judged := 0
	for _, o := range s.Open {
		if slices.Contains(readsWaitTypes, o.Note.Type) || strings.HasPrefix(o.Note.What, NoEligibleReader) {
			judged++
		}
	}
	waiting = asked + marked
	var up, idleUp, staleUp []string
	for _, r := range s.Readers.Rows() {
		state := ReaderDown
		if s.ReaderStates != nil {
			state = cmp.Or(s.ReaderStates[r], ReaderDown)
		}
		if state == ReaderRetired {
			continue
		}
		b, beaten := in.ReaderBeats[r]
		age := "never"
		var d time.Duration
		if beaten && b.Beaten() {
			d = now.Sub(b.At)
			age = d.Round(time.Second).String()
		}
		width := "-"
		if w := s.ReaderWidth(r); w != math.MaxInt {
			width = itoa(w)
		}
		ev := fmt.Sprintf("state=%s beat=%s reading=%d asked=%d width=%s", state, age, reading[r], askedOn[r], width)
		switch {
		case state == ReaderHeld || state == ReaderAway && !beaten:
			continue // the coordinator's hold, said by the hold itself
		case state == ReaderUp && (!beaten || d > DoctorBeatBound):
			staleUp = append(staleUp, r)
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: r, Status: DoctorFail, Evidence: ev + ": the store holds it up and its loop beats no more", Remedy: loopRemedy(in, r)})
		case state == ReaderUp:
			up = append(up, r)
			if reading[r] == 0 {
				idleUp = append(idleUp, r)
			}
		case beaten && d > DoctorBeatBound && askedOn[r] > 0:
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: r, Status: DoctorFail, Evidence: ev + ": reads are asked of it and its loop beats no more", Remedy: loopRemedy(in, r)})
		}
	}
	if in.Machines != nil {
		friends := map[string]bool{}
		for _, f := range in.Req.Friends {
			friends[f.Name] = true
		}
		for _, r := range s.Readers.Rows() {
			if st := s.ReaderStates[r]; st == ReaderRetired || st == ReaderUp {
				continue
			}
			host, ok := ReaderMachine(r)
			if !ok || friends[host] || slices.ContainsFunc(in.Machines, func(m DoctorMachine) bool { return m.Name == host }) {
				continue
			}
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: r, Status: DoctorDrift,
				Evidence: fmt.Sprintf("state=%s: the reader row names %s, which is no machine of nova-config and no friend", cmp.Or(s.ReaderStates[r], ReaderDown), host), Remedy: "nova-sprint reader retire " + r})
		}
	}
	ev := fmt.Sprintf("up=%d idle=%d stale=%d reads-waiting=%d (asked=%d marked=%d) judgments=%d", len(up), len(idleUp), len(staleUp), waiting, asked, marked, judged)
	switch {
	case waiting+judged > 0 && len(up) == 0:
		out = append(out, DoctorCheck{Area: area, Check: check, Subject: "all", Status: DoctorFail, Evidence: ev + ": reads wait and no reader is up", Remedy: "restart the reader loops (nova-sprint doctor --area readers names each), or nova-sprint reader up <reader>"})
	case waiting > 0 && len(idleUp) == len(up):
		out = append(out, DoctorCheck{Area: area, Check: check, Subject: "all", Status: DoctorFail, Evidence: ev + ": reads wait and every reader up reads nothing (" + someIDs(idleUp, 6) + ")", Remedy: loopRemedy(in, idleUp[0]) + " (each idle reader's loop)"})
	case judged > 0:
		out = append(out, DoctorCheck{Area: area, Check: check, Subject: "all", Status: DoctorWarn, Evidence: ev + ": a judgment says reads wait for a reader", Remedy: "nova-sprint inbox"})
	default:
		out = append(out, okCheck(area, check, "all", ev))
	}
	return out
}

// ---- 4. loops-redial ----------------------------------------------------------------------

// DoctorLoopsRedial is check 4: every member and reader whose last beat is older than the
// server's start and DoctorRedialGrace: its loop did not dial the server again after it
// restarted. A host the probe could not reach is down, WARN (down is not an error when
// actually down); one reached whose loop beats no more is FAIL.
func DoctorLoopsRedial(in DoctorInput) (out []DoctorCheck) {
	const area, check = AreaFleet, CheckLoopsRedial
	s := in.Snap
	if s == nil || s.Fleet == nil {
		return nil
	}
	if in.ServerStarted.IsZero() {
		return []DoctorCheck{{Area: area, Check: check, Subject: "all", Status: DoctorWarn, Evidence: "the server's start was not measured: " + cmp.Or(in.ServerHow, "no server address"), Remedy: "export NOVA_SPRINT_SERVER=<the server's --listen address>"}}
	}
	due := in.ServerStarted.Add(DoctorRedialGrace)
	if s.Now.Before(due) {
		return []DoctorCheck{okCheck(area, check, "all", "the server started "+s.Now.Sub(in.ServerStarted).Round(time.Second).String()+" ago: the loops have until "+DoctorRedialGrace.String()+" to dial it")}
	}
	n := 0
	judge := func(loop, host string, b Beat, beaten bool) {
		n++
		if beaten && !b.At.Before(due) {
			return
		}
		age := "never"
		if beaten && b.Beaten() {
			age = s.Now.Sub(b.At).Round(time.Second).String() + " ago"
		}
		ev := fmt.Sprintf("last-beat=%s server-started=%s ago (%s)", age, s.Now.Sub(in.ServerStarted).Round(time.Second), in.ServerHow)
		c := DoctorCheck{Area: area, Check: check, Subject: loop, Status: DoctorFail, Evidence: ev + ": its loop did not dial the server again", Remedy: loopRemedy(in, loop)}
		if p, probed := in.Probes[host]; probed && !p.Reached {
			c.Status, c.Evidence = DoctorWarn, ev+": its host "+host+" does not answer ("+p.Err+"): down"
		}
		out = append(out, c)
	}
	declared := func(host string) bool {
		return in.Machines == nil || slices.ContainsFunc(in.Machines, func(m DoctorMachine) bool { return m.Name == host })
	}
	for _, m := range s.Members() {
		if s.MemberCtl(m) == nil || !declared(m) {
			continue // a row with no machine record is fleet-rows' FAIL, not a loop's
		}
		b, beaten := in.Req.Beats[m]
		judge("member-"+m, m, b, beaten)
	}
	if s.Readers != nil {
		for _, r := range s.Readers.Rows() {
			if st := s.ReaderStates[r]; st == ReaderRetired || st == ReaderHeld {
				continue
			}
			host, _ := ReaderMachine(r)
			if in.Machines == nil || !declared(host) {
				continue // a friend's reader is her daemon's (friends), a stray row the readers' DRIFT
			}
			b, beaten := in.ReaderBeats[r]
			judge(r, host, b, beaten)
		}
	}
	if len(out) == 0 {
		return []DoctorCheck{okCheck(area, check, "all", fmt.Sprintf("loops=%d every one beat since the server started %s ago (%s)", n, s.Now.Sub(in.ServerStarted).Round(time.Second), in.ServerHow))}
	}
	return out
}

// loopRemedy is the verb that restarts a loop on its host: the unit the loops play installed
// (systemd on Linux, launchd on macOS), named as nova-config's loop record names it.
func loopRemedy(in DoctorInput, loop string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(loop, "member-"), ReaderPrefix)
	login := host
	for _, m := range in.Machines {
		if m.Name == host && m.User != "" {
			login = m.User + "@" + host
		}
	}
	switch in.Probes[host].OS {
	case "Linux":
		return "ssh " + login + " systemctl --user restart nova-loop-" + loop + ".service"
	case "Darwin":
		return "ssh " + login + " launchctl kickstart -k gui/$(id -u)/com.nova.loop." + loop
	}
	return "restart loop " + loop + " on " + host + " (nova-config loop show " + loop + ")"
}

// ---- 5. member-env ------------------------------------------------------------------------

// MemberHarnesses is the harness programs a member launches the enabled routes with: opencode
// for a route with no headless harness, and each headless one its control card names
// (Launches) that an enabled route runs under.
func MemberHarnesses(s *Snapshot, member string) []string {
	set := map[string]bool{}
	for _, r := range s.Routes {
		if !r.Enabled {
			continue
		}
		switch {
		case !harness.IsHeadless(r.Harness):
			set[OpenCode] = true
		case s.Launches(member, r):
			set[r.Harness] = true
		}
	}
	return slices.Sorted(maps.Keys(set))
}

// joinRemedy is the bench play that installs what a member needs.
func joinRemedy(m string) string { return "make -C fleet join HOST=" + m }

// DoctorMemberEnv is check 5: each member's host as its loop runs there, probed over the path
// the fleet reaches it by. FAIL for each tool its loop's PATH does not find (MemberTools and
// MemberHarnesses), for a push credential that does not authenticate (the night's b1-b3: every
// finished card was refused at push), for a secrets seat that does not open, and for a home
// under DoctorDiskFailKB free; WARN for a host that does not answer (down) and under
// DoctorDiskWarnKB; DRIFT for an installed nova-sprint other than the declared one.
func DoctorMemberEnv(in DoctorInput) (out []DoctorCheck) {
	const area, check = AreaFleet, CheckMemberEnv
	s := in.Snap
	if s == nil || s.Fleet == nil {
		return nil
	}
	if in.Probes == nil {
		return []DoctorCheck{{Area: area, Check: check, Subject: "all", Status: DoctorWarn, Evidence: "not measured: " + cmp.Or(in.ProbeErr, "probe not configured"), Remedy: "bin/nsc.sh doctor (the seat's wrapper reads nova-config, which names each member's login)"}}
	}
	fine := 0
	for _, m := range s.Members() {
		ctl := s.MemberCtl(m)
		if ctl == nil {
			continue
		}
		p, probed := in.Probes[m]
		if !probed {
			continue
		}
		if !p.Reached {
			st := DoctorWarn
			if slices.Contains(s.UpMembers(), m) {
				st = DoctorFail // the deal gives it cards it cannot be asked about
			}
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: m, Status: st, Evidence: "host " + p.Host + " does not answer: " + p.Err, Remedy: "ssh " + p.Host + " true (or nova-sprint fleet down " + m + " while it is away)"})
			continue
		}
		bad := 0
		need := append(slices.Clone(MemberTools), MemberHarnesses(s, m)...)
		for _, t := range need {
			if p.Tools[t] != "" {
				continue
			}
			bad++
			near := ""
			if w := p.Near[t]; w != "" {
				near = " (the host holds " + w + ", off that PATH)"
			}
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: m, Status: DoctorFail,
				Evidence: fmt.Sprintf("tool=%s is on no PATH entry of its cards (the loop's PATH from %s, with the bench's toolchain as native adds it)%s: every card that needs it ends at once", t, p.PathFrom, near), Remedy: joinRemedy(m)})
		}
		if !p.PushOK {
			bad++
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: m, Status: DoctorFail,
				Evidence: "push credential: " + cmp.Or(p.Push, "not probed") + ": every finished card is refused at push", Remedy: "ansible-playbook fleet/push-credential.yml -l " + m + " (rowan-tools), then " + joinRemedy(m)})
		}
		if !p.SecretOK {
			bad++
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: m, Status: DoctorFail,
				Evidence: "secrets seat: " + cmp.Or(p.Secrets, "not probed") + ": its loop cannot open its keys", Remedy: joinRemedy(m)})
		}
		switch {
		case p.FreeKB >= 0 && p.FreeKB < DoctorDiskFailKB:
			bad++
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: m, Status: DoctorFail, Evidence: fmt.Sprintf("free=%dGB under %dGB", p.FreeKB>>20, DoctorDiskFailKB>>20), Remedy: "nova-sprint gc (on " + m + ")"})
		case p.FreeKB >= 0 && p.FreeKB < DoctorDiskWarnKB:
			bad++
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: m, Status: DoctorWarn, Evidence: fmt.Sprintf("free=%dGB under %dGB", p.FreeKB>>20, DoctorDiskWarnKB>>20), Remedy: "nova-sprint gc (on " + m + ")"})
		}
		if in.Declared != "" && p.Version != "" && p.Version != in.Declared {
			bad++
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: m, Status: DoctorDrift, Evidence: "installed=" + p.Version + " declared=" + in.Declared, Remedy: joinRemedy(m)})
		}
		if bad == 0 {
			fine++
		}
	}
	if len(out) == 0 {
		out = append(out, okCheck(area, check, "all", fmt.Sprintf("members=%d every tool, push credential, seat and disk in place", fine)))
	}
	return out
}

// ---- 6. lander-loop -----------------------------------------------------------------------

// DoctorLanderLoop is check 6: a card whose head the landing refused that is offered or
// accepted again at that same head (the loop of 2026-10-11, epoch 16: accept, refused on a
// conflict, ready to accept, accept), and a card stranded in review: nothing open on it, no
// read live, no reader awaited, for DoctorStrandedBound.
func DoctorLanderLoop(in DoctorInput) (out []DoctorCheck) {
	const area, check = AreaCards, CheckLanderLoop
	s := in.Snap
	if s == nil || s.Work == nil {
		return nil
	}
	openOn := map[string][]string{}
	for _, o := range s.Open {
		if o.Note.Kind == Judgment {
			openOn[o.Subject()] = append(openOn[o.Subject()], o.Note.Type)
		}
	}
	for _, c := range s.Work.Cards() {
		if !c.Placed() || !LandRefusedAtHead(c) {
			continue
		}
		var again string
		switch {
		case c.Col == Merging:
			again = "accepted again: it is merging"
		case s.Merge != nil && s.Merge.Placed(c.ID) != nil && s.Merge.Placed(c.ID).Col == Queued:
			again = "queued to land again"
		case slices.Contains(openOn[c.ID], NReadyToAccept):
			again = "offered to accept again (" + NReadyToAccept + " is open)"
		}
		if again != "" {
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: c.ID, Status: DoctorFail,
				Evidence: "the landing refused head " + shortSHA(c.F("head")) + " and it is " + again + " at the same head", Remedy: "nova-sprint rework " + c.ID + " --fix 'rebase on the base tip and resolve the conflict'"})
		}
	}
	if s.Readers != nil {
		for _, c := range s.Work.Column(Review) {
			if len(openOn[c.ID]) > 0 || c.F(FieldWaitingReader) != "" {
				continue
			}
			live := false
			for _, rc := range s.Readers.Of(c.ID) {
				if rc.Placed() && (rc.Col == Asked || rc.Col == Reading) {
					live = true
				}
			}
			if live {
				continue
			}
			since := latest(stampAt(c, "finished"), stampAt(c, FieldReadyAt))
			if !since.IsZero() && s.Now.Sub(since) < DoctorStrandedBound {
				continue
			}
			what := "nothing is open on it, no read is asked or reading, and no reader is awaited"
			if n, raised := reviewJudgment(s, c, reviewStep{}); raised {
				what += "; the review rule would raise " + n.Type + " and no tick has"
			}
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: c.ID, Status: DoctorFail,
				Evidence: fmt.Sprintf("stranded in review at head %s for %s: %s", shortSHA(c.F("head")), sinceWord(s.Now, since), what), Remedy: "nova-sprint card " + c.ID + " (then ask, rework or drop it)"})
		}
	}
	if len(out) == 0 {
		out = append(out, okCheck(area, check, "all", fmt.Sprintf("review=%d no refused head offered again, none stranded", len(s.Work.Column(Review)))))
	}
	return out
}

// latest is the later of two times.
func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// sinceWord is how long ago at was, "unknown" for none.
func sinceWord(now, at time.Time) string {
	if at.IsZero() {
		return "unknown"
	}
	return now.Sub(at).Round(time.Second).String()
}

// shortSHA is a head's first 12 characters, "-" for none.
func shortSHA(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return orDash(h)
}

// ---- 7. fleet-rows ------------------------------------------------------------------------

// DoctorFleetRows is check 7: the fleet table against nova-config's machine rows. A fleet row
// whose machine record is gone is FAIL (the night's `hetzner` after its rename to hetzner1: held
// and down for ever, never synced away); every other difference fleet sync converges (FleetDrift:
// a member to add, a width to set) is DRIFT.
func DoctorFleetRows(in DoctorInput) (out []DoctorCheck) {
	const area, check = AreaFleet, CheckFleetRows
	s := in.Snap
	if s == nil || s.Fleet == nil {
		return nil
	}
	if in.Machines == nil {
		return []DoctorCheck{{Area: area, Check: check, Subject: "all", Status: DoctorWarn, Evidence: "nova-config not read: " + cmp.Or(in.ConfigErr, "no config store named"), Remedy: "bin/nsc.sh doctor (NOVA_PG_DSN names the config store)"}}
	}
	var machines []string
	var want []SyncMember
	for _, m := range in.Machines {
		machines = append(machines, m.Name)
		w := m.Width
		if m.Default {
			w = WidthOfCores(in.Req.Beats[m.Name].Cores)
		}
		if w > 0 {
			want = append(want, SyncMember{Name: m.Name, Width: w})
		}
	}
	slices.SortFunc(want, func(a, b SyncMember) int { return strings.Compare(a.Name, b.Name) })
	for _, m := range s.Members() {
		if slices.Contains(machines, m) {
			continue
		}
		ctl := s.MemberCtl(m)
		st := "no control card"
		if ctl != nil {
			st = "status=" + orDash(ctl.F(Status))
		}
		out = append(out, DoctorCheck{Area: area, Check: check, Subject: m, Status: DoctorFail,
			Evidence: fmt.Sprintf("fleet row %s (%s, ready=%d working=%d) has no machine record in nova-config", m, st, s.Fleet.Count(m, Ready), s.Fleet.Count(m, Working)), Remedy: "nova-sprint fleet sync"})
	}
	for _, d := range FleetDrift(s, want, machines) {
		if d.Kind == DriftHold || d.Kind == DriftRemove {
			if !slices.Contains(machines, d.Member) {
				continue // said above as a stale row
			}
		}
		out = append(out, DoctorCheck{Area: area, Check: check, Subject: d.Member, Status: DoctorDrift, Evidence: d.Kind + ": " + d.Line(), Remedy: "nova-sprint fleet sync"})
	}
	if len(out) == 0 {
		out = append(out, okCheck(area, check, "all", fmt.Sprintf("members=%d machines=%d the fleet table matches nova-config", len(s.Members()), len(machines))))
	}
	return out
}

// ---- 8. providers -------------------------------------------------------------------------

// DoctorProviders is check 8: each provider resting, its balance where the poll read one, and
// how many enabled routes its rest takes from the deal. A provider out of credit or refused for
// its key is FAIL (only the owner's payment or key ends it); a transient rest is WARN.
func DoctorProviders(in DoctorInput) (out []DoctorCheck) {
	const area, check = AreaRoutes, CheckProviders
	s := in.Snap
	if s == nil || s.Fleet == nil || len(s.Routes) == 0 {
		return nil
	}
	rests := RouteRests(s.Routes, s.Fleet)
	serving := 0
	for _, row := range ProviderRows(s.Routes, s.Fleet, s.Now) {
		taken, cause := 0, ""
		for _, r := range s.Routes {
			if r.Provider != row.Name || !r.Enabled {
				continue
			}
			if rest, ok := rests[r.Name]; ok && rest.Resting(s.Now) {
				taken++
				cause = cmp.Or(cause, rest.Cause)
			}
		}
		if taken == 0 {
			serving++
			continue
		}
		c := DoctorCheck{Area: area, Check: check, Subject: row.Name, Status: DoctorWarn,
			Evidence: fmt.Sprintf("routes-taken=%d balance=%s state=%s", taken, row.Balance, cutText(row.State, 300)), Remedy: "wait: the rest ends by itself"}
		switch cause {
		case RestCredit:
			c.Status, c.Remedy = DoctorFail, "nova-sprint funded "+row.Name+" --reason '<the payment>'"
		case RestAuth:
			c.Status, c.Remedy = DoctorFail, "nova-sprint routes wake "+row.Name+" --reason '<the key replaced>'"
		case RestCoordinator:
			c.Remedy = "nova-sprint routes wake " + row.Name + " --reason '<why>'"
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		out = append(out, okCheck(area, check, "all", fmt.Sprintf("providers=%d serving", serving)))
	}
	return out
}

// ---- 9. seat ------------------------------------------------------------------------------

// DoctorSeat is check 9: the seat's push loop proven within DoctorPushBound, and the judgments
// past their deadline.
func DoctorSeat(in DoctorInput) (out []DoctorCheck) {
	const area, check = AreaSeat, CheckSeat
	now := time.Now()
	if in.Snap != nil {
		now = in.Snap.Now
	}
	switch p := in.Push; {
	case !p.Measured:
		out = append(out, DoctorCheck{Area: area, Check: check, Subject: "push", Status: DoctorWarn, Evidence: "the push loop is not armed: no holder's push record", Remedy: "nova-sprint seat install"})
	case p.Record.Proven.IsZero() || now.Sub(p.Record.Proven) > DoctorPushBound:
		age := "never"
		if !p.Record.Proven.IsZero() {
			age = now.Sub(p.Record.Proven).Round(time.Second).String() + " ago"
		}
		out = append(out, DoctorCheck{Area: area, Check: check, Subject: "push", Status: DoctorFail,
			Evidence: fmt.Sprintf("holder=%s proven=%s bound=%s %s", p.Holder, age, DoctorPushBound, cutText(PushWhy(p.Holder, p.Record, p.Recorded, now), 200)), Remedy: PushSetup(p.Holder, p.Record, p.Recorded)})
	default:
		out = append(out, okCheck(area, check, "push", fmt.Sprintf("holder=%s proven=%s ago", p.Holder, now.Sub(p.Record.Proven).Round(time.Second))))
	}
	if in.Seat.Overdue > 0 {
		out = append(out, DoctorCheck{Area: area, Check: check, Subject: "judgments", Status: DoctorFail, Evidence: fmt.Sprintf("open=%d overdue=%d %s", in.Seat.Judgments, in.Seat.Overdue, in.Seat.Line(now)), Remedy: "nova-sprint inbox"})
	} else {
		out = append(out, okCheck(area, check, "judgments", fmt.Sprintf("open=%d overdue=0", in.Seat.Judgments)))
	}
	return out
}

// ---- 10. finish-failures ------------------------------------------------------------------

// envClasses are the finish failures that are the member's environment, never the card's: a
// tool missing from its PATH, a push refused or no credential, a launch refused at staging
// (native refused), and a take that left no result (the night's b1-b3, whose sqlite3 was missing).
var envClasses = []string{"missing tool", "no credential", "native refused", "no result"}

// strongEnv are the environment classes that name the member alone: a take that left no result
// counts as the environment's only on a member that has one of these too (otherwise it is the
// route's or the card's, as on every member of the fleet at once).
var strongEnv = []string{"missing tool", "no credential", "native refused"}

// FinishClass is the class of one ended take's line: an environment class (envClasses), the
// provider's, or the harness fault or failure class the rules read it by.
func FinishClass(line string) string {
	l := strings.ToLower(line)
	switch {
	case strings.Contains(l, "on no path entry") || strings.Contains(l, "command not found") || strings.Contains(l, "executable file not found"):
		return "missing tool"
	case strings.Contains(l, "no-credential") || strings.Contains(l, "no credential") || strings.Contains(l, "could not read username") || strings.Contains(l, "authentication failed"):
		return "no credential"
	case strings.Contains(l, "native refused") || strings.Contains(l, "usage source"):
		return "native refused"
	case strings.Contains(l, "push refused") || strings.Contains(l, "push was refused"):
		return "push refused"
	case strings.HasPrefix(l, "staging refused") || strings.Contains(l, "refused at staging"):
		return "staging refused"
	case strings.HasPrefix(l, "no result"):
		return "no result"
	case strings.HasPrefix(l, "provider failure"):
		return "provider failure"
	}
	if c := HarnessFault(line); c != "" {
		return c
	}
	return cmp.Or(FailureClass(line), "failed")
}

// memberFailure is one failure on a member in the window: its class, the card and its line.
type memberFailure struct{ class, card, line string }

// DoctorFinishFailures is check 10: the takes that ended failed on each machine member in the
// last DoctorFinishWindow (provider and no-result takes, launches refused at staging, failed
// finishes), by class. A member three or more of whose failures are mostly one environment
// class is FAIL naming the member, never the card (the night's b1-b3: native refused and no
// result, 27 cards' retry bounds burnt); two or more members failing the same environment class
// is one fleet-wide FAIL beside them.
func DoctorFinishFailures(in DoctorInput) (out []DoctorCheck) {
	const area, check = AreaFleet, CheckFinishFailures
	s := in.Snap
	if s == nil || s.Fleet == nil {
		return nil
	}
	since := s.Now.Add(-DoctorFinishWindow)
	within := func(v string) bool {
		t, err := time.Parse(time.RFC3339, v)
		return err == nil && !t.Before(since)
	}
	by := map[string][]memberFailure{}
	add := func(member, card, line string) {
		if member == "" || IsFriendRow(member) {
			return
		}
		by[member] = append(by[member], memberFailure{class: FinishClass(line), card: card, line: line})
	}
	for _, wc := range s.Fleet.Cards() {
		if wc.F("kind") != "work" {
			continue
		}
		takes, _ := ProviderTakes(wc)
		for _, t := range takes {
			if within(t.Finished) {
				add(t.Member, wc.ID, t.Error)
			}
		}
		staged, _ := StagingTakes(wc)
		for _, t := range staged {
			if within(t.Finished) {
				add(t.Member, wc.ID, "staging refused: "+t.Error)
			}
		}
		if wc.Placed() && wc.Col == DoneFailed && within(wc.F("finished")) {
			add(wc.Row, wc.ID, cmp.Or(wc.F("report"), wc.F("why"), "failed"))
		}
	}
	fleetWide := map[string][]string{}
	for _, m := range slices.Sorted(maps.Keys(by)) {
		fs := by[m]
		count := map[string]int{}
		strong := false
		for _, f := range fs {
			count[f.class]++
			strong = strong || slices.Contains(strongEnv, f.class)
		}
		env := 0
		for _, c := range envClasses {
			if c != "no result" || strong {
				env += count[c]
			}
		}
		// the environment's classes together dominate: a member missing a tool fails at staging
		// and again with no result, the one cause twice (the night's b1-b3)
		top := ""
		for _, c := range strongEnv {
			if count[c] > count[top] {
				top = c
			}
		}
		if len(fs) < 3 || top == "" || 2*env <= len(fs) {
			continue
		}
		var classes []string
		for _, c := range slices.Sorted(maps.Keys(count)) {
			classes = append(classes, fmt.Sprintf("%s=%d", strings.ReplaceAll(c, " ", "-"), count[c]))
		}
		var cards []string
		sample := ""
		for _, f := range fs {
			if slices.Contains(strongEnv, f.class) || f.class == "no result" {
				cards = append(cards, f.card)
			}
			if f.class == top {
				sample = cmp.Or(sample, f.line)
			}
		}
		fleetWide[top] = append(fleetWide[top], m)
		remedy := joinRemedy(m)
		if top == "no credential" {
			remedy = "ansible-playbook fleet/push-credential.yml -l " + m + " (rowan-tools), then " + joinRemedy(m)
		}
		out = append(out, DoctorCheck{Area: area, Check: check, Subject: m, Status: DoctorFail,
			Evidence: fmt.Sprintf("failures=%d in %s, %d of them its environment (%s), mostly %q: the member's, not the cards' (%s); e.g. %s", len(fs), DoctorFinishWindow, env, strings.Join(classes, " "), top, someIDs(slices.Compact(cards), 4), cutText(oneLine(sample), 200)),
			Remedy:   remedy + " (and nova-sprint fleet down " + m + " until it is fixed)"})
	}
	for _, c := range slices.Sorted(maps.Keys(fleetWide)) {
		if ms := fleetWide[c]; len(ms) > 1 {
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: "fleet", Status: DoctorFail,
				Evidence: fmt.Sprintf("%d members fail mostly on %q in %s: %s", len(ms), c, DoctorFinishWindow, strings.Join(ms, ",")), Remedy: "make -C fleet join HOST=<each member named>"})
		}
	}
	if len(out) == 0 {
		n := 0
		for _, fs := range by {
			n += len(fs)
		}
		out = append(out, okCheck(area, check, "all", fmt.Sprintf("failures=%d in %s, none a member's environment", n, DoctorFinishWindow)))
	}
	return out
}

// ---- friends ------------------------------------------------------------------------------

// DoctorFriends is the friends group: each friend row's daemon alive on its host (her beat),
// her session's proof, her working cards against her width and against what her beat says she
// runs, and her hold with its reason. A friend up whose daemon beats no more is FAIL; working
// past her width is FAIL; her report and her row disagreeing is DRIFT (work and its report are
// one act); held or down is WARN with its reason (down is not an error when actually down).
func DoctorFriends(in DoctorInput) (out []DoctorCheck) {
	const area, check = AreaFriends, CheckFriends
	s := in.Snap
	if s == nil || s.Fleet == nil {
		return nil
	}
	seats := in.Req.Friends
	if len(seats) == 0 {
		seats = s.Friends
	}
	if len(seats) == 0 {
		return []DoctorCheck{okCheck(area, check, "all", "no friend in the roster")}
	}
	fine := 0
	for _, f := range seats {
		row := FriendRow(f.Name)
		working := s.Fleet.Count(row, Working)
		b, beaten := in.FriendBeats[f.Name]
		beat := "never"
		var age time.Duration
		if beaten && b.Beaten() {
			age = s.Now.Sub(b.At)
			beat = age.Round(time.Second).String()
		}
		proof := "never"
		if !f.Proof.IsZero() {
			proof = s.Now.Sub(f.Proof).Round(time.Second).String()
		}
		ev := fmt.Sprintf("status=%s beat=%s proof=%s working=%d width=%d running=%d", f.Status, beat, proof, working, f.Width, len(f.Running))
		switch {
		case f.Status == Held || f.Status == Down:
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: f.Name, Status: DoctorWarn, Evidence: ev + " why=" + cmp.Or(f.Why, "-"), Remedy: "nova-sprint friend up " + f.Name + " (once its cause is gone)"})
		case !beaten || age > DoctorBeatBound*2:
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: f.Name, Status: DoctorFail, Evidence: ev + ": her daemon beats no more", Remedy: "launchctl kickstart -k gui/$(id -u)/com.nova.friend-" + f.Name + " (on her host)"})
		case f.Width > 0 && working > f.Width:
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: f.Name, Status: DoctorFail, Evidence: ev + ": working past her width", Remedy: "nova-sprint friend reconcile " + f.Name})
		case b.Friend != nil && len(f.Running) != working:
			out = append(out, DoctorCheck{Area: area, Check: check, Subject: f.Name, Status: DoctorDrift, Evidence: ev + ": her beat's running and her row's working differ", Remedy: "nova-sprint friend reconcile " + f.Name})
		default:
			fine++
		}
	}
	if len(out) == 0 || fine > 0 {
		out = append(out, okCheck(area, check, "up", fmt.Sprintf("friends=%d beating, within width and reconciled=%d", len(seats), fine)))
	}
	return out
}
