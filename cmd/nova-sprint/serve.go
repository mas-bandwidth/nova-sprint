package main

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
)

// The sprint's server is single threaded and takes pipelined batches like
// redis, but it is its own client/server, not redis as the transport: a
// redis-like thing the distributed things talk to.
// The run loop is the one writer of the
// sprint; with --listen it also takes the workers' verbs. A worker (a member, a
// reader) sends a batch: its verbs, each the argument list it would give
// nova-sprint, in order. serve runs each through the verb's own code, in this
// process, beside the store, one at a time and never during a tick, and answers
// with each verb's exit code and what it printed. A worker's machine reads and
// writes nothing of the store itself, so no verb of it can lose to another
// writer, and a verb costs one exchange from anywhere.
//
// The server is a state machine with one step, serve: a batch in, its results
// out, the sprint moved. The listener is a shell around it (ServeHTTP), and a
// test steps it with batches and ticks in one process, with no connection.
//
// The need is distance: a worker far from the store loses a write's fence for
// long enough to give up, and a finished card takes minutes to be reported,
// against seconds beside the store.

// workerVerb is the worker a verb of a batch acts as and how many words its
// verb is, or why the server does not run it. A worker sends its own verbs only:
// take, finish, read or queue, then --as and its name; or fleet beat, its name,
// an optional --stop-returns and a whole number of at least 0, an optional --no-room and
// why it starts no card, an optional --draining, an optional --live and a
// live set, --load and a number, in any order with --load anywhere, and nothing more; or friend beat, its name, and its report's flags each with its value (friendBeatReport); or friend cards, its name, and --json at most. The name is one worker, never a list.
// No later word, wherever it stands, is a flag named as, redis or actor: the
// server gives the store and the actor (serve puts them before the worker's
// words, where nothing the worker sent can take them as a value or end the
// flags ahead of them). The check is on the words as sent, not on how the verb
// would parse them: a word that only looks like one of those flags (a report's
// text) is refused too, which is the safe side. The epoch a take, a finish and
// a read must name is checked where the verb has parsed it (runStep, a.serving):
// what counts is the epoch the verb runs at, not a word that looks like one. A beat's load is the worker's own measure: the server cannot measure
// another machine, and would record its own. A queue's --packets and --have (the packets
// the worker wants) are held to their shapes here, before the verb runs: a count, and card
// ids (packetsWanted).

// fleetBeatWords reads `fleet beat <member> [--stop-returns <n>] [--no-room <why>]
// [--draining] [--live <set>] --load <percent>`, the words nova-tools pkg/member sends
// (member.Beat, drainArgs, noRoomArgs): the optional words may stand in any order and
// --load anywhere among them. --load is required, because the server cannot measure the
// worker's machine. A word the list does not name is refused naming it; why is "" when
// the words are a beat.
func fleetBeatWords(rest []string) (as, load, stop, live, noRoom string, draining bool, why string) {
	// the forwarding CLI puts its caller's `--actor <name>` before the member (forward.go,
	// ask): the server's own actor is the member, so it is read and dropped here, never
	// mistaken for the member.
	if len(rest) >= 2 && rest[0] == "--actor" {
		rest = rest[2:]
	}
	if len(rest) < 1 {
		return "", "", "", "", "", false, "a beat names its member first"
	}
	as, words := rest[0], rest[1:]
	loadSet, stopSet := false, false
	for i := 0; i < len(words); i++ {
		w := words[i]
		if w == "--draining" || w == "--dry-run" {
			draining = draining || w == "--draining"
			continue
		}
		var value string
		switch w {
		case "--load", "--stop-returns", "--live", "--no-room":
			if i+1 >= len(words) {
				return "", "", "", "", "", false, "a beat's " + w + " wants its value"
			}
			value = words[i+1]
			i++
		default:
			return "", "", "", "", "", false, "a beat takes no word " + oneline.Escape(w) + ": its words are `fleet beat <member> [--stop-returns <n>] [--no-room <why>] [--draining] [--live <set>] --load <percent>` and nothing more"
		}
		switch w {
		case "--load":
			if loadSet {
				return "", "", "", "", "", false, "a beat's --load is given once"
			}
			load, loadSet = value, true
		case "--stop-returns":
			if stopSet {
				return "", "", "", "", "", false, "a beat's --stop-returns is given once"
			}
			stop, stopSet = value, true
		case "--live":
			if value == "" {
				return "", "", "", "", "", false, "a beat's --live names a set"
			}
			live = value
		case "--no-room":
			noRoom = value
		}
	}
	if !loadSet {
		return "", "", "", "", "", false, "a beat names its --load and nothing more measures the worker's machine"
	}
	return as, load, stop, live, noRoom, draining, ""
}

