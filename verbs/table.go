package verbs

import (
	"context"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// role is who may run a verb (the role matrix, LAYER-6 section 3): cold
// verbs need no hook (they look, or they hook); worker verbs need the name
// hooked in as a worker or the coordinator; coordinator verbs need the
// coordinator. A cold verb that acts as a name checks it itself.
type role int

const (
	roleCold role = iota
	roleWorker
	roleCoordinator
)

func (r role) String() string {
	return [...]string{"cold (no hook needed)", "worker or coordinator", "coordinator"}[r]
}

// verb is one row of the verb table: its name, its usage, its flags (see
// flagSpec), its positional bounds (max -1: any), what it does, its
// prerequisite table (each line "<what refuses>: <the fix it names>"), and
// its handler. snap is the hooked check's snapshot for worker and
// coordinator verbs.
type verb struct {
	name, usage, flags, about, group string
	role                             role
	min, max                         int
	needs                            []string
	more                             []string // help lines after about: what a flag's words are
	run                              func(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error)
}

// groups is the help index's order.
var groups = []string{"start", "work", "coordinate", "operate"}

// table is every verb. Nothing else is a verb (LAYER-6 "v1 verbs dropped").
var table []*verb

func init() {
	table = append(append(append(append(table, startVerbs()...), workVerbs()...), coordinateVerbs()...), operateVerbs()...)
}

// lookup is the verb args name (one word, or two for a verb with
// subcommands: policy, stream) and the rest of args.
func lookup(args []string) (*verb, []string) {
	if len(args) > 1 {
		if v := find(args[0] + " " + args[1]); v != nil {
			return v, args[2:]
		}
	}
	if args[0] == "-h" || args[0] == "--help" {
		return find("help"), args[1:]
	}
	return find(args[0]), args[1:]
}

func find(name string) *verb {
	for _, v := range table {
		if v.name == name {
			return v
		}
	}
	return nil
}

// sub lists a verb's subcommands ("policy" -> "set", "activate", ...).
func sub(name string) []string {
	var out []string
	for _, v := range table {
		if rest, ok := strings.CutPrefix(v.name, name+" "); ok {
			out = append(out, rest)
		}
	}
	return out
}

func startVerbs() []*verb {
	return []*verb{
		{name: "next", group: "start", usage: "[--as <name>] [--json]", flags: "json", max: 0, run: runNext,
			about: "the one thing this name should do now, and its command; never refuses a missing prerequisite (the answer names it)",
			needs: []string{"the store or the bus cannot be read (exit 2): nova-sprint check"}},
		{name: "check", group: "start", usage: "[--integrity]", flags: "integrity", max: 0, run: runCheck,
			about: "the one doctor, one line each: nova-fleet check green, nova-tools against the pin (above it a WARN), the store at this build's schema, the bus answering; then the sprint's health, admission re-proved: each stream's repository reachable, its base at origin, its card branches creatable and its forge able to act; the policy active and answering; each waiting or ready card admitted as add would today; each working card on a live row; the open judgements by kind with their command, who runs the sprint, the width and why nothing deals, each landing's wait; any CHECK RED line exits non-zero; --integrity also rebuilds the history",
			needs: []string{"the fleet is not up: the nova-fleet command", "nova-tools below the pin: nova-fleet install --tag <pin> <machine>", "the store not set up: nova-sprint up --as <you>", "a red sprint line: the fix printed beside it"}},
		{name: "up", group: "start", usage: "--as <coordinator> [--dash tailnet|public=<machine>[:<port>]] [--services <a,b>] [--key <service>] [--run-on <host>] [--version <v>]", flags: "dash= services= key= run-on= version= continuation=", max: 0, run: runUp,
			about: "sets the sprint up, step by step, re-verifying from step 1 each time; hooks you in as coordinator once the store exists",
			needs: []string{"no --as: nova-sprint up --as <you>", "you are not on the bus: nova-bus hook --as <you> under your session's Monitor tool", "the first step that does not verify: its own fix"}},
		{name: "hook", group: "start", usage: "--as <name> --role worker|coordinator", flags: "role=", max: 0, run: runHook,
			about: "records your name and role in the sprint (a fresh session), subscribes you to its pushes, and prints the sprint's commit identity (its two git config lines) and your playbook",
			needs: []string{"not on the bus: nova-bus hook --as <name> under your session's Monitor tool", "already hooked: nova-sprint unhook --as <name>", "a second coordinator: the recorded one unhooks first", "a worker on a sprint with no commit identity: the coordinator runs nova-sprint identity"}},
		{name: "wake", group: "start", usage: "--as <you> (nova-bus hook --as <you> --exec 'nova-sprint wake --as <you>' runs it per message)", max: 0, run: runWake,
			about: "takes every card a DEAL on stdin deals you, then prints the message with each deal line replaced by its TAKEN receipt or its refusal; any other message unchanged"},
		{name: "unhook", group: "start", usage: "[<row>] --as <you>", max: 1, run: runUnhook,
			about: "clears a hook; your own without one, another row's as the coordinator",
			needs: []string{"not hooked: nothing to unhook", "another row, and you are not the coordinator: ask the coordinator"}},
		{name: "help", group: "start", usage: "[<verb> | playbook worker|coordinator]", max: 2, run: runHelp,
			about: "this index, one verb's table, or a role's playbook"},
		{name: "version", group: "start", usage: "", max: 0, run: runVersion, about: "this build's version"},
	}
}

func workVerbs() []*verb {
	return []*verb{
		{name: "take", group: "work", role: roleWorker, usage: "<card>@<gen> --as <row> [--epoch <e>] [--id <op>] [--json]", flags: "epoch= id= json", min: 1, max: 1, run: runTake,
			about: "acknowledges a card dealt to you (it is in your hand from the deal; this stops the nudges) and prints the brief, the repository and the finish or read command; a repeat prints the same and writes nothing",
			needs: []string{"not dealt to this row at this generation: nova-sprint next --as <row>", "withdrawn or reported already: nova-sprint next --as <row>", "the row not in nova-config, or not serving the tier: nova-config", "no commit identity: the coordinator runs nova-sprint identity"}},
		{name: "finish", group: "work", role: roleWorker, usage: "<card>@<gen> --head <sha> --as <row> [--epoch <e>] [--usage '<words>'] [--cost <usd>] | <card>@<gen> --failed --reason <why> --as <row>", flags: reportFlags + " failed", min: 1, max: 1, run: runReport,
			about: "reports a work card done at a head pushed to its branch, or failed with the reason", more: usageHelp,
			needs: []string{"not the holder: nova-sprint show <card>", "not a full sha", "not on the dealt branch at origin: git push origin <branch>", "the head is the base: do the work", "a commit since the base by another author or committer email: the two git config lines hook prints, then rewrite those commits"}},
		{name: "read", group: "work", role: roleWorker, usage: "<card>@<gen> yes|fix [--finding <text>] --as <row> [--epoch <e>] [--usage '<words>'] [--cost <usd>]", flags: reportFlags, min: 2, max: 2, run: runReport,
			about: "answers a read of someone else's head; --cost is what the read cost, in dollars, as finish takes it", more: usageHelp,
			needs: []string{"you wrote the head: wait for the bus", "fix with no finding: --finding \"<path>:<line>: <defect and repair>\""}},
		{name: "report", group: "work", role: roleWorker, usage: "<card>@<gen> --result ok|fix|fail|fault [--head <sha>] [--reason <why>] [--finding <text>] [--usage '<words>'] [--cost-microusd <n> --priced true|false | --cost <usd>] --as <row> [--epoch <e>] [--id <op>] [--json]", flags: reportFlags + " result=", min: 1, max: 1, run: runReport,
			about: "nova-worker's form of finish and read; fault is for a harness, bench or route failure", more: usageHelp,
			needs: []string{"as finish and read", "--cost mixed with --cost-microusd or --priced: give one form"}},
		{name: "release", group: "work", role: roleWorker, usage: "<card>@<gen> --reason <why> --as <you> [--epoch <e>] [--head <sha>] | --row <row> --all --reason <why> [--dry-run]", flags: "reason= epoch= head= row= all dry-run", max: 1, run: runRelease,
			about: "gives a card back to the queue, its pushed branch kept; another row's card, or a whole row's, is the coordinator's",
			needs: []string{"no reason: --reason <why>", "another holder's card, or --row, and you are not the coordinator: ask the coordinator"}},
		{name: "beat", group: "work", role: roleWorker, usage: "--load1 <loadavg> --cores <n> --slots-used <n> --slots-total <n> [--tiers <t,t>] --as <row>", flags: "load1= cores= slots-used= slots-total= tiers=", max: 0, run: runBeat,
			about: "nova-worker's heartbeat: the bench's 1-minute load average, its cores, its nova-reserve slots and the tiers its harness serves, for the board's load cell and the roster's width and tiers; writes nothing when it repeats the last beat",
			needs: []string{"not hooked in: nova-sprint hook --as <row> --role worker", "a flag missing or not a number: nova-sprint help beat"}},
		{name: "show", group: "work", role: roleWorker, usage: "[<card>|<stream>|<row>|run] [--stitch]", flags: "stitch", max: 1, run: runShow,
			about: "one record as JSON, the chooser (run), or a stream's stitch view",
			needs: []string{"an unknown name: nova-sprint show"}},
		{name: "queue", group: "work", role: roleWorker, usage: "[--stream <s>]", flags: "stream=", max: 0, run: runQueue,
			about: "the producers not yet final, in deal order"},
	}
}

// reportFlags are finish's, read's and report's shared flags.
const reportFlags = "head= reason= finding= cost= cost-microusd= priced= usage= epoch= id= json"

// usageHelp is what --usage and --cost are, for finish, read and report.
var usageHelp = []string{
	"--usage is the run's tokens as its harness counted them, one quoted string of words:",
	"  input=<n> cache_read=<n> cache_write=<n> output=<n> reasoning=<n> model=<provider/model>",
	"  (leave out a class the harness did not count; never write a guess as 0)",
	"the engine prices it, never the row: your row's harness decides the billing.",
	"  a bench (nova-worker, API keys): the tokens at the policy's prices(), exactly; no tokens or no price is recorded unpriced, never $0",
	"  a kin on a subscription seat (claude, codex, ...): free; pass nothing, or --usage to have your tokens counted",
	"--cost <usd> is the harness's own dollar figure, kept as charged= evidence beside the price, never as the price",
}
