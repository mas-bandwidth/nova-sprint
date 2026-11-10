package verbs

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

// playFleet is nova-config's machine list and nova-fleet for the two play
// steps: each bench's changed count in check mode, every nova-fleet argv
// run, and a play that fails.
type playFleet struct {
	changed map[string]int // bench -> changed= in the check-mode recap; a play run resets it to 0
	benches []string
	calls   [][]string
	fail    error
}

func (f *playFleet) command(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "nova-config" {
		items := []string{}
		for _, b := range f.benches {
			items = append(items, `{"kind":"machine","fields":{"name":"`+b+`","roles":"bench","cores":8}}`)
		}
		items = append(items, `{"kind":"machine","fields":{"name":"studio","roles":"services","cores":8}}`)
		return []byte(`{"result":{"verb":"machine list","status":"ok","exit":0},"facts":{"kind":"machine","rows":` + strconv.Itoa(len(items)) + `},"items":[` + strings.Join(items, ",") + `]}`), nil
	}
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.fail != nil {
		return nil, f.fail
	}
	check := len(args) > 1 && args[1] == "--check"
	var out strings.Builder
	out.WriteString("\nPLAY [the play] *****\n\nPLAY RECAP *********************************************************************\n")
	for _, b := range f.benches {
		if !check {
			f.changed[b] = 0
		}
		out.WriteString(b + "                      : ok=3    changed=" + strconv.Itoa(f.changed[b]) + "    unreachable=0    failed=0    skipped=1    rescued=0    ignored=0\n")
	}
	return []byte(out.String()), nil
}

// The two fleet plays are steps of up, in the docs' order (runner-packages
// first, so a bench can run gates as soon as it is dealt to; then netwatch),
// between the fleet check and the store. Each is verified in check mode
// (changed=0 on every bench), fixed by running nova-fleet's play, never a
// copy of it, and verified again.
func TestRuntimeSetupRunsTheFleetPlaysAsSteps(t *testing.T) {
	t.Parallel()
	if StepPackages != StepTools+1 || StepNetwatch != StepPackages+1 || StepStore != StepNetwatch+1 || SetupSteps != 10 {
		t.Fatal(StepTools, StepPackages, StepNetwatch, StepStore, SetupSteps)
	}
	ctx := context.Background()
	f := &playFleet{benches: []string{"amd-1", "amd-2"}, changed: map[string]int{"amd-1": 2}}
	r := RuntimeSetup{Plays: FleetPlays{Command: f.command}}
	for _, step := range []int{StepPackages, StepNetwatch} {
		play := []string{"", "", "runner-packages", "netwatch"}[step]
		f.changed = map[string]int{"amd-1": 2}
		f.calls = nil
		check, err := r.Verify(ctx, step, SetupInput{})
		if err != nil || check.Verified || !check.Action || check.Refusal == nil || !strings.Contains(check.Refusal.Problem, play) {
			t.Fatal(step, check, err)
		}
		if err = r.Act(ctx, step, SetupInput{}); err != nil {
			t.Fatal(step, err)
		}
		check, err = r.Verify(ctx, step, SetupInput{})
		if err != nil || !check.Verified || check.Action || !strings.HasPrefix(check.Line, "UP "+strconv.Itoa(step)+"/10 "+play+": ") || !strings.HasSuffix(check.Line, ": VERIFIED") || !strings.Contains(check.Line, "amd-1,amd-2") {
			t.Fatal(step, check, err)
		}
		want := []string{
			"nova-fleet " + play + " --check -- -l amd-1,amd-2",
			"nova-fleet " + play + " -- -l amd-1,amd-2",
			"nova-fleet " + play + " --check -- -l amd-1,amd-2",
		}
		got := []string{}
		for _, c := range f.calls {
			got = append(got, strings.Join(c, " "))
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatal(got)
		}
	}
}