func workerVerb(argv []string) (as string, words int, why string) {
	if len(argv) >= 2 && argv[0] == "fleet" && argv[1] == "beat" {
		as, load, stop, live, _, _, why := fleetBeatWords(argv[2:])
		if why != "" {
			return "", 0, why
		}
		if !sprint.ValidID(as) {
			return "", 0, "a beat's member is a name (letters, digits, _ and -), found " + oneline.Escape(as)
		}
		if live != "" {
			if _, err := sprint.ParseLive(live); err != nil {
				return "", 0, "a beat's " + err.Error()
			}
		}
		if _, err := strconv.ParseFloat(load, 64); err != nil {
			return "", 0, "a beat's --load is a number, found " + load
		}
		if stop != "" {
			n, err := strconv.Atoi(stop)
			if err != nil || n < 0 {
				return "", 0, "a beat's --stop-returns is a whole number of at least 0, found " + stop
			}
		}
		return as, 2, ""
	}
	if len(argv) >= 2 && argv[0] == "friend" && argv[1] == "beat" {
		if len(argv) < 3 || !sprint.ValidID(argv[2]) {
			return "", 0, "a friend's beat sent to the server is `friend beat <friend>` and its report's flags (" + friendBeatServed + ") and nothing more"
		}
		if why := friendBeatReport(argv[3:]); why != "" {
			return "", 0, why
		}
		return argv[2], 2, ""
	}
	if len(argv) >= 2 && argv[0] == "friend" && argv[1] == "cards" {
		// a friend's daemon reads the cards held on her row, each with its brief, to write
		// them into her inbox (friendcards_verb.go): her name, --json at most, nothing more
		if len(argv) < 3 || len(argv) > 4 || !sprint.ValidID(argv[2]) || (len(argv) == 4 && argv[3] != "--json") {
			return "", 0, "a friend's cards sent to the server are `friend cards <friend> [--json]` and nothing more"
		}
		return argv[2], 2, ""
	}
	if len(argv) >= 2 && argv[0] == "budget" && (argv[1] == "take" || argv[1] == "renew" || argv[1] == "release") {
		// a model request's take of the global rate budget, or its release (rate_budget.go):
		// the model, the worker and the request's holder, --json at most, nothing more
		rest := argv[2:]
		if n := len(rest); (n != 5 && n != 6) || !strings.Contains(rest[0], "/") || rest[1] != "--as" || rest[3] != "--holder" || rest[4] == "" || (n == 6 && rest[5] != "--json") {
			return "", 0, "a budget's verb sent to the server is `budget " + argv[1] + " <provider/model> --as <worker> --holder <id> [--json]` and nothing more"
		}
		if name, ok := sprint.FriendOfRow(rest[2]); !sprint.ValidID(rest[2]) && (!ok || !sprint.ValidID(name)) {
			return "", 0, "a budget's verb names one worker (letters, digits, _ and -, or friend.<name>), found " + rest[2]
		}
		return rest[2], 2, ""
	}
	if len(argv) >= 2 && argv[0] == "lane" && (argv[1] == "take" || argv[1] == "give") {
		// a lane's take or give (lane.go; docs/SPEC-SPRINT.md section 18): its kind, the
		// machine and the worker, and nothing more; the server never waits (--wait asks again
		// from the worker's side)
		rest := argv[2:]
		if len(rest) != 5 || !slices.Contains(sprint.LaneKinds, rest[0]) || rest[1] != "--machine" || !sprint.ValidID(rest[2]) || rest[3] != "--as" || !sprint.ValidLaneWho(rest[4]) {
			return "", 0, "a lane's verb sent to the server is `lane " + argv[1] + " <kind> --machine <m> --as <worker>` and nothing more, the kind one of " + strings.Join(sprint.LaneKinds, ", ")
		}
		return rest[4], 2, ""
	}
	// stop-return is a worker's too: a lane the machine's stop cancelled hands its card back
	// through the server (a friend's daemon has no store of its own to send it to; 2026-10-10
	// 21:55Z on a one-shot friend host, every stop-return refused here)
	if len(argv) == 0 || !slices.Contains([]string{"take", "finish", "progress", "read", "queue", "stop-return", "remind"}, argv[0]) {
		return "", 0, "the server runs the workers' verbs only: take, finish, progress, read, queue, stop-return, remind, fleet beat, friend beat, friend cards, lane take, lane give, budget take, budget renew, budget release"
	}
	verb, rest := argv[0], argv[1:]
	if len(rest) < 2 || rest[0] != "--as" {
		return "", 0, "it does not begin `" + verb + " --as <worker>`: a worker's verb names its worker first"
	}
	as, rest = rest[1], rest[2:]
	// a friend's row is a worker (friend.<name>: her session's take, progress and finish)
	if name, ok := sprint.FriendOfRow(as); !sprint.ValidID(as) && (!ok || !sprint.ValidID(name)) {
		return "", 0, "a worker's verb names one worker (letters, digits, _ and -, or friend.<name>), found " + as
	}
	for _, w := range rest {
		if !strings.HasPrefix(w, "-") {
			continue
		}
		switch name, _, _ := strings.Cut(strings.TrimLeft(w, "-"), "="); name {
		case "as", "redis", "actor":
			return "", 0, "--" + name + " is not a worker's to give the server"
		}
	}
	if verb == "queue" {
		packets, okP := flagWord(rest, "packets")
		have, okH := flagWord(rest, "have")
		if !okP || !okH {
			return "", 0, "a queue's --packets and --have are each given at most once, each with its value"
		}
		if _, err := packetsWanted(packets, have); err != nil {
			return "", 0, err.Error()
		}
	}
	return as, 1, ""
}

