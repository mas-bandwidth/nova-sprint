package verbs

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// FleetPlays is up's steps 2 and 3: nova-fleet's runner-packages and
// netwatch plays on every bench (nova-fleet up does not run them; its guide
// says to run them after it, and up is where that happens). A step is
// verified in ansible's check mode, when the play would change nothing on
// any bench (changed=0 in the PLAY RECAP, nothing unreachable or failed);
// it is fixed by running the play; and a play that fails is the refusal,
// in the play's own words. nova-sprint never re-implements a play: the
// one that runs is nova-fleet's, with its defaults (the s390x and VM
// example is opt-in in nova-tools and stays so).
type FleetPlays struct {
	Command CommandOutput
}

// setupPlay is one play step: the play's name and what it gives a bench.
type setupPlay struct{ name, gives string }

var setupPlays = map[int]setupPlay{
	StepPackages: {"runner-packages", "the packages a card's gates need"},
	StepNetwatch: {"netwatch", "a Linux bench that loses its network or the tailnet recovers by itself (a Mac is skipped)"},
}

func (f FleetPlays) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	if f.Command != nil {
		return f.Command(ctx, name, args...)
	}
	return toolOutput(ctx, name, args...)
}

// benches is every bench in nova-config's inventory, by name, in its order.
func (f FleetPlays) benches(ctx context.Context) ([]string, error) {
	machines, err := FleetServices{Command: f.Command}.Machines(ctx)
	if err != nil {
		return nil, cause("cannot inspect the machine inventory", err)
	}
	return WorkerBenches(machines), nil
}

// Verify runs the play in check mode on every bench: verified when it
// would change nothing; otherwise an act (the play), and when the play
// still reports a change, or the check-mode run itself fails, a refusal
// naming the play, the benches and what it said.
func (f FleetPlays) Verify(ctx context.Context, step int) (SetupCheck, error) {
	play, ok := setupPlays[step]
	if !ok {
		return SetupCheck{}, fmt.Errorf("step %d is not a fleet play", step)
	}
	benches, err := f.benches(ctx)
	if err != nil {
		return SetupCheck{}, err
	}
	if len(benches) == 0 {
		return SetupCheck{Verified: true, Line: fmt.Sprintf("UP %d/%d %s: no bench in the inventory, nothing to install: VERIFIED", step, SetupSteps, play.name)}, nil
	}
	limit := strings.Join(benches, ",")
	out, err := f.command(ctx, "nova-fleet", play.name, "--check", "--", "-l", limit)
	if err != nil {
		if ctx.Err() != nil {
			return SetupCheck{}, ctx.Err()
		}
		// The check-mode run failed (a bench unreachable, a task failed):
		// act, since the play itself may still succeed; when it does not,
		// its own error is the refusal (Act); when it does and the check
		// still fails, this one is.
		return SetupCheck{Action: true, Refusal: playRefusal(play.name, limit, err)}, nil
	}
	recap, err := playRecap(out)
	if err != nil {
		return SetupCheck{}, fmt.Errorf("nova-fleet %s --check on %s: %w", play.name, limit, err)
	}
	changed := playRecapChanged(recap)
	if len(changed) == 0 {
		return SetupCheck{Verified: true, Line: fmt.Sprintf("UP %d/%d %s: %s (nova-fleet %s, nothing to change) on %s: VERIFIED", step, SetupSteps, play.name, play.gives, play.name, limit)}, nil
	}
	return SetupCheck{Action: true, Refusal: &OperationError{
		Problem: edge.RedactN(fmt.Sprintf("nova-fleet %s --check still reports a change on %s after the play ran", play.name, strings.Join(changed, ",")), edge.FailureMax),
		Fix:     fmt.Sprintf("nova-fleet %s -- -l %s, read what it changes each time (a play that is not idempotent, or a bench it cannot reach), then nova-sprint up again", play.name, strings.Join(changed, ",")), Exit: 1}}, nil
}

// Act runs the play on every bench; its failure is the refusal, in the
// play's own words, with the fix.
func (f FleetPlays) Act(ctx context.Context, step int) error {
	play, ok := setupPlays[step]
	if !ok {
		return fmt.Errorf("step %d is not a fleet play", step)
	}
	benches, err := f.benches(ctx)
	if err != nil || len(benches) == 0 {
		return err
	}
	limit := strings.Join(benches, ",")
	if _, err = f.command(ctx, "nova-fleet", play.name, "--", "-l", limit); err != nil {
		return RefusalOr(err, playRefusal(play.name, limit, err))
	}
	return nil
}

// playRefusal is a failed play as a refusal: the play, the benches and
// nova-fleet's own words (the task that failed and on which machine), and
// the fix.
func playRefusal(play, limit string, err error) *OperationError {
	return &OperationError{Problem: edge.RedactN("nova-fleet "+play+" on "+limit+" failed: "+err.Error(), edge.FailureMax+400),
		Fix: "fix what it says for each machine it names (nova-fleet check; nova-fleet help " + play + "), or take the bench role off a machine that is gone, then nova-sprint up again", Exit: 1}
}

// playRecapHost is one host's line of ansible's PLAY RECAP.
type playRecapHost struct {
	host                         string
	changed, unreachable, failed int
}

var (
	ansibleColour = regexp.MustCompile("\x1b\\[[0-9;]*m")
	recapLine     = regexp.MustCompile(`^(\S+)\s*:\s*ok=(\d+)\s+changed=(\d+)\s+unreachable=(\d+)\s+failed=(\d+)`)
)

// playRecap reads ansible's PLAY RECAP from a play's output: one record per
// host. No recap at all is an error (the output is not a play's).
func playRecap(out []byte) ([]playRecapHost, error) {
	text := ansibleColour.ReplaceAllString(string(out), "")
	_, after, found := strings.Cut(text, "PLAY RECAP")
	if !found {
		return nil, fmt.Errorf("no PLAY RECAP in the output; first bytes %q", edge.FirstBytes(out))
	}
	var hosts []playRecapHost
	for _, line := range strings.Split(after, "\n") {
		m := recapLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		changed, _ := strconv.Atoi(m[3])
		unreachable, _ := strconv.Atoi(m[4])
		failed, _ := strconv.Atoi(m[5])
		hosts = append(hosts, playRecapHost{host: m[1], changed: changed, unreachable: unreachable, failed: failed})
	}
	return hosts, nil
}

// playRecapChanged is every host the play would change, could not reach or
// failed on, sorted.
func playRecapChanged(recap []playRecapHost) []string {
	var hosts []string
	for _, h := range recap {
		if h.changed > 0 || h.unreachable > 0 || h.failed > 0 {
			hosts = append(hosts, h.host)
		}
	}
	slices.Sort(hosts)
	return hosts
}