// A play that fails refuses with the play's own error and names the fix;
// a check-mode run that fails is an act first (the play itself may still
// succeed), then that refusal.
func TestRuntimeSetupFleetPlayFailureIsItsOwnRefusal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := &playFleet{benches: []string{"amd-1"}, changed: map[string]int{}, fail: errors.New("nova-fleet netwatch: FAILED ansible-playbook exit=2; amd-1 unreachable")}
	r := RuntimeSetup{Plays: FleetPlays{Command: f.command}}
	check, err := r.Verify(ctx, StepNetwatch, SetupInput{})
	if err != nil || check.Verified || !check.Action || check.Refusal == nil || !strings.Contains(check.Refusal.Problem, "amd-1 unreachable") || !strings.Contains(check.Refusal.Fix, "nova-sprint up again") {
		t.Fatal(check, err)
	}
	err = r.Act(ctx, StepNetwatch, SetupInput{})
	var op *OperationError
	if !errors.As(err, &op) || !strings.Contains(op.Problem, "nova-fleet netwatch on amd-1 failed") || !strings.Contains(op.Problem, "amd-1 unreachable") {
		t.Fatal(err)
	}
	// a check-mode run that prints no recap is a broken tool, not a step to act on
	g := &playFleet{benches: []string{"amd-1"}, changed: map[string]int{}}
	r = RuntimeSetup{Plays: FleetPlays{Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		out, err := g.command(ctx, name, args...)
		if name == "nova-fleet" {
			return []byte("nothing\n"), err
		}
		return out, err
	}}}
	if _, err = r.Verify(ctx, StepPackages, SetupInput{}); err == nil || !strings.Contains(err.Error(), "PLAY RECAP") {
		t.Fatal(err)
	}
	// no bench in the inventory: nothing to run, the step is verified and says so
	r = RuntimeSetup{Plays: FleetPlays{Command: (&playFleet{changed: map[string]int{}}).command}}
	check, err = r.Verify(ctx, StepPackages, SetupInput{})
	if err != nil || !check.Verified || !strings.Contains(check.Line, "no bench") {
		t.Fatal(check, err)
	}
}

// tla/Setup.tla's world[k] for k in PlaySteps, bound to FleetPlays over every
// state of a two-bench fleet (each bench clean, with something to change, or
// unreachable, which nova-fleet reports as a failed run beside its recap):
// world[k] is every bench clean, and Verify is verified exactly then; any
// other state is the model's repair (an act, then its refusal), never a
// verified step and never another error.
func TestFleetPlayStepsMatchTheSetupModel(t *testing.T) {
	t.Parallel()
	benches := []string{"amd-1", "amd-2"}
	for _, step := range []int{StepPackages, StepNetwatch} {
		for mask := 0; mask < 9; mask++ {
			states := []int{mask % 3, mask / 3} // 0 clean, 1 a change, 2 unreachable
			world, unreachable := true, false
			var recap strings.Builder
			recap.WriteString("PLAY RECAP *****\n")
			for i, b := range benches {
				changed, gone := 0, 0
				switch states[i] {
				case 1:
					changed = 1
				case 2:
					gone, unreachable = 1, true
				}
				world = world && states[i] == 0
				recap.WriteString(b + " : ok=3 changed=" + strconv.Itoa(changed) + " unreachable=" + strconv.Itoa(gone) + " failed=0 skipped=0 rescued=0 ignored=0\n")
			}
			command := func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if name == "nova-config" {
					return (&playFleet{benches: benches, changed: map[string]int{}}).command(ctx, name, args...)
				}
				if unreachable {
					return []byte(recap.String()), errors.New("nova-fleet: FAILED ansible-playbook exit=4")
				}
				return []byte(recap.String()), nil
			}
			check, err := FleetPlays{Command: command}.Verify(context.Background(), step)
			if err != nil || check.Verified != world || check.Action == world || (check.Refusal == nil) != world {
				t.Fatal(step, states, world, check, err)
			}
		}
	}
}

// The recap parser: every host's changed, unreachable and failed counts,
// colour codes stripped; a line that is not a host's is left alone.
func TestPlayRecapParsesAnsible(t *testing.T) {
	t.Parallel()
	out := "PLAY RECAP *****\n\x1b[0;32mamd-1\x1b[0m                      : \x1b[0;32mok=5   \x1b[0m changed=0    unreachable=0    failed=0    skipped=2    rescued=0    ignored=0\n" +
		"titan : ok=5 changed=1 unreachable=0 failed=0 skipped=0 rescued=0 ignored=0\n" +
		"space : ok=0 changed=0 unreachable=1 failed=0 skipped=0 rescued=0 ignored=0\n"
	recap, err := playRecap([]byte(out))
	if err != nil || len(recap) != 3 {
		t.Fatal(recap, err)
	}
	if got := strings.Join(playRecapChanged(recap), ","); got != "space,titan" {
		t.Fatal(got)
	}
	if _, err = playRecap([]byte("PLAY [x]\nok: [amd-1]\n")); err == nil {
		t.Fatal("no recap accepted")
	}
}