// flagWord is the value the words give the flag name (--name v, --name=v, one dash or
// two), "" when they give none; ok is false when they give it more than once or with no
// value.
func flagWord(words []string, name string) (value string, ok bool) {
	seen := false
	for i := 0; i < len(words); i++ {
		w := words[i]
		if !strings.HasPrefix(w, "-") {
			continue
		}
		n, v, eq := strings.Cut(strings.TrimLeft(w, "-"), "=")
		if n != name {
			continue
		}
		if seen {
			return "", false
		}
		seen = true
		if !eq {
			if i+1 >= len(words) {
				return "", false
			}
			i++
			v = words[i]
		}
		value = v
	}
	return value, true
}

// notServed are the verbs the server runs for nobody: itself (run, tick), the ones that
// work for seconds or minutes outside the store (land's git, the driver), fleet sync and
// friend sync, which read the config store with their caller's own credentials, friend
// clean, which works on the directories of the machine it runs on, and dashboard, which
// serves a page until it is interrupted and reads through the server, doctor, which reads
// the config store with its caller's credentials, and seat install and seat uninstall,
// which record the seat on, and remove the retired push loop's unit from, the machine they
// are typed on.
var notServed = []string{"run", "tick", "land", "play", "fleet sync", "friend sync", "friend clean", "dashboard", "doctor", "answer", "seat install", "seat uninstall", "selftest land", "server switch"}

