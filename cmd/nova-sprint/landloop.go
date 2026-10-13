package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

// The server lands what the readers passed (run --land): the last of the sprint's
// writers outside the server was a coordinator's `land` run beside it, racing the
// tick's accepts for the merge queue: cards queue up and landings are refused
// round after round while both write. Here one landing runs
// at a time, in the server's own process: land's reads and its report take the server's
// line of control (cmdLand, a.serial), its git runs outside it, and nothing else writes
// the merge queue between them but the server itself.

// LandEvery is how often the loop looks for cards queued to merge.
const LandEvery = 2 * time.Second

// LandDeadline is how long a landing may run before the loop raises one judgment
// naming the stage it is in. In a pass each batch's gate is also abandoned at
// this bound, on the batch's own clock from when it holds a width slot
// (landpass.go launch), nothing blamed; a batch waiting for the chain or a slot
// runs no clock, and one waiting for another stream's gate of its base commit
// (acquireGate) leaves that wait at its own bound; the landing continues. A hand
// land of two cards finished in under eight minutes; each gate run still has
// its own landGoBudget.
const LandDeadline = 10 * time.Minute

// landLaneWho is the lander's name on a bench's Go lane, and the prefix of every holder it
// records there: a stream's gate holds as lander/<stream> (fork, laneWho), the base
// re-check as landLaneBase, so the parallel pass's forks are distinct holders and a sibling
// asking a bench one of them holds is queued, never granted by the other's name, and one
// fork's give never frees another's bench (docs/SPEC-SPRINT.md section 7). A lander that is
// neither (none today) holds as landLaneWho.
const landLaneWho = "lander"

// landLaneBase is the holder the base re-check's gate records on a bench's Go lane.
const landLaneBase = landLaneWho + "/base"

// landLoop runs one land cycle every LandEvery until ctx ends. Each cycle writes
// one line: LAND OK, LAND REFUSED, or LAND IDLE. A landing runs beside the loop,
// so a long gate still leaves a line. A refusal is a state, not an event: while one
// stands every cycle writes LAND REFUSED naming its reason and its remedy, and its
// first cycle raises one judgment at once (landRefusal; tla/ServerSwitch.tla
// LandRefused, LandOk).
func (a *app) landLoop(ctx context.Context, addr string, stdout io.Writer) {
	fmt.Fprintf(stdout, "LANDING every %s: land runs here for every stream with cards queued to merge, one landing at a time\n", LandEvery)
	var flight *landFlight
	for ctx.Err() == nil {
		flight = a.landCycle(ctx, addr, stdout, flight)
		if ctx.Err() != nil {
			break
		}
		a.landPause(ctx)
	}
}

// landPause waits LandEvery between land cycles on the app's own sleep, and returns at
// once when ctx ends: a returning run stops an idle land loop immediately instead of
// waiting out its LandStopBound (start-refuses-a-non-working-setting, cold read 1).
// The sleep runs in its own goroutine, which ends when the sleep does.
func (a *app) landPause(ctx context.Context) {
	slept := make(chan struct{})
	go func() {
		defer close(slept)
		a.sleep(LandEvery)
	}()
	select {
	case <-ctx.Done():
	case <-slept:
	}
}

