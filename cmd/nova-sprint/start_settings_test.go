package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// start's settings gate (v1.2.17, start-refuses-a-non-working-setting; the owner,
// 2026-10-13: "OK these MUST be fixed. blocker priority."): epoch 20 started with
// sprint:git_identity empty and the lander could not land for 20 minutes; earlier clears
// left read cards off. start refuses, exit 1, nothing written, naming each setting a
// direct table write (a migration, a hand edit, an older binary) left non-working and the
// verb that fixes it; the same value chosen by set starts with one WARN line; a clean
// cleared epoch starts silently; a server run with --land=false is refused; run lands by
// default. The store is the twin on the test's own clock (tla/MachineReady.tla Start,
// StartRefusedSettings: StartOnlyWorking, DeliberateSetStarts).

// directProp writes a work table property as a migration would, by no set: the epoch's set
// record is not touched.
func (ta *testApp) directProp(name, value string) {
	ta.t.Helper()
	st, err := ta.a.store(common{redis: "mem:0", actor: "migration"})
	require.NoError(ta.t, err)
	res, err := st.Run(context.Background(), store.Step{Verb: "migrate", Load: []string{sprint.Work}, Plan: func(s *sprint.Snapshot) sprint.Plan {
		was, had := s.Work.Prop(name)
		return sprint.Plan{Props: []sprint.PropWrite{{Table: sprint.Work, Name: name, Value: value, Was: was, WasAbsent: !had}},
			Units: []sprint.Unit{{Key: "migrate", Moved: name + " " + value}}}
	}})
	require.NoError(ta.t, err)
	require.Empty(ta.t, res.Refused)
}

// machineRunning says the machine record is RUNNING.
func (ta *testApp) machineRunning() bool {
	ta.t.Helper()
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(ta.t, err)
	m, _, err := st.Machine(context.Background())
	require.NoError(ta.t, err)
	return m.Running()
}

// startLines is the START REFUSED and START WARN lines of a start's output.
func startLines(prefix, text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}

