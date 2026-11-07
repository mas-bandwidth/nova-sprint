// Package bus is the nova-bus edge of layer 4 (docs/LAYER-4-EDGES.md). It sends outbound notifications and reads hook.
package bus

import (
	"cmp"
	"context"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	DefaultSender       = "sprint"
	DefaultCoordinator  = "rowan"
	DefaultTimeout      = 5 * time.Second
	MaxMessageBytes     = 4096
	MaxCardLines        = 20
	MaxFieldBytes       = 300
	CursorPrefix        = "push:"
	SubjectDeal         = "DEAL"
	SubjectWithdrawn    = "WITHDRAWN"
	SubjectNeedsYou     = "NEEDS YOU"
	SubjectLanded       = "LANDED"
	SubjectStreamLanded = "STREAM LANDED"
)

type Bus interface {
	Send(ctx context.Context, to, subject, body string) (msgID string, err error)
	Who(ctx context.Context) (map[string]Presence, error)
}
type Presence struct {
	Name, Uptime, Detail string
}
type Message struct {
	To, Subject, Body string
	MinSeq, MaxSeq    int64
	Epoch             int
}
type Delivered struct {
	Message   Message
	MsgID     string
	CursorSeq int64
}
type Config struct {
	Exec                     edge.Exec
	Transport                Bus
	Bin, Sender, Coordinator string
	Timeout                  time.Duration
	Env                      []string
	AllowNew                 bool
}
type Edge struct {
	exec                     edge.Exec
	transport                Bus
	bin, sender, coordinator string
	timeout                  time.Duration
	env                      []string
	allowNew                 bool
}