// landOnce is a round's landing: land's exit code, and idle when the merge queue was
// read and held nothing.
func (a *app) landOnce(ctx context.Context, addr string, more []string, stdout io.Writer) (int, bool) {
	a.serial.Lock()
	queued, coordinator, err := a.queuedToMerge(ctx, addr)
	a.serial.Unlock()
	idle := err == nil && !queued
	ran := false // land ran: the queue was read, held cards and named a coordinator
	var lines []string
	code := 0
	var gate *hookGateError
	switch {
	case err != nil:
		lines, code = []string{"LAND FAILED the merge queue could not be read: " + oneline.Err(err) + "; nothing was landed, and the next round tries again; run: nova-sprint where"}, 2
	case queued && coordinator != "":
		var out, errb bytes.Buffer
		ran = true
		a.landLazy, a.landCtx = true, ctx
		code = a.landRun()(append([]string{"--redis", addr, "--actor", coordinator}, more...), &out, &errb, &gate)
		a.landLazy, a.landCtx = false, nil
		// what landed (stdout's LAND lines, but its summary), and everything land said
		// was wrong (stderr: a refused or failed batch, a refusal before any batch, the
		// remedy)
		for _, line := range strings.Split(out.String(), "\n") {
			if strings.HasPrefix(line, "LAND ") && !strings.HasPrefix(line, "LAND DONE") {
				lines = append(lines, line)
			}
		}
		for _, line := range strings.Split(errb.String(), "\n") {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, line)
			}
		}
	}
	if gate != nil {
		// The loop's own landing is machine work and is exempt from the hook gate, so
		// its pass runs whatever the hook (land.go reopens the store as run; the
		// automatic pass: tla/ServerSwitch.tla LandOk). This is the hand land's gate
		// alone: the seat has not hooked in, as after a restart. A refusal is a wait,
		// not a fault of the pass, so the rollback window is not asked; the next pass
		// tries again (docs/SPEC-SPRINT.md section 14; tla/ServerSwitch.tla
		// LandHookRefused).
		a.landFailed = ""
		a.writeCycle(stdout, "LAND WAITS for the seat's hook: "+gate.why+"; nothing was landed, and the next pass tries again")
		return code, idle
	}
	said := ""
	if code != 0 {
		exe := a.executable
		if exe == nil {
			exe = os.Executable
		}
		if target, err := exe(); err == nil {
			landErr := &sprint.LandError{Msg: fmt.Sprintf("land failed: %d", code), Code: code}
			if rolled, rerr := sprint.CheckRollbackOnLandFailure(target, landErr, a.now()); rolled {
				at := oneline.Field(a.now().Format("15:04:05"))
				fmt.Fprintf(stdout, "%s SERVER ROLLBACK land failed within switch window; rolled back to previous binary %s.prev\n", at, target)
			} else if rerr != nil {
				at := oneline.Field(a.now().Format("15:04:05"))
				fmt.Fprintf(stdout, "%s SERVER ROLLBACK ERROR: %s\n", at, oneline.Escape(rerr.Error()))
			}
		}
		said = strings.Join(lines, "\n")
	}
	switch {
	case code != 0 && !landBatched(lines):
		// nothing could land: a refusal before any batch (a setting such as the
		// git_identity, the epoch, the fence), a pass that failed, or a queue that could
		// not be read. It stands until a pass gets past it or it changes: its lines are
		// said when it begins, the beat names it every cycle (writeRefused), and its
		// judgment is single (tla/ServerSwitch.tla LandRefused)
		if !a.standRefusal(said, coordinator) {
			a.landFailed = said
			return code, idle
		}
	case ran:
		// the pass got past every check before its batches: whatever each batch did
		// (landed, or refused and recorded on its own), no refusal stands
		// (tla/ServerSwitch.tla LandOk, LandPartial)
		a.clearRefusal()
		if code != 0 && said == a.landFailed {
			return code, idle // said when it began
		}
	}
	a.landFailed = said
	at := oneline.Field(a.now().Format("15:04:05"))
	for _, line := range lines {
		fmt.Fprintf(stdout, "%s %s\n", at, oneline.Escape(line))
	}
	return code, idle
}

// queuedToMerge says a stream has a card queued to merge, and names the sprint's
// coordinator, whose the landing is ("" when the sprint has none); err when the sprint
// could not be read, which says nothing of what is queued.
func (a *app) queuedToMerge(ctx context.Context, addr string) (queued bool, coordinator string, err error) {
	n, coordinator, _, err := a.landQueued(ctx, addr)
	return n > 0, coordinator, err
}

// mergeQueued counts the cards queued to merge, and names the coordinator and the
// first stream that has one. err when the sprint could not be read.
func (a *app) mergeQueued(ctx context.Context, addr string) (n int, coordinator, stream string, err error) {
	st, err := a.storeCtx(ctx, common{verb: "where", redis: addr})
	if err != nil {
		return 0, "", "", err
	}
	if coordinator, err = st.B.Coordinator(ctx); err != nil {
		return 0, "", "", err
	}
	s, err := st.Load(ctx, []string{sprint.Merge}, nil)
	if err != nil {
		return 0, "", "", err
	}
	for _, row := range s.Merge.Rows() {
		c := s.Merge.Count(row, sprint.Queued)
		if c <= 0 {
			continue
		}
		n += c
		if stream == "" {
			stream = row
		}
	}
	return n, coordinator, stream, nil
}

