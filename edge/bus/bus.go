// Package bus is the nova-bus edge of layer 4 (docs/LAYER-4-EDGES.md).
package bus

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	DefaultSender, DefaultCoordinator = "sprint", "rowan"
	DefaultTimeout                    = 5 * time.Second
	MaxMessageBytes, MaxCardLines     = 4096, 20
	MaxFieldBytes                     = 300
	CursorPrefix                      = "push:"
	SubjectDeal, SubjectWithdrawn     = "DEAL", "WITHDRAWN"
	SubjectNeedsYou, SubjectLanded    = "NEEDS YOU", "LANDED"
	SubjectStreamLanded               = "STREAM LANDED"
)

type (
	Bus interface {
		Send(ctx context.Context, to, subject, body string) (msgID string, err error)
		Who(ctx context.Context) (map[string]Presence, error)
	}
	Presence struct{ Name, Uptime, Detail string }
	Message  struct {
		To, Subject, Body string
		MinSeq, MaxSeq    int64
		Epoch             int
	}
	Delivered struct {
		Message   Message
		MsgID     string
		CursorSeq int64
	}
	Config struct {
		Exec                     edge.Exec
		Transport                Bus
		Bin, Sender, Coordinator string
		Timeout                  time.Duration
		Env                      []string
		AllowNew, RequireHooked  bool
		Window                   int
	}
	Edge struct {
		exec                     edge.Exec
		transport                Bus
		bin, sender, coordinator string
		timeout                  time.Duration
		env                      []string
		allowNew, requireHooked  bool
		window                   int
	}
	subjectSpec_internal struct {
		subject   string
		coordOnly bool
		collect   func(snap store.Snapshot, events []store.Event, recipient string) (string, []string, int64, int64, int)
	}
	judgementGroup_internal struct {
		key   string
		items []store.Judgement
	}
)

var subjectTable_internal = []subjectSpec_internal{
	{SubjectDeal, false, collectDeals_internal},
	{SubjectWithdrawn, false, collectWithdrawn_internal},
	{SubjectNeedsYou, true, collectNeedsYou_internal},
	{SubjectLanded, true, collectLanded_internal},
	{SubjectStreamLanded, true, collectStreamLanded_internal},
}

