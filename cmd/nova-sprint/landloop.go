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
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// The server lands what the readers passed (run --land): the last of the sprint's
// writers outside the server was a coordinator's `land` run beside it, racing the
// tick's accepts for the merge queue: cards queue up and landings are refused
// round after round while both write. Here one landing runs
// at a time, in the server's own process: land's reads and its report take the server's
// line of control (cmdLand, a.serial), its git runs outside it, and nothing else writes
// the merge queue between them but the server itself.

// LandEvery is how often the loop looks for cards queued to merge while a landing is in
// flight or a refusal stands, and the least time between two looks.
const LandEvery = 2 * time.Second

// LandIdleEvery is the idle loop's backstop: with nothing queued, in flight or refused, the
// loop waits on the sprint's log (a card queued to merge is a table move, and every move is
// a line on it) and looks again when a line comes, or at the latest this long after its last
// look. On 2026-10-17 the loop looked every LandEvery whatever was queued: each look took the
// server's line for the merge queue's read, ~43,200 a day with nothing queued, and wrote
// LAND IDLE queued=0 (422 of 1,647 log lines in 15 minutes).
const LandIdleEvery = 30 * time.Second

// landIdleLook bounds one wait on the log: the idle loop's wait is cut in slices this long,
// never one long block on the store.
const landIdleLook = 5 * time.Second

// landIdleSaid is how often LAND IDLE queued=0 is said while nothing changes: once when
// the loop goes idle, then once a minute.
const landIdleSaid = time.Minute

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

// landLoop runs one land cycle every LandEvery until ctx ends, and, while nothing is
// queued, in flight or refused, one each time a line comes on the sprint's log, at most
// every LandEvery and at least every LandIdleEvery (landWait). Each cycle writes one line:
// LAND OK, LAND REFUSED, or LAND IDLE (queued=0 only as the loop goes idle and once a
// minute). A landing runs beside the loop,
// so a long gate still leaves a line. A refusal is a state, not an event: while one
// stands every cycle writes LAND REFUSED naming its reason and its remedy, and its
// first cycle raises one judgment at once (landRefusal; tla/ServerSwitch.tla
// LandRefused, LandOk).
func (a *app) landLoop(ctx context.Context, addr string, stdout io.Writer) {
	fmt.Fprintf(stdout, "LANDING every %s: land runs here for every stream with cards queued to merge, one landing at a time\n", LandEvery)
	var flight *landFlight
	cursor := "" // the idle wait's place on the log (landWait)
	for ctx.Err() == nil {
		began := a.now()
		flight = a.landCycle(ctx, addr, stdout, flight)
		if ctx.Err() != nil {
			break
		}
		if flight == nil && a.landQuiet() {
			cursor = a.landWait(ctx, addr, cursor, began)
			continue
		}
		a.landPause(ctx)
	}
}

// landQuiet says the last cycle read the merge queue and found nothing queued, and no
// refusal stands and no close of one is owed: the loop then waits on the log (landWait).
// A landing in flight, a standing refusal, an owed close and an unreadable queue keep the
// LandEvery beat: each is said, retried or watched every beat.
func (a *app) landQuiet() bool {
	b := a.landState()
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.quiet && b.refusal == nil && !b.closing
}

// landWait is the idle loop's wait: it blocks on the sprint's log from cursor, in slices
// of landIdleLook, until a line after it comes or LandIdleEvery has passed (by its slices),
// then waits out what is left of LandEvery since the cycle began, so a busy log is looked
// at no more often than before. It holds nothing while it waits, never the server's line.
// It returns the cursor for the next wait: every line up to it was committed before the
// wait returned, so before the next look reads the queue, and a line after it wakes the
// next wait at once. The first wait ("" is before every line) wakes at once. A store that
// cannot be opened or waited on falls back to the LandEvery pause.
func (a *app) landWait(ctx context.Context, addr, cursor string, began time.Time) string {
	st, err := a.storeCtx(ctx, common{verb: "where", redis: addr})
	if err != nil {
		a.landPause(ctx)
		return cursor
	}
	epoch := st.PinnedEpoch()
	for range int(LandIdleEvery / landIdleLook) {
		if ctx.Err() != nil {
			return cursor
		}
		tail, woke, err := st.WaitLog(ctx, epoch, cursor, landIdleLook)
		if err != nil {
			if ctx.Err() == nil {
				a.landPause(ctx)
			}
			return cursor
		}
		cursor = tail
		if woke {
			break
		}
	}
	if left := LandEvery - a.now().Sub(began); left > 0 && ctx.Err() == nil {
		a.landSleep(ctx, left)
	}
	return cursor
}

