package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/units"
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
		ta.ok("reader remove reader-m1") // the fixture's readers are the named ones; the clear's member reader goes
		// the base the coordinator records (the clear carries it); a sprint with none starts
		// with a START WARN for it (sprintBaseWorking), which these cases do not count
		ta.directProp(sprint.PropSprintBase, "sprint/s1")
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
		}, setting: []string{"fleet", "friends"}, verb: "nova-sprint set --fleet on"}, // chosen by set, nothing deals a ready card: check fails (rule 12) and start refuses
		{name: "fleet tiers serve no needed tier", break_: func(ta *testApp) {
			ta.directProp(sprint.PropFriends, "off")
			ta.directProp(sprint.PropFleetTiers, "heavy")
		}, setting: []string{"fleet_tiers"}, verb: "nova-sprint set --fleet-tiers all"},
		{name: "git identity malformed", break_: func(ta *testApp) { ta.m.SetGitIdentity("nobody") }, // an empty one reads the machine identity (sprint.Defaults)
			setting: []string{"git_identity"}, verb: "nova-config sprint set --git_identity"},
		{name: "max attempts out of range", break_: func(ta *testApp) { ta.m.SetMaxAttempts("0") },
			setting: []string{"max_attempts"}, verb: "nova-config sprint set --max_attempts 4"},
		{name: "the server lands nothing", break_: func(ta *testApp) {
			st, err := ta.a.store(common{redis: "mem:0", actor: "machine"})
			require.NoError(ta.t, err)
			require.NoError(ta.t, st.SetServer(context.Background(), store.ServerRecord{Actor: "machine", Land: false}))
		}, setting: []string{"land"}, verb: "nova-sprint install server --listen <address:port>, then restart its unit"},
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

	// the readers up are the fleet's live state, not a setting: no reader up for a needed
	// tier, and reads above the readers up, start with one WARN line each, never a refusal
	for _, tc := range []struct {
		name, setting, verb string
		break_              func(ta *testApp)
	}{
		{name: "reads above the readers up warns", setting: "reads_needed", verb: "nova-sprint set --reads default",
			break_: func(ta *testApp) { ta.directProp(sprint.PropReadsNeeded, "3") }},
		{name: "no reader up warns", setting: "readers", verb: "nova-sprint reader set <reader> --tiers flash",
			break_: func(ta *testApp) {
				ta.mu.Lock()
				ta.quiet = map[string]bool{"reader-a": true, "reader-b": true}
				ta.mu.Unlock()
				ta.later(2 * sprint.ReaderBeatBound)
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ta := fresh(t)
			ta.ok("add --stream s1 --count 2")
			tc.break_(ta)
			code, out, errs := ta.do("start")
			require.Equal(t, 0, code, "live state starts: %s%s", out, errs)
			assert.Empty(t, startLines("START REFUSED", errs))
			warns := startLines("START WARN", out)
			require.Len(t, warns, 1, out)
			assert.Contains(t, warns[0], "START WARN setting="+tc.setting+" ")
			assert.Contains(t, warns[0], "run: "+tc.verb)
			assert.True(t, ta.machineRunning())
		})
	}

	t.Run("run and install land by default", func(t *testing.T) {
		t.Parallel()
		ta := newTestApp(t)
		_, out, errs := ta.do("run -h")
		help := out + errs
		i := strings.Index(help, "-land")
		require.GreaterOrEqual(t, i, 0, help)
		assert.Contains(t, help[i:], "the server (run --listen) lands by default and a plain run does not")

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

// At most one lander (cold read 1 of PR 188, F1; tla/OneLander.tla): unset, --land is on
// for the server (run --listen) and off for a plain run; a land loop is refused, with one
// line naming the other, while another process's fresh record says it lands; a loop that
// does not land never writes over that record; units --check flags a server unit that
// names no --land and any other unit that lands.
func TestAtMostOneLander(t *testing.T) {
	t.Parallel()

	t.Run("land defaults on for the server and off for a plain run", func(t *testing.T) {
		t.Parallel()
		var unset landFlag
		assert.True(t, unset.lands(true), "run --listen lands")
		assert.False(t, unset.lands(false), "a plain run does not")
		on, off := landFlag{}, landFlag{}
		require.NoError(t, on.Set("true"))
		require.NoError(t, off.Set("false"))
		assert.True(t, on.lands(false), "an explicit --land lands a plain run")
		assert.False(t, off.lands(true), "--land=false: the server lands nothing")
	})

	t.Run("a land loop is refused beside another lander, and allowed with none", func(t *testing.T) {
		t.Parallel()
		ta := newTestApp(t)
		ta.ok("init --readers reader-a,reader-b --members m1")
		st, err := ta.a.store(common{redis: "mem:0", actor: "machine"})
		require.NoError(t, err)
		ctx := context.Background()

		why, err := ta.a.landerClash(ctx, st)
		require.NoError(t, err)
		assert.Empty(t, why, "no record: the loop lands")

		require.NoError(t, st.SetServer(ctx, store.ServerRecord{Actor: "machine", Land: true, PID: 4242, Host: "another-host.invalid"}))
		why, err = ta.a.landerClash(ctx, st)
		require.NoError(t, err)
		assert.Contains(t, why, "LAND REFUSED another lander is up: actor=machine pid=4242 host=another-host.invalid")

		// a loop that does not land never writes over the lander's record
		ta.a.serverLands = false
		ta.a.sayServer(ctx, st, time.Time{}, io.Discard)
		r, ok, err := st.Server(ctx)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, 4242, r.PID, "the lander's record stands")
		assert.True(t, r.Land)

		// the other lander's record says it lands nothing: no clash
		require.NoError(t, st.SetServer(ctx, store.ServerRecord{Actor: "machine", Land: false, PID: 4242, Host: "another-host.invalid"}))
		why, err = ta.a.landerClash(ctx, st)
		require.NoError(t, err)
		assert.Empty(t, why)

		// the record is this process's own: no clash
		host, _ := os.Hostname()
		require.NoError(t, st.SetServer(ctx, store.ServerRecord{Actor: "machine", Land: true, PID: os.Getpid(), Host: host}))
		why, err = ta.a.landerClash(ctx, st)
		require.NoError(t, err)
		assert.Empty(t, why)
	})

	t.Run("units --check flags a server unit with no --land and a second lander", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		server, ok := units.UnitKindOf("server")
		require.True(t, ok)
		table, ok := units.UnitKindOf("table")
		require.True(t, ok)
		write := func(k units.UnitKind, args ...string) {
			q := make([]string, len(args))
			for i, a := range args {
				q[i] = `"` + a + `"`
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, k.File("linux")), []byte("[Service]\nExecStart="+strings.Join(q, " ")+"\n"), 0o644))
		}
		check := func() map[string]units.UnitState {
			states, err := units.CheckUnits(dir, "linux", []units.UnitKind{server, table})
			require.NoError(t, err)
			out := map[string]units.UnitState{}
			for _, s := range states {
				out[s.Kind] = s
			}
			return out
		}
		write(server, "/opt/nova/bin/nova-sprint", "run", "--listen", "10.0.0.1:6390", "--redis", "10.0.0.1:6380")
		write(table, "/opt/nova/bin/nova-sprint", "where", "--watch", "--every", "1s", "--land")
		s := check()
		assert.Equal(t, units.UnitDifferent, s["server"].State)
		assert.Contains(t, s["server"].Why, "names neither --land nor --land=false")
		assert.Equal(t, units.UnitDifferent, s["table"].State)
		assert.Contains(t, s["table"].Why, "a second lander")

		write(server, "/opt/nova/bin/nova-sprint", "run", "--listen", "10.0.0.1:6390", "--redis", "10.0.0.1:6380", "--land", "--land-parallel", "2")
		write(table, "/opt/nova/bin/nova-sprint", "where", "--watch", "--every", "1s")
		s = check()
		assert.Equal(t, units.UnitInstalled, s["server"].State, s["server"].Why)
		assert.Equal(t, units.UnitInstalled, s["table"].State, s["table"].Why)
		write(server, "/opt/nova/bin/nova-sprint", "run", "--listen", "10.0.0.1:6390", "--land=false")
		assert.Equal(t, units.UnitInstalled, check()["server"].State, "an explicit --land=false is the unit's word (start refuses it)")
	})

	t.Run("a RUNNING machine whose server lands nothing raises one judgment", func(t *testing.T) {
		t.Parallel()
		ta := newTestApp(t)
		ta.ok("init --readers reader-a,reader-b --members m1")
		ta.ok("add --stream s1 --count 2")
		ta.ok("start")
		st, err := ta.a.store(common{redis: "mem:0", actor: "machine"})
		require.NoError(t, err)
		has := func() bool {
			for _, g := range ta.inboxGroups() {
				if g.Type == sprint.NServerLandsNothing {
					return true
				}
			}
			return false
		}
		assert.False(t, has(), "no server record: no judgment")
		require.NoError(t, st.SetServer(context.Background(), store.ServerRecord{Actor: "machine", Land: false, PID: 7, Host: "srv"}))
		assert.True(t, has(), "a fresh record that lands nothing while RUNNING")
		require.NoError(t, st.SetServer(context.Background(), store.ServerRecord{Actor: "machine", Land: true, PID: 7, Host: "srv"}))
		assert.False(t, has(), "the server lands: no judgment")
	})
}

// The land loop's pause between cycles ends at once when its context does (cold read 1 of
// PR 188): the sleep is the app's own, here one that never returns until released.
func TestTheLandPauseEndsWhenTheLoopDoes(t *testing.T) {
	t.Parallel()
	a := newApp(func(string) string { return "" })
	release := make(chan struct{})
	slept := make(chan time.Duration, 2)
	a.sleep = func(d time.Duration) { slept <- d; <-release }
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { a.landPause(ctx); close(done) }()
	assert.Equal(t, LandEvery, <-slept, "the pause sleeps LandEvery")
	select {
	case <-done:
		t.Fatal("the pause ended before its sleep or its context")
	default:
	}
	cancel()
	<-done // returns on the cancel, the sleep still blocked

	b := newApp(func(string) string { return "" })
	b.sleep = func(time.Duration) {}
	b.landPause(context.Background()) // a sleep that returns ends the pause
}