// landRefusal is the lander's standing refusal (tla/ServerSwitch.tla refusal and
// judged): the reason and the remedy read off land's refusal line, its key (the
// reason normalised, sprint.LandRefusalKey: what a refusal is the same as), since when
// it has stood, the text of the refusal it replaced (whose judgment it closes), the
// coordinator its judgment is addressed by, and whether that judgment is written.
type landRefusal struct {
	reason, remedy, key string
	prev                string
	since               time.Time
	coord               string
	judged              bool
}

// what is the refusal's judgment text: its reason and its remedy.
func (r landRefusal) what() string { return r.reason + "; run: " + r.remedy }

// landRefusalRemedy is the remedy a refusal that names none is given: the dry run
// prints the batches land would make, and any refusal before them.
const landRefusalRemedy = "nova-sprint land --dry-run"

// landBatchLine is a batch's line (land.go landBatch.line): LAND <STATUS> stream=...
var landBatchLine = regexp.MustCompile(`^LAND [A-Z]+ stream=`)

// landBatched says land reached its batches: a batch landed or was refused. Such a
// pass is never a standing refusal, whatever land's exit (land exits 1 when any batch
// was refused, though others landed); each refused batch has its own record and
// judgment (sprint.PropLandRefusal; tla/ServerSwitch.tla LandPartial).
func landBatched(lines []string) bool {
	for _, line := range lines {
		if landBatchLine.MatchString(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

// landRefusalOf reads a refusal's reason and remedy off what land said, never off its
// first line (a pass's first line may be a LAND OK). The reason is the text after
// REFUSED: on land's refusal line (refuse, main.go), else the loop's own LAND FAILED
// line, else land's own "nova-sprint land: <why>" line (an epoch or a read that
// failed), else the first line; it is cut at its last "; run: ", and the command after
// that is the remedy, else landRefusalRemedy.
func landRefusalOf(said string) (reason, remedy string) {
	const run = "; run: "
	var refused, failed, landSaid, first string
	for _, line := range strings.Split(said, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case refused == "" && strings.Contains(line, " REFUSED: "):
			refused = line[strings.Index(line, " REFUSED: ")+len(" REFUSED: "):]
		case failed == "" && strings.HasPrefix(line, "LAND FAILED "):
			failed = line
		case landSaid == "" && strings.HasPrefix(line, prog+" land: "):
			landSaid = strings.TrimPrefix(line, prog+" land: ")
		}
		if first == "" {
			first = line
		}
	}
	reason = first
	for _, r := range []string{landSaid, failed, refused} {
		if r != "" {
			reason = r
		}
	}
	if i := strings.LastIndex(reason, run); i >= 0 {
		remedy = strings.TrimSpace(reason[i+len(run):])
		reason = reason[:i]
	}
	if remedy == "" {
		remedy = landRefusalRemedy
	}
	return strings.TrimSpace(reason), remedy
}

// standRefusal records a refusal before any batch, what land said. A refusal with the
// same key as the one that stands is the same refusal (its since and its judgment
// kept), whatever counts, timings or ids its text carries; another replaces it, to be
// judged anew (tla/ServerSwitch.tla LandRefused). changed says it replaced one.
func (a *app) standRefusal(said, coord string) (changed bool) {
	reason, remedy := landRefusalOf(said)
	key := sprint.LandRefusalKey(reason)
	b := a.landState()
	b.mu.Lock()
	defer b.mu.Unlock()
	if r := b.refusal; r != nil && r.key == key {
		if r.coord == "" {
			r.coord = coord
		}
		return false
	}
	prev := ""
	if b.refusal != nil && b.refusal.judged {
		prev = b.refusal.what()
	}
	b.refusal = &landRefusal{reason: reason, remedy: remedy, key: key, prev: prev, since: a.now(), coord: coord}
	return true
}

// clearRefusal ends the standing refusal once a pass has got past every check before
// its batches (land ran, and landed, refused a batch, or found nothing to land); its
// judgment is closed by the next judgeRefusal, as is any a previous server left open
// (tla/ServerSwitch.tla LandOk, LandPartial). A round that did not run land (no
// coordinator, nothing queued) clears nothing.
func (a *app) clearRefusal() {
	b := a.landState()
	b.mu.Lock()
	defer b.mu.Unlock()
	if r := b.refusal; r != nil || !b.swept {
		if r != nil {
			b.closeAs = r.coord
		}
		b.closing = true
	}
	b.refusal = nil
}

// standingRefusal is a copy of the standing refusal, nil when none stands.
func (a *app) standingRefusal() *landRefusal {
	b := a.landState()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.refusal == nil {
		return nil
	}
	r := *b.refusal
	return &r
}

// judgeNotWritten says once, until it changes or a write lands, why the standing
// refusal's judgment (or its close) was not written: LAND JUDGMENT NOT WRITTEN.
func (a *app) judgeNotWritten(w io.Writer, why string) {
	b := a.landState()
	b.mu.Lock()
	said := b.notWritten == why
	b.notWritten = why
	b.mu.Unlock()
	if !said {
		a.writeCycle(w, "LAND JUDGMENT NOT WRITTEN: "+why+"; it is tried again next beat")
	}
}

// judgeWritten forgets the last LAND JUDGMENT NOT WRITTEN line.
func (a *app) judgeWritten() {
	b := a.landState()
	b.mu.Lock()
	b.notWritten = ""
	b.mu.Unlock()
}

// judgeRefusal writes the standing refusal's one judgment on the first beat it can,
// and closes a cleared one's. Once written it is checked against the store each beat,
// never only the flag: a judgment another server's step closed (its own, other,
// refusal replacing it) is raised again, while one the seat acknowledged stays
// acknowledged (sprint.LandRefusalSaid). The write is tried, not waited on (the
// server's line of control is taken only when free); one that does not land is said,
// once, as LAND JUDGMENT NOT WRITTEN and tried again next beat. The store is opened as
// the machine (verb run), so a seat that has not hooked in, as after a restart, is
// still told (tla/ServerSwitch.tla RefusalJudgedAtOnce).
func (a *app) judgeRefusal(ctx context.Context, addr string, w io.Writer) {
	b := a.landState()
	b.mu.Lock()
	closing, closeAs := b.closing, b.closeAs
	r := b.refusal
	var standing landRefusal
	if r != nil {
		standing = *r
	}
	b.mu.Unlock()
	if !closing && r == nil {
		return
	}
	if !a.serial.TryLock() {
		return
	}
	defer a.serial.Unlock()
	if r == nil {
		st, err := a.storeCtx(ctx, common{verb: "run", redis: addr, actor: closeAs})
		if err != nil {
			a.judgeNotWritten(w, "the close of the lander's refusal: the store could not be opened: "+oneline.Err(err))
			return
		}
		res, err := st.Run(ctx, store.Step{Verb: "land", Plan: sprint.LandRefusalClose})
		if why := stepNotWritten(res, err); why != "" {
			a.judgeNotWritten(w, "the close of the lander's refusal: "+why)
			return
		}
		a.judgeWritten()
		b.mu.Lock()
		if b.refusal == nil {
			b.closing, b.swept, b.closeAs = false, true, ""
		}
		b.mu.Unlock()
		return
	}
	st, err := a.storeCtx(ctx, common{verb: "run", redis: addr, actor: standing.coord})
	if err != nil {
		a.judgeNotWritten(w, "the store could not be opened: "+oneline.Err(err))
		return
	}
	what := standing.what()
	if standing.judged {
		open, err := st.B.OpenNotes(ctx)
		if err != nil || sprint.LandRefusalSaid(open, what) {
			return // a read that failed is tried again next beat; the judgment stands
		}
	}
	res, err := st.Run(ctx, store.Step{Verb: "land", Plan: func(s *sprint.Snapshot) sprint.Plan {
		return sprint.LandRefusalRaise(s, what, standing.coord, standing.prev)
	}})
	if why := stepNotWritten(res, err); why != "" {
		a.judgeNotWritten(w, why)
		return
	}
	a.judgeWritten()
	b.mu.Lock()
	if b.refusal == r {
		r.judged = true
		// a raise closes the replaced refusal's judgment, so nothing is left to close
		b.closing, b.swept = false, true
	}
	b.mu.Unlock()
}

// stepNotWritten is why a step's write did not land: its error, or its refusal.
func stepNotWritten(res store.Result, err error) string {
	if err != nil {
		return oneline.Err(err)
	}
	if len(res.Refused) > 0 {
		return "refused: " + res.Refused[0].Why
	}
	return ""
}

// landRun is land, or the test's stand-in for it (landBeat.landSeam).
func (a *app) landRun() func(args []string, stdout, stderr io.Writer, gate **hookGateError) int {
	b := a.landState()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.landSeam != nil {
		return b.landSeam
	}
	return a.land
}

// landQueued is mergeQueued, or the test's stand-in for it (landBeat.queueSeam).
func (a *app) landQueued(ctx context.Context, addr string) (n int, coordinator, stream string, err error) {
	b := a.landState()
	b.mu.Lock()
	fn := b.queueSeam
	b.mu.Unlock()
	if fn != nil {
		return fn(ctx, addr)
	}
	return a.mergeQueued(ctx, addr)
}

// landStart runs a flight beside the loop, or as the test's stand-in says (landBeat.startSeam).
func (a *app) landStart(run func()) {
	b := a.landState()
	b.mu.Lock()
	fn := b.startSeam
	b.mu.Unlock()
	if fn != nil {
		fn(run)
		return
	}
	go run()
}

// landFlight is one landing the loop started and has not yet reported.
type landFlight struct {
	mu                 sync.Mutex
	queued             int
	step, proc         string
	stream, coord      string
	began              time.Time
	done, judged, idle bool
	out                []byte
	code               int
}

// landBeat is the loop's bench seam and the landing in flight.
type landBeat struct {
	mu        sync.Mutex
	hostName  func() (string, error)
	gateBench func(ctx context.Context, host, dir string, runs [][]string, withGit bool) (string, int, error)
	landMore  []string
	flight    *landFlight
	// refusal is the lander's standing refusal, nil when none stands; closing says a
	// refusal was cleared and its judgment is still to be closed (judgeRefusal), swept
	// that a landing has closed any judgment a previous server left open.
	refusal        *landRefusal
	closing, swept bool
	closeAs        string
	// notWritten is the last LAND JUDGMENT NOT WRITTEN reason said (judgeNotWritten).
	notWritten string
	// landSeam, queueSeam and startSeam are the test's stand-ins for land itself, the
	// merge queue's count and the goroutine a flight runs on: a unit test drives the
	// loop's beats with no store queue, no git and no real time (nil is the real one).
	landSeam  func(args []string, stdout, stderr io.Writer, gate **hookGateError) int
	queueSeam func(ctx context.Context, addr string) (n int, coordinator, stream string, err error)
	startSeam func(run func())
	// pulse is one slot filled each land-loop cycle. A landing waiting to ask
	// for a Go lane again receives it. The loop's own clock is the wait; the
	// ask adds none.
	pulse chan struct{}
}

// landState is the loop's memory, made once.
func (a *app) landState() *landBeat {
	a.prune.mu.Lock()
	defer a.prune.mu.Unlock()
	if a.prune.beat == nil {
		a.prune.beat = &landBeat{pulse: make(chan struct{}, 1)}
	}
	return a.prune.beat
}

// landPulse fills the cycle's one slot. A slot already full is a pulse the
// landing has not taken; this cycle leaves it.
func (a *app) landPulse() {
	b := a.landState()
	b.mu.Lock()
	ch := b.pulse
	b.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// gate is the test seam that runs a gate command in place of the bench.
func (b *landBeat) gate() func(context.Context, string, string, [][]string, bool) (string, int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.gateBench
}

// landStage records the stage the in-flight landing has reached.
func (a *app) landStage(step, proc string) {
	b := a.landState()
	b.mu.Lock()
	f := b.flight
	b.mu.Unlock()
	if f == nil {
		return
	}
	f.mu.Lock()
	f.step, f.proc = step, proc
	f.mu.Unlock()
}

// machineName is this machine's short host name, lower case, the fleet member
// the gate does not send work to. A test's hostName seam stands in for the OS.
func (a *app) machineName() string {
	b := a.landState()
	b.mu.Lock()
	fn := b.hostName
	b.mu.Unlock()
	var host string
	var err error
	if fn != nil {
		host, err = fn()
	} else {
		host, err = os.Hostname()
	}
	if err != nil || host == "" {
		return ""
	}
	host = strings.ToLower(host)
	if i := strings.IndexByte(host, '.'); i >= 0 {
		host = host[:i]
	}
	return host
}

// landCycle is one beat of the loop. A landing still running writes LAND IDLE.
// A landing that finished writes the lines land kept (LAND OK or LAND REFUSED),
// not also an IDLE line. Nothing queued writes LAND IDLE queued=0.
func (a *app) landCycle(ctx context.Context, addr string, stdout io.Writer, flight *landFlight) *landFlight {
	a.landPulse()
	if flight != nil {
		flight.mu.Lock()
		done, idle := flight.done, flight.idle
		flight.mu.Unlock()
		if !done {
			a.writeBeat(stdout, flight)
			a.raiseIfStuck(ctx, addr, flight)
			a.judgeRefusal(ctx, addr, stdout)
			return flight
		}
		a.reportFlight(ctx, addr, stdout, flight, idle)
		return nil
	}
	a.serial.Lock()
	n, coord, stream, err := a.landQueued(ctx, addr)
	a.serial.Unlock()
	if err != nil {
		// said in full once, by landOnce, then named every beat while it stands; an
		// unreadable queue is not an idle one
		a.landOnce(ctx, addr, nil, stdout)
		if r := a.standingRefusal(); r != nil {
			a.writeRefused(stdout, "?", r)
		}
		a.judgeRefusal(ctx, addr, stdout)
		a.landAfter(ctx, false, stdout)
		return nil
	}
	if n == 0 || coord == "" {
		// nothing ran, so nothing is cleared: a standing refusal is still named
		if r := a.standingRefusal(); r != nil && n > 0 {
			a.writeRefused(stdout, strconv.Itoa(n), r)
		} else {
			a.writeIdle(stdout, n, "-", 0, "no-coordinator")
		}
		a.landAfter(ctx, n == 0, stdout)
		return nil
	}
	f := &landFlight{queued: n, step: "land", proc: "nova-sprint land", stream: stream, coord: coord, began: a.now()}
	b := a.landState()
	b.mu.Lock()
	b.flight = f
	more := append([]string(nil), b.landMore...)
	b.mu.Unlock()
	a.landStart(func() { a.runFlight(ctx, addr, f, more) })
	f.mu.Lock()
	done, idle := f.done, f.idle
	f.mu.Unlock()
	if done { // a landing that finished before its first beat is reported on it
		a.reportFlight(ctx, addr, stdout, f, idle)
		return nil
	}
	a.writeBeat(stdout, f)
	a.judgeRefusal(ctx, addr, stdout)
	return f
}

// reportFlight is the cycle that sees a landing done: its lines, the standing
// refusal's judgment, then the cleanup and the promote step.
func (a *app) reportFlight(ctx context.Context, addr string, stdout io.Writer, f *landFlight, idle bool) {
	a.writeFlight(stdout, f)
	a.clearFlight(f)
	a.judgeRefusal(ctx, addr, stdout)
	a.landAfter(ctx, idle, stdout)
}

// runFlight is the landing beside the loop. Its lines stay in the flight until
// the cycle that sees it done.
func (a *app) runFlight(ctx context.Context, addr string, f *landFlight, more []string) {
	var buf bytes.Buffer
	a.landLazy, a.landCtx = true, ctx
	code, idle := a.landOnce(ctx, addr, more, &buf)
	a.landLazy, a.landCtx = false, nil
	f.mu.Lock()
	f.done, f.code, f.idle, f.out = true, code, idle, append([]byte(nil), buf.Bytes()...)
	f.mu.Unlock()
}

// clearFlight forgets f once the loop has reported it.
func (a *app) clearFlight(f *landFlight) {
	b := a.landState()
	b.mu.Lock()
	if b.flight == f {
		b.flight = nil
	}
	b.mu.Unlock()
}

// writeBeat is the in-flight cycle's one line: the standing refusal while one stands
// (a landing tried again under it is not news), else LAND IDLE why=running.
func (a *app) writeBeat(w io.Writer, f *landFlight) {
	f.mu.Lock()
	queued, step, began := f.queued, f.step, f.began
	f.mu.Unlock()
	if r := a.standingRefusal(); r != nil {
		a.writeRefused(w, strconv.Itoa(queued), r)
		return
	}
	a.writeIdle(w, queued, step, a.now().Sub(began), "running")
}

// writeFlight is the cycle that reports a finished landing: the lines land kept, then
// the standing refusal's line while one stands, on every beat and never only the first
// (tla/ServerSwitch.tla BeatNamesTheRefusal). A landing that said nothing and refused
// nothing found nothing it could land (every queued stream stopped, say).
func (a *app) writeFlight(w io.Writer, f *landFlight) {
	f.mu.Lock()
	out := append([]byte(nil), f.out...)
	queued, step, began, idle := f.queued, f.step, f.began, f.idle
	f.mu.Unlock()
	said := len(bytes.TrimSpace(out)) > 0
	if said {
		_, _ = w.Write(out) // ignored: the cycle's line is already formed; a short write is the pipe closing
	}
	if r := a.standingRefusal(); r != nil {
		a.writeRefused(w, strconv.Itoa(queued), r)
		return
	}
	if said {
		return
	}
	if idle {
		queued = 0
	}
	a.writeIdle(w, queued, step, a.now().Sub(began), "nothing-landable")
}

// writeIdle is one LAND IDLE line, the cycle's beat when nothing has finished and no
// refusal stands. With cards queued it says why nothing lands: a landing is running,
// the queue has no coordinator, or the landing found nothing it could land.
func (a *app) writeIdle(w io.Writer, queued int, step string, age time.Duration, why string) {
	if step == "" {
		step = "-"
	}
	if age < 0 {
		age = 0
	}
	line := fmt.Sprintf("LAND IDLE queued=%d step=%s since=%s", queued, step, age.Round(time.Second))
	if queued > 0 {
		line += " why=" + why
	}
	a.writeCycle(w, line)
}

// writeRefused is the standing refusal's line, written in place of LAND IDLE on every
// beat it stands: the cards queued, how long it has stood, its reason and its remedy.
func (a *app) writeRefused(w io.Writer, queued string, r *landRefusal) {
	age := a.now().Sub(r.since)
	if age < 0 {
		age = 0
	}
	a.writeCycle(w, fmt.Sprintf("LAND REFUSED queued=%s since=%s reason=%s run: %s", queued, age.Round(time.Second), r.reason, r.remedy))
}

// writeCycle prefixes one cycle line the way the loop prefixes land's own lines.
func (a *app) writeCycle(w io.Writer, line string) {
	at := oneline.Field(a.now().Format("15:04:05"))
	fmt.Fprintf(w, "%s %s\n", at, oneline.Escape(line))
}

// raiseIfStuck writes one judgment when a landing with cards queued has run past
// LandDeadline. The line is tried, not waited on, so the beat is never stuck
// behind the landing. A write that does not land is tried again next cycle.
func (a *app) raiseIfStuck(ctx context.Context, addr string, f *landFlight) {
	f.mu.Lock()
	if f.judged || f.queued == 0 || a.now().Sub(f.began) < LandDeadline {
		f.mu.Unlock()
		return
	}
	step, proc, stream, coord := f.step, f.proc, f.stream, f.coord
	age := a.now().Sub(f.began).Round(time.Second)
	f.mu.Unlock()
	if proc == "" {
		proc = "nothing named"
	}
	if !a.serial.TryLock() {
		return
	}
	defer a.serial.Unlock()
	f.mu.Lock()
	if f.judged {
		f.mu.Unlock()
		return
	}
	f.mu.Unlock()
	// opened as the machine (verb run): a seat that has not hooked in, as after a
	// restart, is still told (the hook gate refuses the coordinator's verb land)
	st, err := a.storeCtx(ctx, common{verb: "run", redis: addr, actor: coord})
	if err != nil { // ignored: the judgment is tried again next cycle when the store cannot be opened
		return
	}
	n := sprint.Note{
		Kind:        sprint.Judgment,
		Type:        sprint.NOpStuck,
		Stream:      stream,
		StreamLevel: true,
		Who:         coord,
		Decisions:   append([]string(nil), sprint.Decisions[sprint.NOpStuck]...),
		What:        fmt.Sprintf("landing stuck at step=%s waiting on %s for %s", step, proc, age),
	}
	res, err := st.Run(ctx, store.NoteStep("land", n))
	if err != nil || len(res.Refused) > 0 {
		return
	}
	f.mu.Lock()
	f.judged = true
	f.mu.Unlock()
}

// landAfter is the cleanup and the promote step, never during a landing. The cleanup
// (landprune.go) runs when it is due: a round with nothing queued to merge, or PruneEvery
// branches waiting, and no failed cleanup waiting out PruneRetry; its PRUNE lines are
// printed as land's. A stop of the loop flushes nothing: what is still queued then stays
// on origin. Then it runs the promote step when one is armed (promote.go); nil arms
// nothing, so run --land does not open a pull request.
func (a *app) landAfter(ctx context.Context, idle bool, stdout io.Writer) {
	if a.prune.due(idle, a.now()) {
		at := oneline.Field(a.now().Format("15:04:05"))
		for _, r := range a.flushPrune(ctx, false) {
			fmt.Fprintf(stdout, "%s %s\n", at, oneline.Escape(r.line(false)))
		}
	}
	a.promoteOnTick(ctx, stdout)
}