// serveCtx is the server's one step: the batch's verbs run in order, each through
// the verb's own code with its worker as the actor, and each answered. The
// server's own words (the store, the actor) go between the verb and what the
// worker sent. A verb the server does not run (workerVerb) is answered as a
// usage refusal, exit 2, and the batch goes on: every verb has its own answer.
// Local batches run any verb the server runs (verbArgs.unserved), while fleet
// batches run a worker's verbs only. It is for a caller that can go away (ctx, the request's). A friend's
// beat runs on the beat lane and a read on the read lane, neither on the line
// (servelanes.go); every other verb runs on the line (a.serial), one batch's verbs and
// one tick at a time: the line is taken at the batch's first such verb, after the
// request is read whole, held to the batch's end, and released before any answer is
// written, so a slow worker never holds the tick. A batch waits for the line only while
// its caller waits for the answer: a caller gone (ctx done) before the line is taken has
// its verbs from there on not run, each answered exit 2 saying so, and nothing changed.
func (a *app) serveCtx(ctx context.Context, req sprintwire.Request, local bool) sprintwire.Response {
	out := sprintwire.Response{Results: make([]sprintwire.Result, len(req.Verbs))}
	lanes := a.lanesFor(ctx)
	begun := a.now()
	var took time.Time
	held, beats, reads, onLine, gone := false, 0, 0, 0, 0
	var first []string
	defer func() {
		var wait, hold time.Duration
		if held {
			a.serving = false
			hold = a.now().Sub(took)
			wait = took.Sub(begun)
			a.serial.Unlock()
		}
		a.tally(beats, reads, onLine, gone, wait, hold, first)
	}()
	for i, argv := range req.Verbs {
		var args []string
		actorNotice := ""
		actorResolved := false
		serving, lane := false, ""
		as, words, why := workerVerb(argv)
		switch {
		case why == "":
			// a worker's write names the epoch its worker holds, whoever sent it (runStep)
			serving = true
			if lanes != nil && (isFriendBeat(argv) || isBudget(argv)) {
				lane = "beat"
			}
			args = slices.Concat(argv[:words], []string{"--redis", a.serveAddr, "--actor", as}, argv[words:])
		case local:
			v := readVerb(argv)
			if why = v.unserved(); why == "" {
				// a worker's verb is held to the epoch its worker holds whatever its words
				// (runStep); who acts is the caller's --actor, and no one when it gave none,
				// never whoever the server's own environment names
				serving = verbClasses[v.name] == classWorker
				if lanes != nil && v.err == nil && !v.help && onReadLane(v) {
					lane = "read"
				}
				callerArgs := argv
				callerArgs, actorNotice, actorResolved = a.defaultHookActor(argv, a.serveAddr, true)
				if actorResolved {
					args = slices.Concat(argv[:v.words], []string{"--redis", a.serveAddr}, callerArgs[v.words:])
				} else {
					args = slices.Concat(argv[:v.words], []string{"--redis", a.serveAddr, "--actor", ""}, callerArgs[v.words:])
				}
			}
		}
		if why != "" {
			verb := ""
			if len(argv) > 0 {
				verb = argv[0]
			}
			out.Results[i] = sprintwire.Result{Code: 2, Stderr: fmt.Sprintf("%s server: %s: %s; nothing was changed\n", prog, oneline.Escape(verb), oneline.Escape(why)), Outcome: "refused", Changed: "no"}
			a.logRefused(argv, refusedWorker("", argv), 2, out.Results[i].Stderr)
			continue
		}
		if isFleetBeatDryRun(argv) {
			// the doctor's member-env probe: the form is read and a beat of it is planned, and
			// nothing is written, so an installed build whose form the server would refuse
			// answers here (docs/SPEC-SPRINT.md, "The doctor's refused-verb and beat-form checks").
			out.Results[i] = sprintwire.Result{Code: 0, Stdout: fmt.Sprintf("FLEET-BEAT WOULD %s dry-run: nothing written\n", as), Outcome: "accepted", Changed: "no"}
			continue
		}
		switch lane {
		case "beat":
			beats++
			if isBudget(argv) {
				out.Results[i] = lanes.budget(ctx, argv, args)
				a.logRefused(argv, refusedWorker(as, argv), out.Results[i].Code, out.Results[i].Stderr)
				continue
			}
			out.Results[i] = lanes.friendBeat(ctx, a, argv, args[words:])
			a.logRefused(argv, refusedWorker(as, argv), out.Results[i].Code, out.Results[i].Stderr)
			continue
		case "read":
			reads++
			out.Results[i] = lanes.readVerbRun(ctx, args)
			a.logRefused(argv, refusedWorker(as, argv), out.Results[i].Code, out.Results[i].Stderr)
			continue
		}
		if !held {
			if err := a.serial.LockCtx(ctx); err != nil {
				for j := i; j < len(req.Verbs); j++ {
					out.Results[j] = goneResult(req.Verbs[j])
					gone++
				}
				return out
			}
			held, took, first = true, a.now(), argv
		}
		onLine++
		a.serving = serving
		var stdout, stderr bytes.Buffer
		if actorNotice != "" {
			fmt.Fprintln(&stderr, actorNotice)
		}
		code := a.run(args, &stdout, &stderr)
		out.Results[i] = sprintwire.Result{Code: code, Stdout: stdout.String(), Stderr: stderr.String(), Outcome: a.outcome, Changed: a.changed}
		a.logRefused(argv, as, code, stderr.String())
		if code == 0 && len(argv) > 0 && argv[0] == "queue" && as != "" {
			if st, err := a.store(common{redis: a.serveAddr, actor: as}); err == nil && st != nil {
				// ignored: a best-effort lease renewal on reader beat; next beat will renew
				_, _ = st.Run(ctx, store.Step{
					Verb: "lease",
					Load: []string{sprint.Readers},
					Plan: func(s *sprint.Snapshot) sprint.Plan {
						return sprint.RenewReaderLeases(s, as)
					},
				})
			}
		}
	}
	return out
}

