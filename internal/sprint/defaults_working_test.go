package sprint

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// The settings gate's declarations (defaults_working.go): every setting the sprint holds is
// declared, with a predicate when some value of it leaves the machine non-working. The rows
// are every property set writes on the work table (read off a set that writes them all, so a
// property added to set later is a row here at once) and the outside settings the machine
// depends on (nova-config's git_identity and max_attempts, the readers, the server's landing).
// The Defaults table (clear-sets-every-working-default-in-the-new-epoch) names the same rows
// when it lands; this test then reads them from it.
func TestEveryDefaultsRowDeclaresWhetherItCanBeNonWorking(t *testing.T) {
	t.Parallel()
	w := tierWorld(t)
	p := Set(w.s, SetReq{Who: w.s.Coordinator,
		ReadTier: ReadTierDefault, DealtMax: ReadTierDefault, GoLanes: ReadTierDefault, FriendIdle: ReadTierDefault,
		LandingStallBound: ReadTierDefault, FriendStallAfter: ReadTierDefault, FriendStallStep: ReadTierDefault,
		DriftCommits: ReadTierDefault, DriftHours: ReadTierDefault, Fleet: SwitchOn, Friends: SwitchOn,
		FleetTiers: TiersAll, FriendsTiers: TiersAll, ReadCards: SwitchOff, Reads: ReadTierDefault,
		UnservedTierFallsTo: ReadTierDefault, ReworkPriority: ReworkKeep,
		AlarmReview: AlarmOff, AlarmMerging: AlarmOff, AlarmFleet: AlarmOff, AlarmReady: AlarmOff})
	require.Empty(t, p.Refused, "a set of every sprint setting")
	// every work-table setting the clear writes or carries (sprint.Defaults), and the
	// settings outside the work table the gate reads
	rows := []string{SettingGitIdentity, SettingMaxAttempts, SettingReaders, SettingLand}
	for _, d := range Defaults {
		if d.Home == HomeProp || d.Home == HomeCarry {
			rows = append(rows, d.Name)
		}
	}
	for _, pw := range p.Props {
		if pw.Name != PropSetBy {
			require.Contains(t, rows, pw.Name, "a property set writes has a Defaults row")
		}
	}
	require.GreaterOrEqual(t, len(rows), 20, "every row read: %v", rows)
	for _, name := range rows {
		_, ok := workingChecks[name]
		assert.True(t, ok, "setting %s has no declaration in workingChecks: say whether it can be non-working (a predicate) or not (nil)", name)
	}
	for _, name := range []string{PropReadCards, PropFleet, PropFriends, PropFleetTiers, PropFriendsTiers, PropReadsNeeded, SettingReaders, SettingGitIdentity, SettingMaxAttempts, SettingLand} {
		assert.True(t, WorkingCanBeNonWorking(name), "%s has a predicate", name)
	}
	// the set wrote read_cards off: its record names it, by the coordinator at the clock
	var rec *PropWrite
	for i := range p.Props {
		if p.Props[i].Name == PropSetBy {
			rec = &p.Props[i]
		}
	}
	require.NotNil(t, rec, "a set of a gated setting writes the epoch's set record")
	for _, pw := range p.Props {
		w.s.Work.SetProp(pw.Name, pw.Value)
	}
	by := SetByRecord(w.s)
	assert.Contains(t, by, PropReadCards)
	// a value written after the set by anything but set is no set's choice
	w.s.Work.SetProp(PropReadCards, ReadCardsOnWord)
	assert.NotContains(t, SetByRecord(w.s), PropReadCards)
	assert.NotContains(t, by, PropDealtMax, "a setting with no predicate is not recorded")
}