func New(cfg Config) *Edge {
	ex := cfg.Exec
	if ex == nil {
		ex = edge.NewOSExec()
	}
	e := &Edge{
		exec:        ex,
		bin:         cmp.Or(cfg.Bin, "nova-bus"),
		sender:      cmp.Or(cfg.Sender, DefaultSender),
		coordinator: cmp.Or(cfg.Coordinator, DefaultCoordinator),
		timeout:     cmp.Or(cfg.Timeout, DefaultTimeout),
		env:         slices.Clone(cfg.Env),
		allowNew:    cfg.AllowNew,
	}
	if cfg.Transport != nil {
		e.transport = cfg.Transport
	} else {
		e.transport = e
	}
	return e
}
func CursorName(recipient string) string { return CursorPrefix + recipient }
func BusReply(sender string) string {
	sender = strings.TrimSpace(sender)
	if sender == "" {
		sender = "<row>"
	}
	return "replies are verbs: run nova-sprint next --as " + sender
}
func TruncateField(text, id string) string {
	text = sanitizeInline_internal(text)
	if len(text) <= MaxFieldBytes {
		return text
	}
	cut := text[:MaxFieldBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "... (full text: nova-sprint show " + id + ")"
}
func sanitizeInline_internal(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
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
	argv = append(argv, subject, body)
	res, err := e.exec.Run(ctx, edge.Cmd{Argv: argv, Env: e.env, Timeout: e.timeout})
	if err != nil {
		return "", fmt.Errorf("bus send to %s (%s): %w", to, subject, err)
	}
	if res.ExitCode != 0 {
		detail := strings.TrimSpace(cmp.Or(res.Stderr, res.Stdout))
		return "", fmt.Errorf("bus send to %s (%s): nova-bus exited %d (%s); verify %s is hooked in (`nova-bus who`)", to, subject, res.ExitCode, detail, e.sender)
	}
	msgID, ok := parseSendConfirmation_internal(res.Stdout, to)
	if !ok {
		return "", fmt.Errorf("bus send to %s (%s): missing `<recipient> <id>` confirmation in stdout %q; leaving cursor unmoved", to, subject, strings.TrimSpace(res.Stdout))
	}
	return msgID, nil
}
func parseSendConfirmation_internal(stdout, to string) (string, bool) {
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 2 && fields[0] == to && fields[1] != "" {
			return fields[1], true
		}
	}
	return "", false
}
func (e *Edge) Who(ctx context.Context) (map[string]Presence, error) {
	res, err := e.exec.Run(ctx, edge.Cmd{Argv: []string{e.bin, "who"}, Env: e.env, Timeout: e.timeout})
	if err != nil {
		return nil, fmt.Errorf("bus who: %w", err)
	}
	if res.ExitCode != 0 {
		detail := strings.TrimSpace(cmp.Or(res.Stderr, res.Stdout))
		return nil, fmt.Errorf("bus who: nova-bus exited %d (%s); check NOVA_BUS_REDIS and redis-server", res.ExitCode, detail)
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
		line = strings.TrimSpace(line)
		if line == "" {
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
	var out []Message
	if m, ok := buildDealMessage_internal(snap, accepted, recipient); ok {
		out = append(out, m)
	}
	if m, ok := buildWithdrawnMessage_internal(snap, accepted, recipient); ok {
		out = append(out, m)
	}
	if recipient == e.coordinator {
		if m, ok := buildNeedsYouMessage_internal(snap, accepted, recipient); ok {
			out = append(out, m)
		}
		if m, ok := buildLandedMessage_internal(snap, accepted, recipient); ok {
			out = append(out, m)
		}
		if m, ok := buildStreamLandedMessage_internal(snap, accepted, recipient); ok {
			out = append(out, m)
		}
	}
	return out
}
func buildDealMessage_internal(snap store.Snapshot, events []store.Event, recipient string) (Message, bool) {
	var entries []string
	var minSeq, maxSeq int64
	seen := make(map[string]bool)
	epoch := snap.Epoch
	for _, ev := range events {
		for _, id := range sortedCardIDs_internal(ev.After.Cards) {
			c := ev.After.Cards[id]
			switch {
			case (c.Kind != "work" && c.Kind != "read") || c.Holder != recipient || c.State != "ready":
				continue
			case ev.Before != nil && ev.Before.Cards[id].State == "ready":
				continue
			case seen[id]:
				continue
			}
			seen[id] = true
			entries = append(entries, formatDealEntry_internal(snap, ev, c, recipient))
			minSeq, maxSeq = updateSeqBounds_internal(minSeq, maxSeq, ev.Seq)
			if ev.Epoch > 0 {
				epoch = ev.Epoch
			}
		}
	}
	if len(entries) == 0 {
		return Message{}, false
	}
	word := "cards"
	if len(entries) == 1 {
		word = "card"
	}
	header := fmt.Sprintf("DEAL: %d %s for %s (epoch %d)", len(entries), word, recipient, epoch)
	trailer := fmt.Sprintf("and %%d more: nova-sprint next --as %s", recipient)
	return Message{To: recipient, Subject: SubjectDeal, Body: packEntries_internal(header, entries, trailer), MinSeq: minSeq, MaxSeq: maxSeq, Epoch: epoch}, true
}
func formatDealEntry_internal(snap store.Snapshot, ev store.Event, c store.Card, recipient string) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("card=%s attempt=%d kind=%s stream=%s tier=%s", c.ID, c.Attempt, c.Kind, c.Stream, c.Tier))
	if c.Kind == "work" && (c.Prior != "" || c.Attempt > 1 || c.Finding != "") {
		why := cmp.Or(c.Finding, c.Reason, producerReason_internal(snap, ev, c.Of))
		lines = append(lines, fmt.Sprintf("prior=%s reason=%q", c.Prior, TruncateField(why, c.ID)))
	}
	if c.Kind == "read" && c.Head != "" {
		lines = append(lines, fmt.Sprintf("kind=read head=%s", c.Head))
	}
	lines = append(lines, fmt.Sprintf("run: nova-sprint take %s@%d --as %s", c.ID, c.Gen, recipient))
	return strings.Join(lines, "\n")
}
func producerReason_internal(snap store.Snapshot, ev store.Event, prodID string) string {
	if ev.After != nil {
		if p, ok := ev.After.Cards[prodID]; ok && (p.Finding != "" || p.Reason != "") {
			return cmp.Or(p.Finding, p.Reason)
		}
	}
	if p, ok := snap.Cards[prodID]; ok {
		return cmp.Or(p.Finding, p.Reason)
	}
	return ""
}
func buildWithdrawnMessage_internal(snap store.Snapshot, events []store.Event, recipient string) (Message, bool) {
	var entries []string
	var minSeq, maxSeq int64
	seen := make(map[string]bool)
	epoch := snap.Epoch
	for _, ev := range events {
		for _, id := range sortedCardIDs_internal(ev.After.Cards) {
			c := ev.After.Cards[id]
			switch {
			case (c.Kind != "work" && c.Kind != "read") || c.Holder != recipient || c.State != "fail":
				continue
			case !wasLiveOrWithdrawn_internal(ev.Before, c) || seen[id]:
				continue
			}
			seen[id] = true
			entry := fmt.Sprintf("card=%s of=%s branch=%s reason=%q\nstop; push committed work",
				c.ID, c.Of, c.Branch, TruncateField(c.Reason, c.ID))
			entries = append(entries, entry)
			minSeq, maxSeq = updateSeqBounds_internal(minSeq, maxSeq, ev.Seq)
			if ev.Epoch > 0 {
				epoch = ev.Epoch
			}
		}
	}
	if len(entries) == 0 {
		return Message{}, false
	}
	header := fmt.Sprintf("WITHDRAWN: %d card(s) for %s (epoch %d)", len(entries), recipient, epoch)
	trailer := fmt.Sprintf("and %%d more: nova-sprint next --as %s", recipient)
	return Message{To: recipient, Subject: SubjectWithdrawn, Body: packEntries_internal(header, entries, trailer), MinSeq: minSeq, MaxSeq: maxSeq, Epoch: epoch}, true
}
func wasLiveOrWithdrawn_internal(before *store.State, after store.Card) bool {
	if before != nil {
		if b, ok := before.Cards[after.ID]; ok {
			return b.State == "ready" || b.State == "working"
		}
	}
	return after.Result == ""
}