// refusedLogBytes is the ceiling on the verb's own first stderr line in a SERVE REFUSED
// line: enough for a refusal sentence, short enough that one runaway value cannot be the
// whole of the reader's context (oneline.Cap).
const refusedLogBytes = 300

// refusedVerb is the verb word a refused verb is named by: the first word of its argv.
func refusedVerb(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	return argv[0]
}

// isFleetBeatDryRun says a verb is a fleet beat carrying --dry-run: the doctor's member-env
// probe, planned and answered without a write (docs/SPEC-SPRINT.md, "The doctor's
// refused-verb and beat-form checks").
func isFleetBeatDryRun(argv []string) bool {
	if len(argv) < 2 || argv[0] != "fleet" || argv[1] != "beat" {
		return false
	}
	return slices.Contains(argv[2:], "--dry-run")
}

// refusedVerbName is the verb word a refused verb is recorded by: a beat is its two words
// (the doctor's member-beat check names `fleet beat` and not `fleet`), every other verb its
// first word (refusedVerb).
func refusedVerbName(argv []string) string {
	if len(argv) >= 2 && (argv[0] == "fleet" || argv[0] == "friend") && argv[1] == "beat" {
		return argv[0] + " beat"
	}
	return refusedVerb(argv)
}

// recordRefused appends one refused verb to the epoch's refused-verb record, the server's own
// record the doctor reads (docs/SPEC-SPRINT.md, "The doctor's refused-verb and beat-form
// checks"): the verb, the member, the card it named, what it sent and the refusal's first
// line. It is best effort; nothing of the refusal's answer depends on it.
func (a *app) recordRefused(argv []string, as string, code int, stderr string) {
	member := refusedWorker(as, argv)
	if member == "" {
		return
	}
	st, err := a.store(common{redis: a.serveAddr, actor: member})
	if err != nil || st == nil {
		return
	}
	// ignored: the record is best effort; a refusal is answered whether or not it is recorded
	_ = st.RecordRefused(context.Background(), sprint.RefusedVerb{
		At:     a.now(),
		Verb:   refusedVerbName(argv),
		Member: member,
		Card:   refusedCard(argv),
		Argv:   strings.Join(argv, " "),
		Why:    firstRefusalLine(stderr),
	})
}

// refusedWorker is the worker a refused verb names: as workerVerb read it; a beat refused
// before workerVerb read its member names it positionally (`fleet beat <member>` /
// `friend beat <member>`, its third word), so a form refusal (no --load, an unknown word) is
// still recorded under the member that sent it; otherwise the --as word itself for a verb
// the server did not run far enough to read it; "" when it names none.
func refusedWorker(as string, argv []string) string {
	if as != "" {
		return as
	}
	if len(argv) >= 3 && (argv[0] == "fleet" || argv[0] == "friend") && argv[1] == "beat" && sprint.ValidID(argv[2]) {
		return argv[2]
	}
	for i, w := range argv {
		if w == "--as" && i+1 < len(argv) {
			return argv[i+1]
		}
		if v, ok := strings.CutPrefix(w, "--as="); ok {
			return v
		}
	}
	return ""
}

// refusedCard is the card argument a refused verb names, "" when it names none: the first
// word shaped <id>@<gen> (never a flag's value, which starts with a dash).
func refusedCard(argv []string) string {
	for _, w := range argv {
		if strings.HasPrefix(w, "-") {
			continue
		}
		if id, gen, ok := strings.Cut(w, "@"); ok && gen != "" && sprint.ValidCardID(id) {
			return w
		}
	}
	return ""
}

// firstRefusalLine is the first line of a refused verb's own stderr, one line and cut to
// refusedLogBytes, marked when it was cut: the verb's own words, never a summary.
func firstRefusalLine(stderr string) string {
	s := strings.TrimRight(stderr, "\n")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return oneline.Escape(oneline.Cap(strings.TrimSpace(s), refusedLogBytes))
}

// refusedLine is a SERVE REFUSED line's text before the repeat suffix: the verb word, the
// worker, the exit, the card when the verb names one, and the verb's own first stderr line.
func refusedLine(verb, as string, code int, card, first string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "SERVE REFUSED verb=%s as=%s exit=%d",
		oneline.Field(orDashStr(verb, "-")), oneline.Field(orDashStr(as, "-")), code)
	if card != "" {
		fmt.Fprintf(&b, " card=%s", oneline.Field(card))
	}
	if first != "" {
		fmt.Fprintf(&b, ": %s", first)
	}
	return b.String()
}