// Working finds each predicate's setting, exactly it, with its verb; a value a set chose
// carries who chose it.
func TestWorkingNamesEachNonWorkingSetting(t *testing.T) {
	t.Parallel()
	clean := tierWorld(t)
	clean.s.GitIdentity = "nova-sprint <nova-sprint@example.invalid>"
	base := SettingsWorking(clean.s, WorkingBars{})
	require.Empty(t, base, "the world as built works: %+v", base)

	for _, tc := range []struct {
		name  string
		bars  WorkingBars
		apply func(s *Snapshot)
		want  []string
	}{
		{"read cards off", WorkingBars{}, func(s *Snapshot) { s.Work.SetProp(PropReadCards, SwitchOff) }, []string{PropReadCards}},
		{"fleet and friends off", WorkingBars{}, func(s *Snapshot) {
			s.Work.SetProp(PropFleet, SwitchOff)
			s.Work.SetProp(PropFriends, SwitchOff)
		}, []string{PropFleet, PropFriends}},
		{"fleet tiers leave a needed tier out", WorkingBars{}, func(s *Snapshot) {
			s.Work.SetProp(PropFriends, SwitchOff)
			s.Work.SetProp(PropFleetTiers, "frontier")
		}, []string{PropFleetTiers}},
		{"git identity empty", WorkingBars{}, func(s *Snapshot) { s.GitIdentity = "" }, []string{SettingGitIdentity}},
		{"no reader up (live state)", WorkingBars{}, func(s *Snapshot) {
			s.ReaderStates = map[string]string{"reader-a": ReaderDown, "reader-b": ReaderDown, "reader-c": ReaderDown}
		}, []string{SettingReaders}},
		{"reads above the readers up (live state)", WorkingBars{}, func(s *Snapshot) {
			s.ReaderStates = map[string]string{"reader-a": ReaderUp, "reader-b": ReaderDown, "reader-c": ReaderDown}
			s.Work.SetProp(PropReadsNeeded, "3")
		}, []string{PropReadsNeeded}},
		{"max attempts out of range", WorkingBars{}, func(s *Snapshot) { s.MaxAttempts = "0" }, []string{SettingMaxAttempts}},
		{"server lands nothing", WorkingBars{ServerUp: true}, func(*Snapshot) {}, []string{SettingLand}},
	} {
		w := tierWorld(t)
		w.s.GitIdentity = clean.s.GitIdentity
		tc.apply(w.s)
		var got []string
		for _, f := range SettingsWorking(w.s, tc.bars) {
			got = append(got, f.Setting)
			assert.NotEmpty(t, f.Verb, "%s: %s names its verb", tc.name, f.Setting)
			assert.Empty(t, f.SetBy, "%s: no set chose it", tc.name)
			assert.Equal(t, f.Setting == SettingReaders || f.Setting == PropReadsNeeded, f.Live, "%s: %s is live state (a WARN) only for the readers", tc.name, f.Setting)
		}
		slices.Sort(got)
		want := slices.Clone(tc.want)
		slices.Sort(want)
		assert.Equal(t, want, got, tc.name)
	}

	// the same read_cards off chosen by a set of the epoch: found, with who chose it
	w := tierWorld(t)
	w.s.GitIdentity = clean.s.GitIdentity
	p := Set(w.s, SetReq{Who: w.s.Coordinator, ReadCards: SwitchOff})
	require.Empty(t, p.Refused)
	for _, pw := range p.Props {
		w.s.Work.SetProp(pw.Name, pw.Value)
	}
	fs := SettingsWorking(w.s, WorkingBars{})
	require.Len(t, fs, 1)
	assert.Equal(t, PropReadCards, fs[0].Setting)
	assert.Contains(t, fs[0].SetBy, w.s.Coordinator)
}

// The nova-config remedies the gate, the doctor and the lander print name only flags the
// release line's nova-config takes: sprint set's flags are the sprint kind's fields (and
// --actor), apply's are --kind with a kind's name and --actor. Read 2 of PR 188 ran an older
// form against a nova-config whose sprint kind had no such field; this holds the printed
// words to the kind the CLI builds its flags from.
func TestTheNovaConfigRemediesNameRealFlags(t *testing.T) {
	t.Parallel()
	fields := map[string]bool{}
	kinds := map[string]bool{}
	for _, k := range config.Kinds {
		kinds[k.Name] = true
		if k.Name == config.KindSprint {
			for _, f := range k.Fields {
				fields[f.Name] = true
			}
		}
	}
	require.NotEmpty(t, fields, "the sprint kind has fields")
	for _, remedy := range []string{GitIdentityRemedy, MaxAttemptsRemedy} {
		steps := strings.Split(remedy, "&&")
		require.Len(t, steps, 2, remedy)
		set, apply := remedyWords(steps[0]), remedyWords(steps[1])
		require.Equal(t, []string{"nova-config", "sprint", "set"}, set[:3], remedy)
		for i := 3; i < len(set); i += 2 {
			flag := strings.TrimPrefix(set[i], "--")
			require.True(t, strings.HasPrefix(set[i], "--") && i+1 < len(set), "%s: %q is a flag with a value", remedy, set[i])
			assert.True(t, flag == "actor" || fields[flag], "%s: --%s is a sprint kind field or --actor", remedy, flag)
		}
		require.Equal(t, []string{"nova-config", "apply"}, apply[:2], remedy)
		for i := 2; i < len(apply); i += 2 {
			require.True(t, i+1 < len(apply), remedy)
			switch apply[i] {
			case "--kind":
				assert.True(t, kinds[apply[i+1]], "%s: --kind %s names a kind", remedy, apply[i+1])
			case "--actor":
			default:
				t.Errorf("%s: apply takes --kind and --actor here, not %s", remedy, apply[i])
			}
		}
	}
	assert.Contains(t, GitIdentityRemedy, "'"+config.DefaultGitIdentity+"'")
}

// remedyWords is a shell command's words, '...' quoting one word.
func remedyWords(s string) []string {
	var out []string
	var cur strings.Builder
	quoted, in := false, false
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r == '\'':
			quoted, in = !quoted, true
		case r == ' ' && !quoted:
			if in {
				out = append(out, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	if in {
		out = append(out, cur.String())
	}
	return out
}
