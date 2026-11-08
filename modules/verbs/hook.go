package verbs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// runHook is `hook` (Stella's rulings, 2026-11-06): the name on the bus
// first; the sprint mints the session; one hook per name and one
// coordinator per sprint; a worker only once the sprint has a commit
// identity; the identity's two git config lines and where the role's
// playbook is printed (help playbook: the hook is run after every
// wrap-around, and the playbook is read once). A worker's restart
// is the bus hook, its own unhook, this hook, then next.
func runHook(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	r := in.str("role")
	if r != "worker" && r != "coordinator" {
		return "", misuse("hook", "--role is worker or coordinator")
	}
	if err := e.onBus(ctx, in.actor); err != nil {
		return "", err
	}
	snap, err := e.snapshot(ctx) // the store credential finish and read need: without it, CredentialFix
	if err != nil {
		return "", err
	}
	if r == "worker" && !snap.Identity.Set() { // nothing commits until the sprint knows as whom
		return "", noIdentity()
	}
	if err := e.originsReadable(ctx, snap); err != nil {
		return "", err
	}
	return e.hookAs(ctx, snap, in.actor, r)
}

// originsReadable is hook's admission (intent rule 34): the hooking
// process has what finish and read will need. The store credential is
// proved by the read before this; finish also reads the card's head and
// the group branch at the stream's origin, so every stream's repository
// must be readable from this process, all read at once. A held stream
// deals nothing, so its origin is not read. No forge token is asked of a
// row: the run does the forge work.
func (e *Engine) originsReadable(ctx context.Context, snap store.Snapshot) error {
	ss := slices.DeleteFunc(streamsInOrder(snap), func(s store.Stream) bool { return s.Held })
	errs := make([]error, len(ss))
	var wg sync.WaitGroup
	for i, s := range ss {
		wg.Go(func() { _, errs[i] = e.baseTip(ctx, s, "hook again") })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			f := *classify(err)
			f.What = "finish and read could not reach stream " + ss[i].Name + " from this process: " + f.What
			return &f
		}
	}
	return nil
}

// hookAs records name's session and role on snap and prints the sprint's
// commit identity (the two git config lines) and the command that prints
// the role's playbook: the hook verb's, and up's and restore's for their
// coordinator.
func (e *Engine) hookAs(ctx context.Context, snap store.Snapshot, name, r string) (string, error) {
	if row := snap.Rows[name]; row.Session != "" {
		return "", refuse(name+" is already hooked in as "+row.Role, "nova-sprint unhook --as "+name+", then hook again")
	}
	if c := coordinatorOf(snap); r == "coordinator" && c != "" {
		return "", refuse("the sprint's coordinator is "+c, "ask "+c+" to run nova-sprint unhook --as "+c+" first")
	}
	session, err := e.newSession()
	if err != nil {
		return "", err
	}
	op := e.newOp("hook", name, snap, session, r)
	op.Writes = []store.Write{store.PutRow{Name: name, Session: session, Role: r}}
	if r == "coordinator" { // one coordinator: no other hook may land between the read and this op
		op.Expect = []store.Expect{store.AtSeq{Seq: snap.Seq}}
	}
	if _, err := e.commit(ctx, op, "nova-sprint unhook --as "+name+", then hook again"); err != nil {
		return "", err
	}
	return done("nova-sprint next --as "+name, fmt.Sprintf("HOOKED %s role=%s", name, r), identityLines(snap), playbookLine(r)), nil
}

// playbookLine is where role's playbook is: read it once, not on every hook.
func playbookLine(role string) string {
	return "playbook: nova-sprint help playbook " + role + " (read it once; every output names its next command)"
}

func (e *Engine) newSession() (string, error) {
	if e.Session != nil {
		return e.Session()
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", RefusalOr(err, broken("no randomness for a session ("+errText(err)+")", "run nova-sprint hook again"))
	}
	return hex.EncodeToString(b[:]), nil
}