// logRefused records a verb the server answered with a non-zero code on the server's own
// log: one line naming the verb, the worker, the exit and the card when the verb names one,
// with the verb's own first stderr line. The record is rate bounded: the same verb, worker,
// exit and first line repeated within ServeSayEvery is written once, and when it changes the
// line that follows says how many repeats it stood for, so 40 minutes of refused finishes
// from ten members is a few hundred lines and never one per verb. It carries the verb word,
// the worker, the exit, the card argument and stderr's first line only: never the verb's
// whole argv (a brief, a report) or its stdout. It is the record the v1.2.10 outage lacked
// (docs/SPEC-SPRINT.md section 14, The server).
func (a *app) logRefused(argv []string, as string, code int, stderr string) {
	if code == 0 {
		return
	}
	verb, card := refusedVerb(argv), refusedCard(argv)
	a.recordRefused(argv, as, code, stderr)
	if a.serveLog == nil {
		return
	}
	text := refusedLine(verb, as, code, card, firstRefusalLine(stderr))
	now := a.now()
	t := &a.served
	t.mu.Lock()
	if t.refused == nil {
		t.refused = map[string]int{}
	}
	t.refused[verb]++
	var said []string
	switch {
	case text == t.refuseText && now.Sub(t.refuseSince) < ServeSayEvery:
		t.refuseRepeat++
	case t.refuseRepeat > 0:
		said = append(said, fmt.Sprintf("%s (repeated %d times since %s)", t.refuseText, t.refuseRepeat, t.refuseSince.Format("15:04:05")))
		t.refuseRepeat, t.refuseSince = 0, now
		if text != t.refuseText {
			said = append(said, text)
			t.refuseText = text
		}
	default:
		said = append(said, text)
		t.refuseText, t.refuseSince = text, now
	}
	t.mu.Unlock()
	for _, line := range said {
		a.serveSay("%s", line)
	}
}

// ServeHTTP is the listener's shell around serve: one endpoint, a batch in the
// body, its results in the answer.
func (a *app) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.serveHTTP(w, r, false) }

// localHandler is the shell for the loopback listener: the coordinator's verbs.
type localHandler struct{ a *app }

func (h localHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.a.serveHTTP(w, r, true) }

func (a *app) serveHTTP(w http.ResponseWriter, r *http.Request, local bool) {
	if strings.HasPrefix(r.URL.Path, viewPath) {
		a.serveView(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, friendCardsPath) {
		a.serveFriendCards(w, r)
		return
	}
	if r.URL.Path != sprintwire.Path || r.Method != http.MethodPost {
		http.Error(w, "the sprint server takes POST "+sprintwire.Path, http.StatusNotFound)
		return
	}
	var req sprintwire.Request
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, sprintwire.MaxRequest))
	if err == nil {
		err = json.Unmarshal(body, &req)
	}
	if err != nil {
		http.Error(w, "the request is not a batch of verbs: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Verbs) > sprintwire.MaxVerbs {
		http.Error(w, fmt.Sprintf("a batch is at most %d verbs, found %d", sprintwire.MaxVerbs, len(req.Verbs)), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// THE ANSWER IS COMPRESSED FOR A CLIENT THAT TAKES IT. A worker's queue carries each of
	// its cards' briefs, every pass: 46 to 93 KB an answer on a sprint of 1000 cards, and a
	// worker on a slow link takes seconds to read one.
	// The briefs are text and alike, and go to a twentieth. Go's client asks
	// for gzip and reads it by itself.
	var out io.Writer = w
	if takesGzip(r.Header.Get("Accept-Encoding")) {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		// ignored: a worker that has gone reads no answer
		defer func() { _ = gz.Close() }()
		out = gz
	}
	// ignored: a worker that has gone reads no answer; what ran is in the sprint's log
	_ = json.NewEncoder(out).Encode(a.serveCtx(r.Context(), req, local))
}

// takesGzip says a request's Accept-Encoding names gzip and does not refuse it (q=0).
func takesGzip(accept string) bool {
	for _, part := range strings.Split(accept, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.TrimSpace(name) != "gzip" {
			continue
		}
		q := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(params), " ", ""), "q=")
		if f, err := strconv.ParseFloat(q, 64); params != "" && err == nil && f == 0 {
			return false
		}
		return true
	}
	return false
}

