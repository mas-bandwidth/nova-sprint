package sprint

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// The settings gate (v1.2.17, start-refuses-a-non-working-setting; the owner, 2026-10-13:
// "OK these MUST be fixed. blocker priority."; standing, 2026-10-11: "Defaults should always
// be 'the sprint machine works' not 'it's broken until you find this setting and change
// it'"). Epoch 20 started with sprint:git_identity empty and the lander could not commit for
// 20 minutes; earlier clears left read_cards off. The clear writes the working defaults
// (clear-sets-every-working-default-in-the-new-epoch); this is the second wall: whatever path
// left a setting non-working (a store migrated from an older binary, a hand edit, a column
// added later), the machine never STARTS on it unless a set of this epoch chose it.
//
// SettingsWorking reads every setting the design depends on and returns one SettingFinding for each whose
// effective value leaves the machine degraded, with the verb that fixes it. start
// (cmd/nova-sprint/run.go startSettingsGate) refuses, exit 1, nothing written, on a finding
// no set of this epoch chose, and starts with one WARN line on one a set chose.
//
// tla/MachineReady.tla Start, StartRefusedSettings (StartOnlyWorking, DeliberateSetStarts)

// SettingFinding is one setting whose effective value leaves the machine degraded: its name (the
// work table's property, or nova-config's sprint row field), its value as read, why it does
// not work, and the verb that makes it work. SetBy is who chose the value by set in this
// epoch and when ("" for a value no set of this epoch wrote): the gate warns on it and
// refuses on the rest. Live says the finding is the fleet's live state, not a setting (the
// readers up for a tier, and so whether reads_needed can be met): the gate always warns on
// it and never refuses, since readers come up after a start (the coordinator, 2026-10-13:
// the owner's ruling covers settings left off or empty after a clear, not who is up).
type SettingFinding struct {
	Setting string `json:"setting"`
	Value   string `json:"value"`
	Why     string `json:"why"`
	Verb    string `json:"verb"`
	SetBy   string `json:"set_by,omitempty"`
	Live    bool   `json:"live,omitempty"`
}

// WorkingBars is what the gate reads beside the snapshot: the server's fresh record, whether
// one is up and whether it lands (run --land). The host's global git user.email is not read:
// start runs on the seat's host, the lander commits on the server's, so the doctor's
// equal-to-the-owner rule (DoctorGitIdentity) is the doctor's, run where the lander runs.
type WorkingBars struct {
	ServerUp    bool
	ServerLands bool
}

// The names of the settings the gate reads that are not work table properties: nova-config's
// sprint row fields, the readers the reads need, and the server's landing.
const (
	SettingGitIdentity = "git_identity"
	SettingMaxAttempts = "max_attempts"
	SettingReaders     = "readers"
	SettingLand        = "land"
)

// workingCheck is one setting's predicate: nil Check for a setting that cannot leave the
// machine non-working (every value set takes runs the machine, at worst slower or louder).
type workingCheck struct {
	Check func(s *Snapshot, b WorkingBars) []SettingFinding
	// BySet says a set of the epoch may choose the value (a work table property); a
	// nova-config field or the server's flag is never chosen by set.
	BySet bool
}

// workingChecks is every setting's declaration, keyed by its name: every property set
// writes on the work table and every outside setting the machine depends on.
// TestEveryDefaultsRowDeclaresWhetherItCanBeNonWorking fails on a setting with no entry.
var workingChecks = map[string]workingCheck{
	PropReadCards:         {Check: readCardsWorking, BySet: true},
	PropFleet:             {Check: sidesWorking(PropFleet), BySet: true},
	PropFriends:           {Check: sidesWorking(PropFriends), BySet: true},
	PropFleetTiers:        {Check: tiersWorking(PropFleetTiers), BySet: true},
	PropFriendsTiers:      {Check: tiersWorking(PropFriendsTiers), BySet: true},
	PropReadsNeeded:       {Check: readsNeededWorking, BySet: true},
	SettingReaders:        {Check: readersWorking},
	SettingGitIdentity:    {Check: gitIdentityWorking},
	SettingMaxAttempts:    {Check: maxAttemptsWorking},
	SettingLand:           {Check: landWorking},
	PropReadTier:          {BySet: true},
	PropDealtMax:          {BySet: true},
	PropGoLanes:           {BySet: true},
	PropFriendIdle:        {BySet: true},
	PropLandingStallBound: {BySet: true},
	PropDriftCommits:      {BySet: true},
	PropDriftHours:        {BySet: true},
	PropReworkPriority:    {BySet: true},
	PropAlarmReview:       {BySet: true},
	PropAlarmMerging:      {BySet: true},
	PropAlarmFleet:        {BySet: true},
	PropAlarmReady:        {BySet: true},
	// the rows clear-sets-every-working-default added (sprint.Defaults): every value set
	// takes runs the machine, but a missing base after a clear
	PropFriendFinish:   {BySet: true},
	PropPeggedBound:    {BySet: true},
	PropFriendStartMax: {},
	PropIdentical:      {},
	PropReadyMax:       {},
	PropLowBalanceUsd:  {},
	PropTwinBriefs:     {},
	PropSprintBase:     {Check: sprintBaseWorking, BySet: true},
}