type judgementGroup_internal struct {
	key   string
	items []store.Judgement
}

func buildNeedsYouMessage_internal(snap store.Snapshot, events []store.Event, recipient string) (Message, bool) {
	var groups []*judgementGroup_internal
	byKey := make(map[string]*judgementGroup_internal)
	var minSeq, maxSeq int64
	epoch := snap.Epoch
	for _, ev := range events {
		for _, id := range sortedJudgementIDs_internal(ev.After.Judgements) {
			j := ev.After.Judgements[id]
			if j.State != "open" || (ev.Before != nil && ev.Before.Judgements[id].State != "") {
				continue
			}
			k := cmp.Or(j.Group, j.ID)
			g, ok := byKey[k]
			if !ok {
				g = &judgementGroup_internal{key: k}
				byKey[k] = g
				groups = append(groups, g)
			}
			g.items = append(g.items, j)
			minSeq, maxSeq = updateSeqBounds_internal(minSeq, maxSeq, ev.Seq)
			if ev.Epoch > 0 {
				epoch = ev.Epoch
			}
		}
	}
	if len(groups) == 0 {
		return Message{}, false
	}
	entries := make([]string, 0, len(groups))
	for _, g := range groups {
		entries = append(entries, formatJudgementGroup_internal(g))
	}
	header := fmt.Sprintf("NEEDS YOU: %d group(s) (epoch %d)", len(groups), epoch)
	trailer := fmt.Sprintf("and %%d more: nova-sprint next --as %s", recipient)
	return Message{To: recipient, Subject: SubjectNeedsYou, Body: packEntries_internal(header, entries, trailer), MinSeq: minSeq, MaxSeq: maxSeq, Epoch: epoch}, true
}
func formatJudgementGroup_internal(g *judgementGroup_internal) string {
	first := g.items[0]
	ids := make([]string, 0, len(g.items))
	for _, item := range g.items {
		ids = append(ids, item.ID)
	}
	due := "none"
	if !first.Deadline.IsZero() {
		due = first.Deadline.UTC().Format(time.RFC3339)
	}
	cmd := first.Command
	if !strings.HasPrefix(cmd, "nova-sprint judge ") {
		cmd = fmt.Sprintf("nova-sprint judge %s <choice> --reason \"<why>\"", first.ID)
	}
	head := fmt.Sprintf("NEEDS YOU judgement=%s group=%s n=%d size=%d ids=%s kind=%s about=%s due=%s",
		first.ID, g.key, len(g.items), len(g.items), strings.Join(ids, ","), first.Kind, first.About, due)
	return fmt.Sprintf("%s\nquestion: %q\nrun: %s", head, TruncateField(first.Question, first.ID), cmd)
}
func buildLandedMessage_internal(snap store.Snapshot, events []store.Event, recipient string) (Message, bool) {
	var entries []string
	var minSeq, maxSeq int64
	seen := make(map[string]bool)
	epoch := snap.Epoch
	for _, ev := range events {
		for _, id := range sortedCardIDs_internal(ev.After.Cards) {
			c := ev.After.Cards[id]
			switch {
			case (c.Kind != "producer" && c.Kind != "sentinel") || c.State != "landed" || seen[id]:
				continue
			case ev.Before != nil && ev.Before.Cards[id].State == "landed":
				continue
			}
			seen[id] = true
			entries = append(entries, fmt.Sprintf("LANDED card=%s stream=%s head=%s\nrun: nova-sprint show %s", c.ID, c.Stream, c.Head, c.ID))
			minSeq, maxSeq = updateSeqBounds_internal(minSeq, maxSeq, ev.Seq)
			if ev.Epoch > 0 {
				epoch = ev.Epoch
			}
		}
	}
	if len(entries) == 0 {
		return Message{}, false
	}
	header := fmt.Sprintf("LANDED: %d card(s) (epoch %d)", len(entries), epoch)
	trailer := fmt.Sprintf("and %%d more: nova-sprint next --as %s", recipient)
	return Message{To: recipient, Subject: SubjectLanded, Body: packEntries_internal(header, entries, trailer), MinSeq: minSeq, MaxSeq: maxSeq, Epoch: epoch}, true
}
func buildStreamLandedMessage_internal(snap store.Snapshot, events []store.Event, recipient string) (Message, bool) {
	var streams []string
	seenStream := make(map[string]bool)
	var minSeq, maxSeq int64
	epoch := snap.Epoch
	for _, ev := range events {
		for _, id := range sortedCardIDs_internal(ev.After.Cards) {
			c := ev.After.Cards[id]
			if (c.Kind == "producer" || c.Kind == "sentinel") && c.State == "landed" && !seenStream[c.Stream] {
				seenStream[c.Stream] = true
				streams = append(streams, c.Stream)
				minSeq, maxSeq = updateSeqBounds_internal(minSeq, maxSeq, ev.Seq)
				if ev.Epoch > 0 {
					epoch = ev.Epoch
				}
			}
		}
	}
	slices.Sort(streams)
	var entries []string
	for _, sName := range streams {
		count, lastHead, allLanded := streamCompletion_internal(snap, sName)
		if allLanded && count > 0 {
			base := snap.Streams[sName].Base
			entries = append(entries, fmt.Sprintf("STREAM LANDED stream=%s base=%s head=%s cards=%d\nrun: nova-sprint show %s --stitch", sName, base, lastHead, count, sName))
		}
	}
	if len(entries) == 0 {
		return Message{}, false
	}
	header := fmt.Sprintf("STREAM LANDED: %d stream(s) (epoch %d)", len(entries), epoch)
	trailer := fmt.Sprintf("and %%d more: nova-sprint next --as %s", recipient)
	return Message{
		To:      recipient,
		Subject: SubjectStreamLanded,
		Body:    packEntries_internal(header, entries, trailer),
		MinSeq:  minSeq,
		MaxSeq:  maxSeq,
		Epoch:   epoch,
	}, true
}
func streamCompletion_internal(snap store.Snapshot, streamName string) (int, string, bool) {
	count := 0
	lastHead := ""
	for _, id := range sortedCardIDs_internal(snap.Cards) {
		c := snap.Cards[id]
		switch {
		case c.Stream != streamName || (c.Kind != "producer" && c.Kind != "sentinel"):
			continue
		case c.State != "landed":
			return 0, "", false
		}
		count++
		if c.Head != "" {
			lastHead = c.Head
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
		remainingAfter := len(entries) - (i + 1)
		reserve := 0
		switch {
		case remainingAfter > 0:
			reserve = 1 + len(fmt.Sprintf(trailerFmt, remainingAfter))
		case b.Len()+1+len(entry)+reserve > MaxMessageBytes:
			break
		}
		b.WriteString("\n")
		b.WriteString(entry)
		included++
	}
	if remaining := len(entries) - included; remaining > 0 {
		tail := "\n" + fmt.Sprintf(trailerFmt, remaining)
		if b.Len()+len(tail) <= MaxMessageBytes {
			b.WriteString(tail)
		}
	}
	return b.String()
}
func sortedCardIDs_internal(m map[string]store.Card) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
func sortedJudgementIDs_internal(m map[string]store.Judgement) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
func updateSeqBounds_internal(minSeq, maxSeq, seq int64) (int64, int64) {
	if minSeq == 0 || seq < minSeq {
		minSeq = seq
	}
	if seq > maxSeq {
		maxSeq = seq
	}
	return minSeq, maxSeq
}
func (e *Edge) PushRecipient(ctx context.Context, st store.Store, snap store.Snapshot, recipient, opIDPrefix string, now time.Time) ([]Delivered, error) {
	cursorKey := CursorName(recipient)
	curSeq := snap.Cursors[cursorKey]
	events, err := st.Events(ctx, curSeq, store.MaxEvents)
	if err != nil {
		return nil, fmt.Errorf("bus push %s: read events after %d: %w", recipient, curSeq, err)
	}
	msgs := e.BuildRecipientMessages(snap, events, recipient)
	if len(msgs) == 0 {
		return nil, nil
	}
	var delivered []Delivered
	var maxConfirmed int64
	for _, m := range msgs {
		msgID, sendErr := e.transport.Send(ctx, m.To, m.Subject, m.Body)
		switch {
		case sendErr != nil:
			return delivered, sendErr
		case m.MaxSeq > maxConfirmed:
			maxConfirmed = m.MaxSeq
		}
		delivered = append(delivered, Delivered{Message: m, MsgID: msgID, CursorSeq: maxConfirmed})
	}
	if maxConfirmed > curSeq {
		if err := advanceCursor_internal(ctx, st, snap.Epoch, recipient, maxConfirmed, opIDPrefix, now); err != nil {
			return delivered, err
		}
	}
	return delivered, nil
}
func advanceCursor_internal(ctx context.Context, st store.Store, epoch int, recipient string, seq int64, opIDPrefix string, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	opID := fmt.Sprintf("%s-cursor-%s-%d", cmp.Or(opIDPrefix, "push"), recipient, seq)
	rec, err := st.Apply(ctx, store.Op{
		ID:     opID,
		Epoch:  cmp.Or(epoch, 1),
		Actor:  DefaultSender,
		Kind:   "cursor",
		At:     now,
		Writes: []store.Write{store.SetCursor{Name: CursorName(recipient), Seq: seq}},
	})
	switch {
	case err != nil:
		return fmt.Errorf("bus push %s: advance cursor to %d: %w", recipient, seq, err)
	case !rec.Accepted:
		return fmt.Errorf("bus push %s: cursor write to %d refused: %s", recipient, seq, rec.Reason)
	}
	return nil
}
func (e *Edge) PushTick(ctx context.Context, st store.Store, opIDPrefix string, now time.Time) ([]Delivered, error) {
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("bus push tick: snapshot: %w", err)
	}
	recipients := make([]string, 0, len(snap.Rows)+1)
	seen := make(map[string]bool)
	for name := range snap.Rows {
		recipients = append(recipients, name)
		seen[name] = true
	}
	if e.coordinator != "" && !seen[e.coordinator] {
		recipients = append(recipients, e.coordinator)
	}
	slices.Sort(recipients)
	var all []Delivered
	for _, r := range recipients {
		deliv, pushErr := e.PushRecipient(ctx, st, snap, r, opIDPrefix, now)
		all = append(all, deliv...)
		if pushErr != nil {
			return all, pushErr
		}
	}
	return all, nil
}