// listenRefused is why host is not an address the server binds, "" when it is.
// The decision is listenable's, so the server and the dashboard refuse the same
// addresses: a name, a public address, a link-local address and an unspecified
// address. Loopback, a private address and the tailnet stay. docs/SPEC-SPRINT.md
// section 14, The server. The caller returns this before net.Listen.
func listenRefused(host string) string {
	const refused = "--listen wants one address of this machine (its address on the fleet's private network, or 127.0.0.1): the server checks no credential, so it does not listen on every network"
	if host == "" || listenable(net.ParseIP(host)) != "" {
		return refused
	}
	return ""
}

// listen starts the server on the address for the store the run loop ticks,
// and returns once it is listening. The address is the coordinator's machine's
// on the fleet's private network, which is what keeps others out: the server
// checks no credential, relying on the fleet's private network as the whole
// of its access control, so an address every network can reach is refused
// (docs/SPEC-SPRINT.md section 14, The server). The refusal is returned before
// a socket is opened. The coordinator's verbs stay on loopback; a private or
// tailnet address is the workers' listener beside that loopback listener.
func (a *app) listen(addr, redis string, stdout io.Writer) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("--listen wants host:port, found %s", addr)
	}
	if why := listenRefused(host); why != "" {
		return errors.New(why)
	}
	// the fleet's listener takes the workers' verbs; the loopback one, on the same port,
	// takes the coordinator's (any verb the server runs). An address that is loopback
	// itself is the one listener, the coordinator's
	_, port, _ := net.SplitHostPort(addr)
	loop := net.JoinHostPort("127.0.0.1", port)
	lns := map[string]http.Handler{loop: localHandler{a}}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		lns[addr] = a
	}
	a.serveAddr, a.serveLog = redis, stdout
	// the lanes are made before the first batch, while the line is free: a batch never
	// waits for the line to make them (servelanes.go)
	a.lanesFor(context.Background())
	if st, err := a.store(common{redis: redis}); err == nil && st != nil {
		// ignored: a best-effort read cleanup on server listen; tick takes care of any subsequent lapses
		_ = a.serverStart(context.Background(), st)
	}
	// the coordinator's push: its judgments to its name on the bus, and its sprint push
	// held in its nova-hook record (coordinator_push.go)
	go a.coordPushLoop(context.Background())
	for at, h := range lns {
		ln, err := net.Listen("tcp", at)
		if err != nil {
			return fmt.Errorf("--listen %s: %w", at, err)
		}
		srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: time.Minute, WriteTimeout: 5 * time.Minute}
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				fmt.Fprintf(stdout, "SERVER STOPPED %s: %s; its verbs are not taken until run is started again\n", at, oneline.Escape(err.Error()))
			}
		}()
	}
	if _, fleet := lns[addr]; fleet {
		fmt.Fprintf(stdout, "SERVER listening on %s: the workers' verbs (take, finish, read, queue, fleet beat) run here, one at a time, beside the store\n", addr)
	}
	fmt.Fprintf(stdout, "SERVER listening on %s: the coordinator's verbs, from this machine (NOVA_SPRINT_SERVER=%s)\n", loop, loop)
	return nil
}

// friendBeatFlags are the flags of a friend's beat the server runs, each with the shape of
// its value: what her machinery reports of her work (friend beat), and her daemon's word
// that she is down until a time and why (--until, --reason: her harness at its limit).
var friendBeatFlags = map[string]func(string) bool{
	"--running": runningIDs,
	"--live": func(v string) bool {
		_, err := sprint.ParseLive(v)
		return err == nil
	},
	"--working": wholeAtLeast(0),
	"--queue":   wholeAtLeast(0),
	"--width":   wholeAtLeast(1),
	"--load": func(v string) bool {
		f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
		return err == nil && f >= 0
	},
	"--active": rfc3339,
	"--pong":   oneLineText, // any word: the server's proof step says a beat with no proof
	"--check":  sprint.ValidID,
	"--run":    sprint.ValidID,
	"--until":  rfc3339,
	"--reason": oneLineText,
}