// WorkingDeclared says the setting has a declaration in the gate (a predicate, or none
// because no value of it can stop the machine).
func WorkingDeclared(name string) bool {
	_, ok := workingChecks[name]
	return ok
}

// WorkingCanBeNonWorking says the setting has a predicate: some value of it leaves the
// machine degraded.
func WorkingCanBeNonWorking(name string) bool { return workingChecks[name].Check != nil }

// SettingsWorking is every finding of every setting, sorted by setting, each with SetBy filled from
// the epoch's set record (PropSetBy) when a set of this epoch chose the value.
//
// tla/MachineReady.tla Gate
func SettingsWorking(s *Snapshot, b WorkingBars) []SettingFinding {
	var out []SettingFinding
	by := SetByRecord(s)
	for _, w := range workingChecks {
		if w.Check == nil {
			continue
		}
		for _, f := range w.Check(s, b) {
			if workingChecks[f.Setting].BySet {
				f.SetBy = by[f.Setting]
			}
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Setting < out[j].Setting })
	return out
}

// workProp is the work table's property as read, "" when absent.
func workProp(s *Snapshot, name string) string {
	if s == nil || s.Work == nil {
		return ""
	}
	v, _ := s.Work.Prop(name)
	return v
}

func readCardsWorking(s *Snapshot, _ WorkingBars) []SettingFinding {
	if s.ReadCardsOn() {
		return nil
	}
	return []SettingFinding{{Setting: PropReadCards, Value: workProp(s, PropReadCards), Why: "read cards are off: the deal asks no read as a card and reviews wait on the retired readers table", Verb: "nova-sprint set --read-cards on"}}
}

// sidesWorking is fleet's and friends' predicate: both switched off deals no work card to
// anyone, each named with its own verb.
func sidesWorking(prop string) func(*Snapshot, WorkingBars) []SettingFinding {
	return func(s *Snapshot, _ WorkingBars) []SettingFinding {
		if !s.FleetOff() || !s.FriendsOff() {
			return nil
		}
		flag := "--" + prop
		return []SettingFinding{{Setting: prop, Value: SwitchOff, Why: "fleet and friends are both off: the deal hands no work card to anyone", Verb: "nova-sprint set " + flag + " " + SwitchOn}}
	}
}