// landPause waits LandEvery between land cycles on the app's own sleep, and returns at
// once when ctx ends: a returning run stops an idle land loop immediately instead of
// waiting out its LandStopBound (start-refuses-a-non-working-setting, cold read 1).
func (a *app) landPause(ctx context.Context) { a.landSleep(ctx, LandEvery) }

// landSleep waits d on the app's own sleep, and returns at once when ctx ends. The sleep
// runs in its own goroutine, which ends when the sleep does.
func (a *app) landSleep(ctx context.Context, d time.Duration) {
	slept := make(chan struct{})
	go func() {
		defer close(slept)
		a.sleep(d)
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
		lines, code = []string{"LAND FAILED code=queue-unread the merge queue could not be read: " + oneline.Err(err) + "; nothing was landed, and the next round tries again; run: nova-sprint where"}, 2
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
		if r := a.standingRefusal(); r != nil {
			a.writeRefused(stdout, "?", r) // a wait is no pass: the refusal still stands
		}
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
	if err == nil {
		a.clearQueueRefusal() // the queue was read: a queue cause is gone
	}
	switch {
	case code != 0 && !landBatched(lines):
		// nothing could land: a refusal before any batch (a setting such as the
		// git_identity, the epoch, the fence), a pass that failed, or a queue that could
		// not be read. It stands until a pass gets past it or it changes: its lines are
		// said when it begins, the beat names it every cycle (writeRefused), and its
		// judgment is single (tla/ServerSwitch.tla LandRefused)
		if !a.standRefusal(said, coordinator, code) {
			// the same cause: its judgment and since stand, and its words are not said
			// again in full (the beat names it, in its latest words); only a queue
			// outage's error is said again when it changes, being all that tells two
			// outages apart
			if said == a.landFailed || !strings.HasPrefix(said, "LAND FAILED ") {
				a.landFailed = said
				return code, idle
			}
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
// first stream that has one. err when the sprint could not be read. The merge table's
// row counts are read first (one exchange): when every queued cell is read and empty,
// nothing is queued and neither the coordinator nor the table's cards are read (the
// coordinator is "": nothing is landed); else the table is loaded and counted as before.
func (a *app) mergeQueued(ctx context.Context, addr string) (n int, coordinator, stream string, err error) {
	st, err := a.storeCtx(ctx, common{verb: "where", redis: addr})
	if err != nil {
		return 0, "", "", err
	}
	if mergeNoneQueued(ctx, st) {
		return 0, "", "", nil
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

// mergeNoneQueued says the merge table's shape, read in one exchange, has every row's
// queued cell read and empty. Any doubt (a read that fails, a cell not read, no queued
// column) is false, and the caller loads the table.
func mergeNoneQueued(ctx context.Context, st *store.Store) bool {
	shapes, err := st.B.Shapes(ctx, []string{st.Names.Table(sprint.Merge)})
	if err != nil || len(shapes) != 1 {
		return false
	}
	t := shapes[0]
	j := t.Column(sprint.Queued)
	if j < 0 {
		return false
	}
	for _, r := range t.Rows {
		if j >= len(r.Cells) || r.Cells[j].Unread || r.Cells[j].Count != 0 {
			return false
		}
	}
	return true
}

// landRefusal is the lander's standing refusal (tla/ServerSwitch.tla refusal and
// judged): the reason and the remedy read off land's refusal line, its key (the
// reason normalised, sprint.LandRefusalKey: what a refusal is the same as), since when
// it has stood, the text of the refusal it replaced (whose judgment it closes), the
// coordinator its judgment is addressed by, and whether that judgment is written.
type landRefusal struct {
	kind                string // causeRefused, causeFailed or causeQueue
	code, stream        string // the class at the source (landClass), "" for a line with none
	reason, remedy, key string
	prev, prevType      string
	since               time.Time
	coord               string
	judged              bool
}

// The kinds of cause a refusal stands for (tla/ServerSwitch.tla Causes): land refused
// before any batch (a REFUSED: line), land failed before any batch (it said no
// REFUSED:, or nothing), or the loop could not read the merge queue. The failures are
// judged as sprint.NLanderFails and their beat line is LAND FAILED, so a store that
// blinked is never worded as a refusal.
const (
	causeRefused = "refused"
	causeFailed  = "failed"
	causeQueue   = "queue"
)

// landClass marks a refusal before any batch with its class at the source: the
// machine-readable code=<class> the land loop keys a standing cause on (with the stream,
// where one applies), never the human text after it, whose counts, timings, hosts and
// paths change pass to pass (landRefusalOf, sprint.LandRefusalKey). The classes:
// usage, store-open, store-read, epoch, fenced, git-identity; the loop's own
// queue-unread, and fault for a land that said nothing.
func landClass(class, why string) string { return "code=" + class + " " + why }

// landClassErr is landClass on an error, keeping its chain (a refusal stays one).
func landClassErr(class string, err error) error { return fmt.Errorf("code=%s %w", class, err) }

// landQueueUnread is the queue cause's stable reason: the error's words vary read to
// read and are said in the LAND FAILED line, never in the cause's key.
const landQueueUnread = "the merge queue could not be read"

// what is the refusal's judgment text: its class (and stream) when it has one, its
// reason and its remedy (sprint.LandRefusalKey reads the class back off it).
func (r landRefusal) what() string {
	if r.code == "" {
		return r.reason + "; run: " + r.remedy
	}
	head := "code=" + r.code
	if r.stream != "" {
		head += " stream=" + r.stream
	}
	return head + ": " + r.reason + "; run: " + r.remedy
}

// typ is the refusal's judgment type: a refusal, or a failure.
func (r landRefusal) typ() string {
	if r.kind == causeRefused {
		return sprint.NLanderRefuses
	}
	return sprint.NLanderFails
}

// word is the refusal's beat-line word: REFUSED, or FAILED.
func (r landRefusal) word() string {
	if r.kind == causeRefused {
		return "REFUSED"
	}
	return "FAILED"
}

// author is who the judgment is by: the coordinator the pass ran as, else (a queue
// that could not be read names none) the machine.
func (r landRefusal) author() string {
	if r.coord != "" {
		return r.coord
	}
	return sprint.MachineActor
}

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
func landRefusalOf(said string, code int) (kind, reason, remedy string) {
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
	kind, reason = causeFailed, first
	switch {
	case refused != "":
		kind, reason = causeRefused, refused
	case failed != "":
		kind, reason = causeQueue, failed
	case landSaid != "":
		reason = landSaid
	}
	if reason == "" {
		reason = fmt.Sprintf("land exited %d and said nothing", code)
	}
	if i := strings.LastIndex(reason, run); i >= 0 {
		remedy = strings.TrimSpace(reason[i+len(run):])
		reason = reason[:i]
	}
	if remedy == "" {
		remedy = landRefusalRemedy
	}
	return kind, strings.TrimSpace(reason), remedy
}

// landCodeHead is a refusal's class at its head, and the stream after it: code=<class>
// [stream=<s>] (landClass).
var landCodeHead = regexp.MustCompile(`^code=(\S+)(?:\s+stream=(\S+))?\s*`)

// landCodeOf splits a reason into its class, its stream and its human text.
func landCodeOf(reason string) (code, stream, text string) {
	m := landCodeHead.FindStringSubmatch(reason)
	if m == nil {
		return "", "", reason
	}
	return m[1], m[2], strings.TrimSpace(reason[len(m[0]):])
}

// standRefusal records a refusal before any batch, what land said. A refusal with the
// same key as the one that stands is the same refusal (its since and its judgment
// kept), whatever counts, timings or ids its text carries; another replaces it, to be
// judged anew (tla/ServerSwitch.tla LandRefused). changed says it replaced one.
func (a *app) standRefusal(said, coord string, code int) (changed bool) {
	kind, reason, remedy := landRefusalOf(said, code)
	class, stream, reason := landCodeOf(reason)
	switch {
	case kind == causeQueue:
		class, reason = "queue-unread", landQueueUnread // the error's words are in the LAND FAILED line
	case class == "" && strings.HasPrefix(reason, "land exited "):
		class = "fault"
	}
	// keyed on the class at the source and its stream; a line with no class (none
	// land prints today, but a new one could) falls back to its folded text
	key := kind + ": " + sprint.LandRefusalKey(landRefusal{code: class, stream: stream, reason: reason, remedy: remedy}.what())
	b := a.landState()
	b.mu.Lock()
	defer b.mu.Unlock()
	if r := b.refusal; r != nil && r.key == key {
		if r.coord == "" {
			r.coord = coord
		}
		r.reason, r.remedy = reason, remedy // the display follows the latest wording
		return false
	}
	prev, prevType := "", ""
	if b.refusal != nil && b.refusal.judged {
		prev, prevType = b.refusal.what(), b.refusal.typ()
	}
	b.refusal = &landRefusal{kind: kind, code: class, stream: stream, reason: reason, remedy: remedy, key: key, prev: prev, prevType: prevType, since: a.now(), coord: coord}
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
		b.closeGen++
	}
	b.refusal = nil
}

// clearQueueRefusal ends a standing queue cause once the queue is read: its cause is
// gone (tla/ServerSwitch.tla NoRun). Any other cause stands until a pass gets past it.
func (a *app) clearQueueRefusal() {
	b := a.landState()
	b.mu.Lock()
	queue := b.refusal != nil && b.refusal.kind == causeQueue
	b.mu.Unlock()
	if queue {
		a.clearRefusal()
	}
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
	closing, closeAs, gen := b.closing, b.closeAs, b.closeGen
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
	if closing {
		// an owed close is written before any raise: a cause that cleared and came back
		// before its close was written would otherwise meet its own stale
		// acknowledgement and stay silent (LandRefusalRaise writes nothing for a cause
		// acknowledged). Until the close lands, nothing is raised (tla/ServerSwitch.tla
		// closeOwed, CloseWrite; the witness "staleack"). The owe is this server's
		// memory; its durable form is the acknowledgement the store still holds, which a
		// restarted server's first raise of the cause closes in the same step as the
		// new judgment (sprint.LandRefusalRaise; the witness "restartack").
		if closeAs == "" {
			closeAs = sprint.MachineActor
		}
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
		if b.closeGen == gen {
			b.closing, b.swept, b.closeAs = false, true, ""
		}
		b.mu.Unlock()
		if r == nil {
			return
		}
	}
	st, err := a.storeCtx(ctx, common{verb: "run", redis: addr, actor: standing.author()})
	if err != nil {
		a.judgeNotWritten(w, "the store could not be opened: "+oneline.Err(err))
		return
	}
	what, typ := standing.what(), standing.typ()
	if standing.judged {
		open, err := st.B.OpenNotes(ctx)
		if err != nil {
			a.judgeNotWritten(w, "its judgment could not be checked against the store: "+oneline.Err(err))
			return // tried again next beat; the judgment stands
		}
		if sprint.LandRefusalSaid(open, typ, what) {
			a.judgeWritten()
			return
		}
	}
	res, err := st.Run(ctx, store.Step{Verb: "land", Plan: func(s *sprint.Snapshot) sprint.Plan {
		return sprint.LandRefusalRaise(s, typ, what, standing.author(), standing.prevType, standing.prev)
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
	// closeGen counts the clears (clearRefusal): a close written for one generation
	// leaves a later clear's close owed. closing is memory and a restart loses it; what
	// it owed stays in the store as the lander's acknowledgement, which the next
	// server never reads as said on a first raise (sprint.LandRefusalRaise).
	closeGen int
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
	// quiet says the last cycle read an empty merge queue (landQuiet); idleAt is when it
	// last said LAND IDLE queued=0, zero once a cycle has said anything else.
	quiet  bool
	idleAt time.Time
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
// not also an IDLE line. Nothing queued writes LAND IDLE queued=0 on the first such
// cycle and once a landIdleSaid after, while nothing else is said between.
func (a *app) landCycle(ctx context.Context, addr string, stdout io.Writer, flight *landFlight) *landFlight {
	a.landPulse()
	// the cycle's own memory: quiet until it reads an empty queue, and when it last said
	// LAND IDLE queued=0, kept only by a cycle that says it or passes it over
	b := a.landState()
	b.mu.Lock()
	idleAt := b.idleAt
	b.quiet, b.idleAt = false, time.Time{}
	b.mu.Unlock()
	if flight != nil {
		flight.mu.Lock()
		done, idle := flight.done, flight.idle
		flight.mu.Unlock()
		if !done {
			a.writeBeat(stdout, flight)
			a.raiseIfStuckTo(ctx, addr, flight, stdout)
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
	a.clearQueueRefusal() // the queue was read: a queue cause is gone
	if n == 0 || coord == "" {
		// nothing ran, so nothing else is cleared: a standing refusal is still named,
		// with cards queued or none (tla/ServerSwitch.tla NoRun, BeatNamesTheRefusal)
		if r := a.standingRefusal(); r != nil {
			a.writeRefused(stdout, strconv.Itoa(n), r)
		} else if n > 0 {
			a.writeIdle(stdout, n, "-", 0, "no-coordinator")
		} else {
			// nothing queued is said when the loop goes idle and once a minute while it
			// stays so, never every look
			if now := a.now(); idleAt.IsZero() || now.Sub(idleAt) >= landIdleSaid || now.Before(idleAt) {
				a.writeIdle(stdout, 0, "-", 0, "")
				idleAt = now
			}
			b.mu.Lock()
			b.quiet, b.idleAt = true, idleAt
			b.mu.Unlock()
		}
		a.judgeRefusal(ctx, addr, stdout) // a cleared queue cause's judgment is closed
		a.landAfter(ctx, n == 0, stdout)
		return nil
	}
	f := &landFlight{queued: n, step: "land", proc: "nova-sprint land", stream: stream, coord: coord, began: a.now()}
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
	class := ""
	if r.code != "" {
		class = "code=" + r.code + " "
		if r.stream != "" {
			class += "stream=" + r.stream + " "
		}
	}
	a.writeCycle(w, fmt.Sprintf("LAND %s queued=%s since=%s %sreason=%s run: %s", r.word(), queued, age.Round(time.Second), class, r.reason, r.remedy))
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
	a.raiseIfStuckTo(ctx, addr, f, io.Discard)
}

// raiseIfStuckTo is raiseIfStuck saying, once, as LAND JUDGMENT NOT WRITTEN, a
// stuck judgment the store did not take.
func (a *app) raiseIfStuckTo(ctx context.Context, addr string, f *landFlight, w io.Writer) {
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
	if err != nil { // tried again next cycle
		a.judgeNotWritten(w, "the stuck landing's judgment: the store could not be opened: "+oneline.Err(err))
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
	if why := stepNotWritten(res, err); why != "" {
		a.judgeNotWritten(w, "the stuck landing's judgment: "+why)
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