// oneLineText is the shape of a short text on one line: not empty, no control character.
func oneLineText(v string) bool {
	if v == "" || len(v) > 1024 {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// rfc3339 is the shape of a time.
func rfc3339(v string) bool {
	_, err := time.Parse(time.RFC3339, v)
	return err == nil
}

// wholeAtLeast is the shape of a count of at least min.
func wholeAtLeast(min int) func(string) bool {
	return func(v string) bool {
		n, err := strconv.Atoi(v)
		return err == nil && n >= min
	}
}

// friendBeatServed names friendBeatFlags for a refusal.
var friendBeatServed = strings.Join(slices.Sorted(maps.Keys(friendBeatFlags)), ", ")

// friendBeatReport is why the words after a friend's beat's name are not its report's
// flags, each once with a value of its shape; "" when they are.
func friendBeatReport(words []string) string {
	seen := map[string]bool{}
	for i := 0; i < len(words); i += 2 {
		ok, known := friendBeatFlags[words[i]]
		switch {
		case !known || seen[words[i]]:
			return "a friend's beat sent to the server takes its report's flags (" + friendBeatServed + "), each once with its value, and nothing more; found " + oneline.Escape(words[i])
		case i+1 == len(words) || !ok(words[i+1]):
			return "a friend's beat's " + words[i] + " wants its value"
		}
		seen[words[i]] = true
	}
	return ""
}

// runningIDs says the value is a list of card ids or job names, comma separated: letters,
// digits, and . _ - ~ only.
func runningIDs(v string) bool {
	if v == "" || len(v) > 4096 {
		return false
	}
	for _, r := range v {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("._-~,", r) {
			return false
		}
	}
	return true
}

// viewPath is where the server serves the role views (view.go): GET /api/view/coordinator
// and GET /api/view/worker?as=<name>, each with since=<cursor>, and the coordinator's with
// all=1. They are reads, served on both listeners as the workers' queue is: the fleet's
// private network is the whole of the access control (listen).
const viewPath = "/api/view/"

// serveView runs view <role> --json for a GET, on the line of control as any verb the server
// runs (a.serial: never during a tick), and answers its JSON, gzipped for a client that takes
// it. A role, a name or a cursor of the wrong shape is a 400 and nothing is run; a name that
// is no worker of the sprint is a 404; a store that did not answer is a 503; each with the
// verb's line.
func (a *app) serveView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "the views are read with GET "+viewPath+"coordinator or "+viewPath+"worker?as=<name>", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	role := strings.TrimPrefix(r.URL.Path, viewPath)
	argv := []string{"view", role, "--redis", a.serveAddr, "--actor", "", "--json"}
	switch role {
	case "coordinator":
		if on, err := strconv.ParseBool(cmp.Or(q.Get("all"), "false")); err != nil {
			http.Error(w, "all is 1 or 0: "+oneline.Err(err), http.StatusBadRequest)
			return
		} else if on {
			argv = append(argv, "--all")
		}
	case "cards":
		if q.Get("since") != "" {
			http.Error(w, "the cards view takes no since", http.StatusBadRequest)
			return
		}
		for _, k := range []string{"col", "stream", "holder", "by"} {
			if v := q.Get(k); v != "" {
				if !sprint.ValidID(v) {
					http.Error(w, k+" is a name (letters, digits, _ and -)", http.StatusBadRequest)
					return
				}
				argv = append(argv, "--"+k, v)
			}
		}
	case "worker":
		as := q.Get("as")
		if !sprint.ValidID(as) {
			http.Error(w, "as=<name> names one fleet member or friend (letters, digits, _ and -)", http.StatusBadRequest)
			return
		}
		argv = append(argv, "--as", as)
	default:
		http.Error(w, "the views are "+viewPath+"coordinator, "+viewPath+"cards and "+viewPath+"worker?as=<name>", http.StatusNotFound)
		return
	}
	if since := q.Get("since"); since != "" {
		if _, err := parseCursor(since); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		argv = append(argv, "--since", since)
	}
	var stdout, stderr bytes.Buffer
	a.serial.Lock()
	code := a.run(argv, &stdout, &stderr)
	a.serial.Unlock()
	switch code {
	case 0:
	case 1:
		http.Error(w, strings.TrimSpace(stderr.String()), http.StatusNotFound)
		return
	default:
		http.Error(w, strings.TrimSpace(stderr.String()), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	var out io.Writer = w
	if takesGzip(r.Header.Get("Accept-Encoding")) {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer func() { _ = gz.Close() }() // ignored: a reader that has gone reads no answer
		out = gz
	}
	_, _ = out.Write(stdout.Bytes()) // ignored: a reader that has gone reads no answer
}
