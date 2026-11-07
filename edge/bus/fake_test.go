package bus

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
)

const InitialClock int64 = 1000

type SentMessage struct {
	ID, To, Subject, Body string
	At                    int64
}
type Fake struct {
	mu           sync.Mutex
	clock, seq   int64
	sent, wire   []SentMessage
	present      map[string]Presence
	failNext     []error
	dropReply    int
	failByTarget map[string]error
	whoErr       error
}

func NewFake() *Fake {
	return &Fake{clock: InitialClock, present: make(map[string]Presence), failByTarget: make(map[string]error)}
}
func (f *Fake) Now() int64 { f.mu.Lock(); defer f.mu.Unlock(); return f.clock }
func (f *Fake) SetPresent(name, uptime, detail string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.present[name] = Presence{Name: name, Uptime: uptime, Detail: detail}
}
func (f *Fake) RemovePresent(name string) { f.mu.Lock(); defer f.mu.Unlock(); delete(f.present, name) }
func (f *Fake) SetWhoError(err error)     { f.mu.Lock(); defer f.mu.Unlock(); f.whoErr = err }
func (f *Fake) FailNextSend(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNext = append(f.failNext, err)
}
func (f *Fake) SetRecipientError(to string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.failByTarget, to)
		return
	}
	f.failByTarget[to] = err
}
func (f *Fake) DropNextReply(count int) { f.mu.Lock(); defer f.mu.Unlock(); f.dropReply += count }
func (f *Fake) Sent() []SentMessage     { f.mu.Lock(); defer f.mu.Unlock(); return slices.Clone(f.sent) }
func (f *Fake) WireMessages() []SentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.wire)
}
func (f *Fake) Send(ctx context.Context, to, subject, body string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	switch {
	case strings.TrimSpace(to) == "" || strings.TrimSpace(subject) == "":
		return "", fmt.Errorf("bus send: recipient and subject are required; pass --to <name> and a non-empty subject")
	case len(body) > MaxMessageBytes:
		return "", fmt.Errorf("bus send to %s (%s): body is %d bytes, exceeding the %d-byte cap", to, subject, len(body), MaxMessageBytes)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clock++
	if len(f.failNext) > 0 {
		err := f.failNext[0]
		f.failNext = f.failNext[1:]
		return "", err
	}
	if err, ok := f.failByTarget[to]; ok {
		return "", err
	}
	f.seq++
	msg := SentMessage{ID: fmt.Sprintf("%d-%d", f.clock, f.seq), To: to, Subject: subject, Body: body, At: f.clock}
	f.wire = append(f.wire, msg)
	if f.dropReply > 0 {
		f.dropReply--
		return "", fmt.Errorf("bus send to %s (%s): lost `<recipient> <id>` confirmation after send", to, subject)
	}
	f.sent = append(f.sent, msg)
	return msg.ID, nil
}
func (f *Fake) Who(ctx context.Context) (map[string]Presence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clock++
	if f.whoErr != nil {
		return nil, f.whoErr
	}
	return maps.Clone(f.present), nil
}