func New(cfg Config) *Edge {
	e := &Edge{
		exec: cmp.Or[edge.Exec](cfg.Exec, edge.NewOSExec()), bin: cmp.Or(cfg.Bin, "nova-bus"),
		sender: cmp.Or(cfg.Sender, DefaultSender), coordinator: cmp.Or(cfg.Coordinator, DefaultCoordinator),
		timeout: cmp.Or(cfg.Timeout, DefaultTimeout), env: slices.Clone(cfg.Env),
		allowNew: cfg.AllowNew, requireHooked: cfg.RequireHooked, window: cmp.Or(cfg.Window, store.MaxEvents),
	}
	e.transport = cmp.Or[Bus](cfg.Transport, e)
	return e
}
func CursorName(recipient string) string { return CursorPrefix + recipient }
func BusReply(sender string) string {
	return "replies are verbs: run nova-sprint next --as " + cmp.Or(strings.TrimSpace(sender), "<row>")
}
func TruncateField(text, id string) string {
	text = strings.TrimSpace(strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(text))
	if len(text) <= MaxFieldBytes {
		return text
	}
	cut := text[:MaxFieldBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "... (full text: nova-sprint show " + id + ")"
}
func (e *Edge) Send(ctx context.Context, to, subject, body string) (string, error) {
	switch {
	case strings.TrimSpace(to) == "" || strings.TrimSpace(subject) == "":
		return "", fmt.Errorf("bus send: recipient and subject are required; pass --to <name> and a non-empty subject")
	case len(body) > MaxMessageBytes:
		return "", fmt.Errorf("bus send to %s (%s): body is %d bytes, exceeding the %d-byte cap; truncate before sending", to, subject, len(body), MaxMessageBytes)
	}
	argv := []string{e.bin, "send", "--as", e.sender, "--to", to}
	if e.allowNew {
		argv = append(argv, "--new")
	}
	res, err := e.exec.Run(ctx, edge.Cmd{Argv: append(argv, subject, body), Env: e.env, Timeout: e.timeout})
	if err != nil {
		return "", fmt.Errorf("bus send to %s (%s): %w", to, subject, err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("bus send to %s (%s): nova-bus exited %d (%s); verify %s is hooked in (`nova-bus who`)", to, subject, res.ExitCode, strings.TrimSpace(cmp.Or(res.Stderr, res.Stdout)), e.sender)
	}
	if msgID, ok := parseSendConfirmation_internal(res.Stdout, to); ok {
		return msgID, nil
	}
	return "", fmt.Errorf("bus send to %s (%s): missing `<recipient> <id>` confirmation in stdout %q; leaving cursor unmoved", to, subject, strings.TrimSpace(res.Stdout))
}
func parseSendConfirmation_internal(stdout, to string) (string, bool) {
	for _, line := range strings.Split(stdout, "\n") {
		if fields := strings.Fields(strings.TrimSpace(line)); len(fields) == 2 && fields[0] == to && fields[1] != "" {
			return fields[1], true
		}
	}
	return "", false
}
func (e *Edge) Who(ctx context.Context) (map[string]Presence, error) {
	res, err := edge.RunArgv(ctx, e.exec, e.timeout, "", e.env, e.bin, "who")
	if err != nil {
		return nil, fmt.Errorf("bus who: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("bus who: nova-bus exited %d (%s); check NOVA_BUS_REDIS and daemon status", res.ExitCode, strings.TrimSpace(cmp.Or(res.Stderr, res.Stdout)))
	}
	return parseWhoOutput_internal(res.Stdout)
}
func parseWhoOutput_internal(stdout string) (map[string]Presence, error) {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "WHO OK") {
		return nil, fmt.Errorf("bus who: unexpected header %q; expected `WHO OK present=<n>`", strings.TrimSpace(stdout))
	}
	out := make(map[string]Presence)
	for _, line := range lines[1:] {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 || parts[0] == "" {
			return nil, fmt.Errorf("bus who: malformed presence line %q", line)
		}
		p := Presence{Name: parts[0], Uptime: parts[1]}
		if len(parts) == 3 {
			p.Detail = parts[2]
		}
		out[p.Name] = p
	}
	return out, nil
}
func (e *Edge) BuildRecipientMessages(snap store.Snapshot, events []store.Event, recipient string) []Message {
	cursor := snap.Cursors[CursorName(recipient)]
	var accepted []store.Event
	for _, ev := range events {
		if ev.Accepted && ev.After != nil && ev.Seq > cursor {
			accepted = append(accepted, ev)
		}
	}
	if len(accepted) == 0 {
		return nil
	}
	trailer := fmt.Sprintf("and %%d more: nova-sprint next --as %s", recipient)
	var out []Message
	for _, spec := range subjectTable_internal {
		if spec.coordOnly && recipient != e.coordinator {
			continue
		}
		if header, entries, minSeq, maxSeq, epoch := spec.collect(snap, accepted, recipient); len(entries) > 0 {
			out = append(out, Message{To: recipient, Subject: spec.subject, Body: packEntries_internal(header, entries, trailer), MinSeq: minSeq, MaxSeq: maxSeq, Epoch: epoch})
		}
	}
	return out
}
func scanCards_internal(snap store.Snapshot, events []store.Event, match func(ev store.Event, c store.Card) (string, bool)) ([]string, int64, int64, int) {
	var entries []string
	var minSeq, maxSeq int64
	seen := make(map[string]bool)
	epoch := snap.Epoch
	for _, ev := range events {
		for _, id := range slices.Sorted(maps.Keys(ev.After.Cards)) {
			if !seen[id] {
				if entry, ok := match(ev, ev.After.Cards[id]); ok {
					seen[id] = true
					entries = append(entries, entry)
					minSeq, maxSeq = updateSeqBounds_internal(minSeq, maxSeq, ev.Seq)
					epoch = cmp.Or(ev.Epoch, epoch)
				}
			}
		}
	}
	return entries, minSeq, maxSeq, epoch
}
func collectDeals_internal(snap store.Snapshot, events []store.Event, recipient string) (string, []string, int64, int64, int) {
	entries, minSeq, maxSeq, epoch := scanCards_internal(snap, events, func(ev store.Event, c store.Card) (string, bool) {
		if (c.Kind != "work" && c.Kind != "read") || c.Holder != recipient || c.State != "ready" || (ev.Before != nil && ev.Before.Cards[c.ID].State == "ready") {
			return "", false
		}
		lines := []string{fmt.Sprintf("card=%s attempt=%d kind=%s stream=%s tier=%s", c.ID, c.Attempt, c.Kind, c.Stream, c.Tier)}
		if c.Kind == "work" && (c.Prior != "" || c.Attempt > 1 || c.Finding != "") {
			var p store.Card
			if ev.After != nil {
				p = ev.After.Cards[c.Of]
			}
			why := cmp.Or(c.Finding, c.Reason, p.Finding, p.Reason, snap.Cards[c.Of].Finding, snap.Cards[c.Of].Reason)
			lines = append(lines, fmt.Sprintf("prior=%s reason=%q", c.Prior, TruncateField(why, c.ID)))
		}
		if c.Kind == "read" && c.Head != "" {
			lines = append(lines, fmt.Sprintf("kind=read head=%s", c.Head))
		}
		return strings.Join(append(lines, fmt.Sprintf("run: nova-sprint take %s@%d --as %s", c.ID, c.Gen, recipient)), "\n"), true
	})
	word := "cards"
	if len(entries) == 1 {
		word = "card"
	}
	return fmt.Sprintf("DEAL: %d %s for %s (epoch %d)", len(entries), word, recipient, epoch), entries, minSeq, maxSeq, epoch
}
func collectWithdrawn_internal(snap store.Snapshot, events []store.Event, recipient string) (string, []string, int64, int64, int) {
	entries, minSeq, maxSeq, epoch := scanCards_internal(snap, events, func(ev store.Event, c store.Card) (string, bool) {
		live := c.Result == ""
		if ev.Before != nil {
			if b, ok := ev.Before.Cards[c.ID]; ok {
				live = b.State == "ready" || b.State == "working"
			}
		}
		if (c.Kind != "work" && c.Kind != "read") || c.Holder != recipient || c.State != "fail" || !live {
			return "", false
		}
		return fmt.Sprintf("card=%s of=%s branch=%s reason=%q\nstop; push committed work", c.ID, c.Of, c.Branch, TruncateField(c.Reason, c.ID)), true
	})
	return fmt.Sprintf("WITHDRAWN: %d card(s) for %s (epoch %d)", len(entries), recipient, epoch), entries, minSeq, maxSeq, epoch
}
func collectNeedsYou_internal(snap store.Snapshot, events []store.Event, _ string) (string, []string, int64, int64, int) {
	var groups []*judgementGroup_internal
	byKey := make(map[string]*judgementGroup_internal)
	var minSeq, maxSeq int64
	epoch := snap.Epoch
	for _, ev := range events {
		for _, id := range slices.Sorted(maps.Keys(ev.After.Judgements)) {
			j := ev.After.Judgements[id]
			if j.State != "open" || (ev.Before != nil && ev.Before.Judgements[id].State != "") {
				continue
			}
			k := cmp.Or(j.Group, j.ID)
			g, ok := byKey[k]
			if !ok {
				g = &judgementGroup_internal{key: k}
				byKey[k], groups = g, append(groups, g)
			}
			g.items = append(g.items, j)
			minSeq, maxSeq = updateSeqBounds_internal(minSeq, maxSeq, ev.Seq)
			epoch = cmp.Or(ev.Epoch, epoch)
		}
	}
	entries := make([]string, 0, len(groups))
	for _, g := range groups {
		entries = append(entries, formatJudgementGroup_internal(g))
	}
	return fmt.Sprintf("NEEDS YOU: %d group(s) (epoch %d)", len(groups), epoch), entries, minSeq, maxSeq, epoch
}
func formatJudgementGroup_internal(g *judgementGroup_internal) string {
	first := g.items[0]
	ids := make([]string, len(g.items))
	for i, item := range g.items {
		ids[i] = item.ID
	}
	due := "none"
	if !first.Deadline.IsZero() {
		due = first.Deadline.UTC().Format(time.RFC3339)
	}
	cmd := first.Command
	if !strings.HasPrefix(cmd, "nova-sprint judge ") {
		cmd = fmt.Sprintf("nova-sprint judge %s <choice> --reason \"<why>\"", first.ID)
	}
	head := fmt.Sprintf("NEEDS YOU judgement=%s group=%s n=%d size=%d ids=%s kind=%s about=%s due=%s", first.ID, g.key, len(g.items), len(g.items), strings.Join(ids, ","), first.Kind, first.About, due)
	return fmt.Sprintf("%s\nquestion: %q\nrun: %s", head, TruncateField(first.Question, first.ID), cmd)
}
func collectLanded_internal(snap store.Snapshot, events []store.Event, _ string) (string, []string, int64, int64, int) {
	entries, minSeq, maxSeq, epoch := scanCards_internal(snap, events, func(ev store.Event, c store.Card) (string, bool) {
		if (c.Kind != "producer" && c.Kind != "sentinel") || c.State != "landed" || (ev.Before != nil && ev.Before.Cards[c.ID].State == "landed") {
			return "", false
		}
		return fmt.Sprintf("LANDED card=%s stream=%s head=%s\nrun: nova-sprint show %s", c.ID, c.Stream, c.Head, c.ID), true
	})
	return fmt.Sprintf("LANDED: %d card(s) (epoch %d)", len(entries), epoch), entries, minSeq, maxSeq, epoch
}
func collectStreamLanded_internal(snap store.Snapshot, events []store.Event, _ string) (string, []string, int64, int64, int) {
	var streams, entries []string
	seenStream := make(map[string]bool)
	var minSeq, maxSeq int64
	epoch := snap.Epoch
	for _, ev := range events {
		for _, id := range slices.Sorted(maps.Keys(ev.After.Cards)) {
			if c := ev.After.Cards[id]; (c.Kind == "producer" || c.Kind == "sentinel") && c.State == "landed" && !seenStream[c.Stream] {
				seenStream[c.Stream], streams = true, append(streams, c.Stream)
				minSeq, maxSeq = updateSeqBounds_internal(minSeq, maxSeq, ev.Seq)
				epoch = cmp.Or(ev.Epoch, epoch)
			}
		}
	}
	slices.Sort(streams)
	for _, sName := range streams {
		if count, lastHead, allLanded := streamCompletion_internal(snap, sName); allLanded && count > 0 {
			entries = append(entries, fmt.Sprintf("STREAM LANDED stream=%s base=%s head=%s cards=%d\nrun: nova-sprint show %s --stitch", sName, snap.Streams[sName].Base, lastHead, count, sName))
		}
	}
	return fmt.Sprintf("STREAM LANDED: %d stream(s) (epoch %d)", len(entries), epoch), entries, minSeq, maxSeq, epoch
}
func streamCompletion_internal(snap store.Snapshot, streamName string) (int, string, bool) {
	count, lastHead := 0, ""
	for _, id := range slices.Sorted(maps.Keys(snap.Cards)) {
		if c := snap.Cards[id]; c.Stream == streamName && (c.Kind == "producer" || c.Kind == "sentinel") {
			if c.State != "landed" {
				return 0, "", false
			}
			count, lastHead = count+1, cmp.Or(c.Head, lastHead)
		}
	}
	return count, lastHead, count > 0
}
func packEntries_internal(header string, entries []string, trailerFmt string) string {
	var b strings.Builder
	b.WriteString(header)
	included := 0
	for i, entry := range entries {
		if included >= MaxCardLines {
			break
		}
		reserve := 0
		if rem := len(entries) - (i + 1); rem > 0 {
			reserve = 1 + len(fmt.Sprintf(trailerFmt, rem))
		}
		if b.Len()+1+len(entry)+reserve > MaxMessageBytes {
			break
		}
		b.WriteString("\n" + entry)
		included++
	}
	if rem := len(entries) - included; rem > 0 {
		if tail := "\n" + fmt.Sprintf(trailerFmt, rem); b.Len()+len(tail) <= MaxMessageBytes {
			b.WriteString(tail)
		}
	}
	return b.String()
}
func updateSeqBounds_internal(minSeq, maxSeq, seq int64) (int64, int64) {
	return min(cmp.Or(minSeq, seq), seq), max(maxSeq, seq)
}
func maxAcceptedSeq_internal(events []store.Event, curSeq int64) (int64, bool) {
	maxSeq, hasDomain := curSeq, false
	for _, ev := range events {
		maxSeq, hasDomain = max(maxSeq, ev.Seq), hasDomain || ev.Kind != "cursor"
	}
	return maxSeq, hasDomain
}
func (e *Edge) PushRecipient(ctx context.Context, st store.Store, snap store.Snapshot, recipient, opIDPrefix string, now time.Time) ([]Delivered, error) {
	var all []Delivered
	for {
		deliv, more, err := e.pushWindow_internal(ctx, st, &snap, recipient, opIDPrefix, now)
		all = append(all, deliv...)
		if err != nil || !more {
			return all, err
		}
	}
}
func (e *Edge) pushWindow_internal(ctx context.Context, st store.Store, snap *store.Snapshot, recipient, opIDPrefix string, now time.Time) ([]Delivered, bool, error) {
	cursorKey := CursorName(recipient)
	curSeq, win := snap.Cursors[cursorKey], cmp.Or(e.window, store.MaxEvents)
	events, err := st.Events(ctx, curSeq, win)
	if err != nil || len(events) == 0 {
		if err != nil {
			return nil, false, fmt.Errorf("bus push %s: read events after %d: %w", recipient, curSeq, err)
		}
		return nil, false, nil
	}
	windowMax, hasDomain := maxAcceptedSeq_internal(events, curSeq)
	snap.Cursors[cursorKey] = curSeq
	msgs := e.BuildRecipientMessages(*snap, events, recipient)
	hasLater := len(events) == win && (snap.Seq == 0 || windowMax < snap.Seq)
	deliv, nextSeq, sendErr := e.sendWindow_internal(ctx, msgs, curSeq, windowMax, hasDomain || hasLater)
	if nextSeq > curSeq {
		if advErr := advanceCursor_internal(ctx, st, snap.Epoch, recipient, nextSeq, opIDPrefix, now); advErr != nil {
			return deliv, false, advErr
		}
		snap.Cursors[cursorKey] = nextSeq
	}
	return deliv, sendErr == nil && hasLater, sendErr
}
func (e *Edge) sendWindow_internal(ctx context.Context, msgs []Message, curSeq, windowMax int64, advanceEmpty bool) ([]Delivered, int64, error) {
	if len(msgs) == 0 {
		if advanceEmpty {
			return nil, windowMax, nil
		}
		return nil, curSeq, nil
	}
	var delivered []Delivered
	for _, m := range msgs {
		msgID, sendErr := e.transport.Send(ctx, m.To, m.Subject, m.Body)
		if sendErr != nil {
			return delivered, curSeq, sendErr
		}
		delivered = append(delivered, Delivered{Message: m, MsgID: msgID, CursorSeq: windowMax})
	}
	return delivered, windowMax, nil
}
func advanceCursor_internal(ctx context.Context, st store.Store, epoch int, recipient string, seq int64, opIDPrefix string, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rec, err := st.Apply(ctx, store.Op{
		ID: fmt.Sprintf("%s-cursor-%s-%d", cmp.Or(opIDPrefix, "push"), recipient, seq), Epoch: cmp.Or(epoch, 1),
		Actor: DefaultSender, Kind: "cursor", At: now, Writes: []store.Write{store.SetCursor{Name: CursorName(recipient), Seq: seq}},
	})
	switch {
	case err != nil:
		return fmt.Errorf("bus push %s: advance cursor to %d: %w", recipient, seq, err)
	case !rec.Accepted:
		return fmt.Errorf("bus push %s: cursor write to %d refused: %s", recipient, seq, rec.Reason)
	default:
		return nil
	}
}
func (e *Edge) PushTick(ctx context.Context, st store.Store, opIDPrefix string, now time.Time) ([]Delivered, error) {
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("bus push tick: snapshot: %w", err)
	}
	recipients, seen := make([]string, 0, len(snap.Rows)+1), make(map[string]bool)
	for name := range snap.Rows {
		if isRecipientHooked_internal(snap, name, e.requireHooked) {
			recipients, seen[name] = append(recipients, name), true
		}
	}
	if e.coordinator != "" && !seen[e.coordinator] && isRecipientHooked_internal(snap, e.coordinator, e.requireHooked) {
		recipients = append(recipients, e.coordinator)
	}
	slices.Sort(recipients)
	var all []Delivered
	var errs []error
	for _, r := range recipients {
		deliv, pushErr := e.PushRecipient(ctx, st, snap, r, opIDPrefix, now)
		all, errs = append(all, deliv...), append(errs, pushErr)
	}
	return all, errors.Join(errs...)
}
func isRecipientHooked_internal(snap store.Snapshot, name string, requireHooked bool) bool {
	hasSessions := requireHooked
	for _, r := range snap.Rows {
		hasSessions = hasSessions || r.Session != ""
	}
	r, ok := snap.Rows[name]
	return (!ok && !requireHooked) || (ok && r.State == "up" && (!hasSessions || r.Session != ""))
}