// liveCards is the epoch's primaries not landed, placed on the work table, in id order.
func liveCards(s *Snapshot) []*Card {
	if s == nil || s.Work == nil {
		return nil
	}
	var out []*Card
	for _, c := range s.Work.Cards() {
		if !c.Placed() || c.Col == Landed || c.F("kind") == "sentinel" || c.F("kind") == "wait" {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// workTiersNeeded is the work tiers the epoch's cards not landed are dealt on, frontier
// counted like every tier (a frontier card is drawn from its routes like every tier).
func workTiersNeeded(s *Snapshot) []string {
	var out []string
	for _, c := range liveCards(s) {
		if t := CardTier(c); !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	slices.Sort(out)
	return out
}

// tiersWorking is fleet_tiers' and friends_tiers' predicate: a tier a card of the epoch
// needs that no side switched on takes; each side on whose tiers leave it out is named.
func tiersWorking(prop string) func(*Snapshot, WorkingBars) []SettingFinding {
	return func(s *Snapshot, _ WorkingBars) []SettingFinding {
		if s.FleetOff() && s.FriendsOff() {
			return nil // the sides' own finding names it
		}
		var unserved []string
		for _, t := range workTiersNeeded(s) {
			if (!s.FleetOff() && s.FleetTakes(t)) || (!s.FriendsOff() && s.FriendsTake(t)) {
				continue
			}
			unserved = append(unserved, t)
		}
		if len(unserved) == 0 {
			return nil
		}
		on := !s.FleetOff()
		if prop == PropFriendsTiers {
			on = !s.FriendsOff()
		}
		if !on {
			return nil
		}
		flag := "--" + strings.ReplaceAll(prop, "_", "-")
		return []SettingFinding{{Setting: prop, Value: workProp(s, prop), Why: "no side switched on takes tier " + strings.Join(unserved, ",") + ", which a card of the epoch needs", Verb: "nova-sprint set " + flag + " " + TiersAll}}
	}
}

// readersServing is how many readers up serve the read tier: the reader rows up that serve it
// (readerServesTier) and the friends who may read with the reader role and the tier.
func readersServing(s *Snapshot, tier string) int {
	n := 0
	if s.Readers != nil {
		for _, rd := range s.UpReaders() {
			if s.readerServesTier(rd, tier) {
				n++
			}
		}
	}
	for _, f := range s.Friends {
		if friendHasRole(f, RoleReader) && friendCanRead(s, f) && (len(f.Tiers) == 0 || slices.Contains(f.Tiers, tier)) {
			n++
		}
	}
	return n
}

// readNeeds is, for each read tier a card of the epoch not landed needs read, the most reads
// a card of it needs (ReadsNeededIn); a tier no card needs a read of is absent.
func readNeeds(s *Snapshot) map[string]int {
	out := map[string]int{}
	for _, c := range liveCards(s) {
		if need := ReadsNeededIn(s, c); need > 0 {
			t := s.readTierOf(c)
			out[t] = max(out[t], need)
		}
	}
	return out
}

func sortedKeys(m map[string]int) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

// readersWorking (live state, a WARN): a read tier a card of the epoch needs that no reader up serves.
func readersWorking(s *Snapshot, _ WorkingBars) []SettingFinding {
	needs := readNeeds(s)
	var none []string
	for _, t := range sortedKeys(needs) {
		if readersServing(s, t) == 0 {
			none = append(none, t)
		}
	}
	if len(none) == 0 {
		return nil
	}
	return []SettingFinding{{Setting: SettingReaders, Value: "none", Why: "no reader up serves tier " + strings.Join(none, ",") + ", which a card of the epoch needs read: its review never ends", Verb: "nova-sprint reader set <reader> --tiers " + strings.Join(none, ","), Live: true}}
}

// readsNeededWorking (live state, a WARN): the sprint's reads count set above the readers that serve a tier a
// card of the epoch needs (a tier with none is the readers' finding).
func readsNeededWorking(s *Snapshot, _ WorkingBars) []SettingFinding {
	v := workProp(s, PropReadsNeeded)
	if !slices.Contains(readsWords, v) {
		return nil
	}
	n, _ := strconv.Atoi(v)
	needs := readNeeds(s)
	var short []string
	for _, t := range sortedKeys(needs) {
		if have := readersServing(s, t); have > 0 && n > have {
			short = append(short, fmt.Sprintf("%s (%d up)", t, have))
		}
	}
	if len(short) == 0 {
		return nil
	}
	return []SettingFinding{{Setting: PropReadsNeeded, Value: v, Why: "every card in review needs " + v + " reads, above the readers up of " + strings.Join(short, ", ") + ": no card of it is accepted", Verb: "nova-sprint set --reads " + ReadTierDefault, Live: true}}
}

// GitIdentityRemedy and MaxAttemptsRemedy are the nova-config verbs that give the sprint row a
// working git_identity and max_attempts, as the v1.2 release line's nova-config takes them
// (the installed v1.2.16 and nova-tools sprint/v1.2.17-tools: the sprint kind's fields are
// its flags, and apply takes --kind; nova-tools main has no such sprint field and refuses
// the flag). Every refusal of the identity names the first (start's gate, the doctor, the
// lander); TestTheNovaConfigRemediesNameRealFlags holds both to pkg/config's sprint kind.
const GitIdentityRemedy = "nova-config sprint set --git_identity '" + config.DefaultGitIdentity + "' --actor <you> && nova-config apply --kind sprint --actor <you>"

var MaxAttemptsRemedy = fmt.Sprintf("nova-config sprint set --max_attempts %d --actor <you> && nova-config apply --kind sprint --actor <you>", AttemptsDefault)

// gitIdentityWorking: none or unparseable (DoctorGitIdentity's first rule): the landing
// refuses to commit. Its second rule, an email equal to the host's global user.email, is the
// doctor's alone (WorkingBars).
func gitIdentityWorking(s *Snapshot, _ WorkingBars) []SettingFinding {
	if _, ok := config.ParseGitIdentity(s.GitIdentity); !ok {
		return []SettingFinding{{Setting: SettingGitIdentity, Value: orDash(s.GitIdentity), Why: "the sprint row names no git_identity it can parse: the landing refuses to commit", Verb: GitIdentityRemedy}}
	}
	return nil
}

// sprintBaseWorking: a cleared epoch with no base recorded (the clear carries the old
// epoch's, so none was set there either): add and brief hold no card to the sprint's
// branch. The first epoch, which init leaves with none, is not judged (add holds dev
// alone until set --base). It WARNs and never refuses (Live): a sprint that lands on main
// or dev can record no base at all (set --base refuses a protected branch), so a refusal
// would keep it from ever starting after a clear.
func sprintBaseWorking(s *Snapshot, _ WorkingBars) []SettingFinding {
	if s == nil || s.Epoch == 0 || SprintBase(s) != "" {
		return nil
	}
	return []SettingFinding{{Setting: PropSprintBase, Value: "-", Why: "the sprint records no base after a clear: no card is held to the branch every stream lands on (none is right for a sprint that lands on main or dev)", Verb: "nova-sprint set --base sprint/<name>", Live: true}}
}

// maxAttemptsWorking: nova-config's max_attempts applied but not a whole number from 1 to
// AttemptsMax; absent is the default and works.
func maxAttemptsWorking(s *Snapshot, _ WorkingBars) []SettingFinding {
	v := strings.TrimSpace(s.MaxAttempts)
	if v == "" {
		return nil
	}
	if _, ok := maxAttemptsOf(v); ok {
		return nil
	}
	return []SettingFinding{{Setting: SettingMaxAttempts, Value: v, Why: fmt.Sprintf("max_attempts is not a whole number from 1 to %d", AttemptsMax), Verb: MaxAttemptsRemedy}}
}

// LandVerb is the verb that makes a server land: the server unit (install server) lands by
// default since v1.2.17; on the fleet it is installed again and its unit restarted.
const LandVerb = "nova-sprint install server --listen <address:port>, then restart its unit (the server lands by default; by hand: nova-sprint run --listen <address:port>)"

// landWorking: a server up whose run lands nothing (run --land=false); no server up is not
// this setting's finding (start says nothing is ticking).
func landWorking(_ *Snapshot, b WorkingBars) []SettingFinding {
	if !b.ServerUp || b.ServerLands {
		return nil
	}
	return []SettingFinding{{Setting: SettingLand, Value: "false", Why: "the server lands nothing (run --land=false): accepted cards are never merged", Verb: LandVerb}}
}

// PropSetBy is the work table's record of the settings a set of this epoch chose: one
// property, "name=who@time|value" entries joined by ";" in name order, written by set beside the
// values it sets (SetByWrite), so the gate tells a deliberate degraded run from a value no
// one chose. A clear starts the epoch with none, as it starts every property.
const PropSetBy = "set_by"

// SetByRecord is the epoch's set record by setting: "who at time" for each setting a set of
// this epoch wrote whose value is still the one that set wrote (a value written after it by
// anything but set is no set's choice).
func SetByRecord(s *Snapshot) map[string]string {
	out := map[string]string{}
	for _, e := range strings.Split(workProp(s, PropSetBy), ";") {
		name, rest, ok := strings.Cut(e, "=")
		if !ok || name == "" {
			continue
		}
		who, rest, _ := strings.Cut(rest, "@")
		at, value, _ := strings.Cut(rest, "|")
		if value != workProp(s, name) {
			continue
		}
		out[name] = who + " at " + at
	}
	return out
}

// SetByWrite is the write of the epoch's set record for the property writes a set plans:
// each gated setting (a predicate, chosen by set) written now by who at the snapshot's
// clock, merged into the record as read; none when the set writes no gated setting.
//
// tla/MachineReady.tla SetSetting
func SetByWrite(s *Snapshot, who string, writes []PropWrite) []PropWrite {
	was, had := "", false
	if s != nil && s.Work != nil {
		was, had = s.Work.Prop(PropSetBy)
	}
	rec := map[string]string{}
	for _, e := range strings.Split(was, ";") {
		if name, rest, ok := strings.Cut(e, "="); ok && name != "" {
			rec[name] = rest
		}
	}
	changed := false
	at := time.Time{}
	if s != nil {
		at = s.Now
	}
	for _, w := range writes {
		if w.Table != Work || !WorkingCanBeNonWorking(w.Name) || !workingChecks[w.Name].BySet {
			continue
		}
		rec[w.Name] = strings.NewReplacer(";", "", "=", "", "@", "", "|", "").Replace(orDash(who)) + "@" + at.UTC().Format(time.RFC3339) + "|" + w.Value
		changed = true
	}
	if !changed {
		return nil
	}
	names := make([]string, 0, len(rec))
	for n := range rec {
		names = append(names, n)
	}
	slices.Sort(names)
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = n + "=" + rec[n]
	}
	return []PropWrite{{Table: Work, Name: PropSetBy, Value: strings.Join(parts, ";"), Was: was, WasAbsent: !had}}
}