// runUnhook is `unhook`: one's own hook needs nothing (a worker's restart
// starts with it); another row's is the coordinator's.
func runUnhook(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	target := in.arg(0)
	if target == "" || target == in.actor {
		if in.actor == "" {
			return "", refuse("no name given", "pass --as <you>, or set NOVA_SPRINT_AS=<you>")
		}
		snap, err := e.snapshot(ctx)
		if err != nil {
			return "", err
		}
		return e.unhook(ctx, snap, in.actor, in.actor)
	}
	snap, err := e.hooked(ctx, in, roleCoordinator)
	if err != nil {
		return "", err
	}
	return e.unhook(ctx, snap, target, in.actor)
}

func (e *Engine) unhook(ctx context.Context, snap store.Snapshot, target, actor string) (string, error) {
	if snap.Rows[target].Session == "" && target == actor { // a restart's unhook: already true
		return done("nova-sprint hook --as "+target+" --role worker", target+" is not hooked in; nothing to unhook"), nil
	}
	if snap.Rows[target].Session == "" {
		return "", refuse(target+" is not hooked in", "nova-sprint show "+target)
	}
	op := e.newOp("unhook", actor, snap, target)
	op.Expect = []store.Expect{store.RowGen{Row: target, Gen: snap.Rows[target].Gen}}
	op.Writes = []store.Write{store.PutRow{Name: target}}
	if _, err := e.commit(ctx, op, "nova-sprint show "+target); err != nil {
		return "", err
	}
	return done("nova-sprint hook --as "+target+" --role worker", "UNHOOKED "+target), nil
}

// runWake is `wake`, nova-bus hook's --exec (Stella's and Johnny's ask,
// 2026-11-07): the hook takes, the friend works. Each deal line of a
// message from sprint that names this row is taken here, before the session
// wakes, and replaced by take's own output (TAKEN with the brief, or the
// refusal); every other line, and every other message, passes unchanged.
// It never fails a message: nova-bus would end the hook on a non-zero exit.
func runWake(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	if in.actor == "" {
		return "", misuse("wake", "pass --as <you>")
	}
	raw, err := io.ReadAll(e.Stdin)
	if err != nil {
		return "", RefusalOr(err, broken("stdin unreadable ("+clip(err.Error())+")", "nova-bus help hook"))
	}
	line := strings.TrimSuffix(string(raw), "\n")
	f := strings.SplitN(line, "\t", 5)
	if len(f) != 5 || f[1] != "sprint" {
		return line + "\n", nil
	}
	body := strings.Split(busUnescape.Replace(f[4]), "\n")
	for i, l := range body {
		if ref, epoch, ok := takeLine(l, in.actor); ok {
			body[i] = strings.TrimSuffix(e.Execute(ctx, []string{"take", ref, "--epoch", epoch, "--as", in.actor}).Output, "\n")
		}
	}
	f[4] = busEscape.Replace(strings.Join(body, "\n"))
	return strings.Join(f, "\t") + "\n", nil
}

// takeLine is the take a sprint line tells row to run ("run nova-sprint
// take <ref> --epoch <e> --as <row>" in a DEAL, "run: nova-sprint take <ref>
// --as <row> --epoch <e>" in a NUDGE, either flag order): its ref and epoch.
func takeLine(l, row string) (ref, epoch string, ok bool) {
	_, cmd, found := strings.Cut(l, "nova-sprint take ")
	w := strings.Fields(cmd)
	if !found || len(w) < 5 {
		return "", "", false
	}
	flags := map[string]string{w[1]: w[2], w[3]: w[4]}
	if flags["--as"] != row || flags["--epoch"] == "" {
		return "", "", false
	}
	return w[0], flags["--epoch"], true
}

// nova-bus's line escapes, both ways.
var (
	busEscape   = strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	busUnescape = strings.NewReplacer(`\\`, `\`, `\n`, "\n", `\r`, "\r", `\t`, "\t")
)