func TestStartRefusesEveryNonWorkingSettingNoSetWrote(t *testing.T) {
	t.Parallel()
	fresh := func(t *testing.T) *testApp {
		ta := newTestApp(t)
		ta.ok("init --readers reader-a,reader-b --members m1")
		ta.ok("clear --confirm sprint")
		return ta
	}

	t.Run("a clean cleared epoch starts with no line", func(t *testing.T) {
		t.Parallel()
		ta := fresh(t)
		ta.ok("add --stream s1 --count 2")
		code, out, errs := ta.do("start")
		require.Equal(t, 0, code, out+errs)
		assert.Empty(t, startLines("START REFUSED", errs))
		assert.Empty(t, startLines("START WARN", out))
		assert.True(t, ta.machineRunning())
	})

	for _, tc := range []struct {
		name    string
		break_  func(ta *testApp)
		setting []string
		verb    string
		// set is the set line that chooses the same value on purpose; "" when no set can
		// (a nova-config field, the server's flag, the readers)
		set string
	}{
		{name: "read cards off", break_: func(ta *testApp) { ta.directProp(sprint.PropReadCards, "off") },
			setting: []string{"read_cards"}, verb: "nova-sprint set --read-cards on", set: "set --read-cards off"},
		{name: "fleet and friends off", break_: func(ta *testApp) {
			ta.directProp(sprint.PropFleet, "off")
			ta.directProp(sprint.PropFriends, "off")
		}, setting: []string{"fleet", "friends"}, verb: "nova-sprint set --fleet on", set: "set --fleet off --friends off"},
		{name: "fleet tiers serve no needed tier", break_: func(ta *testApp) {
			ta.directProp(sprint.PropFriends, "off")
			ta.directProp(sprint.PropFleetTiers, "heavy")
		}, setting: []string{"fleet_tiers"}, verb: "nova-sprint set --fleet-tiers all"},
		{name: "reads above the readers up", break_: func(ta *testApp) { ta.directProp(sprint.PropReadsNeeded, "3") },
			setting: []string{"reads_needed"}, verb: "nova-sprint set --reads default", set: "set --reads 3"},
		{name: "no reader up", break_: func(ta *testApp) {
			ta.mu.Lock()
			ta.quiet = map[string]bool{"reader-a": true, "reader-b": true}
			ta.mu.Unlock()
			ta.later(2 * sprint.ReaderBeatBound)
		}, setting: []string{"readers"}, verb: "nova-sprint reader set <reader> --tiers flash"},
		{name: "git identity empty", break_: func(ta *testApp) { ta.m.SetGitIdentity("") },
			setting: []string{"git_identity"}, verb: "nova-config sprint set --git_identity"},
		{name: "max attempts out of range", break_: func(ta *testApp) { ta.m.SetMaxAttempts("0") },
			setting: []string{"max_attempts"}, verb: "nova-config sprint set --max_attempts 4"},
		{name: "the server lands nothing", break_: func(ta *testApp) {
			st, err := ta.a.store(common{redis: "mem:0", actor: "machine"})
			require.NoError(ta.t, err)
			require.NoError(ta.t, st.SetServer(context.Background(), store.ServerRecord{Actor: "machine", Land: false}))
		}, setting: []string{"land"}, verb: "nova-sprint run --listen"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ta := fresh(t)
			ta.ok("add --stream s1 --count 2")
			tc.break_(ta)
			code, out, errs := ta.do("start")
			require.Equal(t, 1, code, "refused: %s%s", out, errs)
			lines := startLines("START REFUSED", errs)
			require.Len(t, lines, len(tc.setting), "one line per finding, exactly the settings broken: %s", errs)
			for i, s := range tc.setting {
				assert.Contains(t, lines[i], "START REFUSED setting="+s+" ")
			}
			assert.Contains(t, lines[0], "run: "+tc.verb)
			assert.Contains(t, errs, "nothing was written")
			assert.False(t, ta.machineRunning(), "nothing was written: the machine is not RUNNING")
			assert.NotContains(t, out, "START OK")

			if tc.set == "" {
				return
			}
			// the same value chosen on purpose by set: starts, one WARN line each, never silent
			ta2 := fresh(t)
			ta2.ok("add --stream s1 --count 2")
			ta2.ok(tc.set)
			code, out, errs = ta2.do("start")
			require.Equal(t, 0, code, "a set of this epoch chose it: %s%s", out, errs)
			warns := startLines("START WARN", out)
			require.Len(t, warns, len(tc.setting), out)
			for i, s := range tc.setting {
				assert.Contains(t, warns[i], "START WARN setting="+s+" ")
				assert.Contains(t, warns[i], "set by coordinator at ")
			}
			assert.Empty(t, startLines("START REFUSED", errs))
			assert.True(t, ta2.machineRunning())
		})
	}

	t.Run("run and install land by default", func(t *testing.T) {
		t.Parallel()
		ta := newTestApp(t)
		_, out, errs := ta.do("run -h")
		help := out + errs
		i := strings.Index(help, "-land")
		require.GreaterOrEqual(t, i, 0, help)
		assert.Contains(t, help[i:], "On by default since v1.2.17")

		home := t.TempDir()
		env := map[string]string{"NOVA_SPRINT_REDIS": "127.0.0.1:6380", "NOVA_SPRINT_ACTOR": "seat-a", OwnerEnv: "owner-a"}
		a := newApp(func(k string) string { return env[k] })
		a.goos = "linux"
		a.home = func() (string, error) { return home, nil }
		a.executable = func() (string, error) { return "/opt/nova/bin/nova-sprint", nil }
		m := store.NewMem()
		a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) { return m, nil }
		a.seatLoad = func(string, string, string) error { return nil }
		install := func(args ...string) string {
			var o, e bytes.Buffer
			code := a.cmdInstall(append([]string{"server", "--listen", "127.0.0.1:6390"}, append(args, "--dry-run")...), &o, &e)
			require.Equal(t, 0, code, o.String()+e.String())
			return o.String()
		}
		assert.Contains(t, install(), "\"--land\"\n", "no flag: the unit lands")
		assert.Contains(t, install("--land=false"), `"--land=false"`)
	})
}
