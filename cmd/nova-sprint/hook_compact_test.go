//go:build functional

// hook --compact run as a process of its own against an httptest listener, its
// stdout a real OS pipe read within real-time bounds. A functional test (RULES 6a:
// no real time or sockets in a unit test); compactSay's own test is the unit
// tier's (hook_compact_say_test.go).
package main

import (
	"bufio"
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// TestHookCompactPrintsOnlyWhatTheSeatActsOn is the red test of the card: on a
// private store, the compact subscription prints only what the seat acts on. It
// runs a server, proves a `hook --compact --overdue 2s` whose stdout is a real OS
// pipe, raises one card-holding JUDGMENT and a HAPPENED push, and asserts within
// 5s that the JUDGMENT and its first detail line are read and the HAPPENED is not;
// with the judgment unanswered past --overdue it asserts one OVERDUE line, a
// second after another interval, and none once the judgment is answered. Every
// line is readable while the hook still runs (one write a line, no pty).
// (docs/CLI.md, `nova-sprint hook --compact`; docs/SPEC-SPRINT.md, "The hook".)
func TestHookCompactPrintsOnlyWhatTheSeatActsOn(t *testing.T) {
	t.Parallel()
	const name = "hook-compact"
	ta, env := hookSprint(t, name)
	srv := httptest.NewServer(localHandler{ta.a})
	t.Cleanup(srv.Close)
	env["NOVA_SPRINT_SERVER"] = srv.Listener.Addr().String()

	client := newApp(ta.a.getenv)
	client.hookSock = ta.a.hookSock
	client.busOpen = ta.a.busOpen // the twin's bus store: the hook arms its bus push there
	client.backend = ta.a.backend // the compact process reads the server's judgment list from the same private store
	client.now = ta.a.now         // one clock: ages are the store's
	client.sleep = ta.a.sleep

	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	defer pr.Close()
	done := make(chan int, 1)
	go func() {
		var errb bytes.Buffer
		code := runInto(client, []string{"hook", "--compact", "--overdue", "2s", "--redis", "mem:0"}, pw, &errb)
		_ = pw.Close()
		done <- code
	}()

	lines := make(chan string, 256)
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	// next waits for a line with the prefix within d, returning it; "" on timeout.
	next := func(prefix string, d time.Duration) string {
		t.Helper()
		timer := time.After(d)
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					return ""
				}
				if strings.HasPrefix(l, prefix) {
					return l
				}
			case <-timer:
				return ""
			}
		}
	}

	chalLine := next("HOOK CHALLENGE ", 5*time.Second)
	require.NotEmpty(t, chalLine, "the compact hook's challenge was not readable on the pipe within 5s")
	chal := strings.TrimSuffix(strings.Fields(chalLine)[2], ":")
	code, out, errs := func() (int, string, string) {
		var o, e bytes.Buffer
		c := runInto(client, []string{"hook", "--prove", chal, "--actor", name, "--redis", "mem:0"}, &o, &e)
		return c, o.String(), e.String()
	}()
	require.Equal(t, 0, code, out+errs)
	assert.Contains(t, out, "HOOK PROVEN name="+name)

	// raise one card-holding judgment (NWorkFailed), which the server's own list
	// holds, and one HAPPENED note addressed to the seat: the running hook's look
	// pushes both, and the compact subscription must print the judgment and drop
	// the HAPPENED on the pipe.
	ta.ok("start")
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)
	card := ta.fleetCard("s1-1.w1")
	require.NotNil(t, card)
	ta.failOnce(card.Row, "s1-1.w1@1", "the compact test holds the card")
	ta.ok("tick")
	st, err := ta.a.store(common{redis: "mem:0"})
	require.NoError(t, err)
	_, err = st.Run(context.Background(), store.Step{Verb: "persist happened", Plan: func(*sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Happened, Type: sprint.NReadyToMerge, Stream: "s1", To: name,
			Count: 1, Who: sprint.MachineActor, At: ta.a.now(),
			What: "1 accepted and queued to merge: s1-1; run: nova-sprint land --stream s1"}}}
	}})
	require.NoError(t, err)
	s := ta.hookSession()
	require.NotNil(t, s)
	s.look(context.Background())

	// the JUDGMENT is printed as PUSH <n> JUDGMENT ... with its first detail line;
	// the HAPPENED push raised on the same look is dropped, never printed.
	var pushed, detail string
	deadline := time.After(5 * time.Second)
waitPush:
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				break waitPush
			}
			if strings.Contains(l, "ready to merge") {
				t.Fatalf("a HAPPENED push was printed on the compact stream: %q", l)
			}
			if strings.HasPrefix(l, "PUSH ") && strings.Contains(l, "JUDGMENT") {
				pushed = l
				select {
				case d, ok := <-lines:
					if ok {
						detail = d
					}
				case <-time.After(time.Second):
				}
				break waitPush
			}
		case <-deadline:
			break waitPush
		}
	}
	require.NotEmpty(t, pushed, "the JUDGMENT was not read as a PUSH line within 5s")
	assert.Contains(t, pushed, "PUSH ")
	assert.Contains(t, pushed, "JUDGMENT ")
	assert.NotContains(t, pushed, "session=", "the compact PUSH line carries no acknowledge menu")
	require.NotEmpty(t, detail, "the judgment's first detail line was not read")
	assert.True(t, strings.HasPrefix(detail, "  "), "the detail line is indented under its PUSH line: %q", detail)
	assert.NotContains(t, detail, "nova-sprint hook --ack", "no verb menu is printed")

	// unanswered past --overdue: one OVERDUE line.
	ta.step(2 * time.Second)
	first := next("OVERDUE ", 5*time.Second)
	require.NotEmpty(t, first, "no OVERDUE line within 5s of --overdue 2s")
	assert.Contains(t, first, ":: s1-1")
	t.Logf("overdue: %s", first)

	// again every --overdue until answered: a second after another interval.
	ta.step(2 * time.Second)
	second := next("OVERDUE ", 5*time.Second)
	require.NotEmpty(t, second, "no second OVERDUE line within another interval")

	// answered: it leaves the server's list and is not named again.
	ta.ok("rework s1-1 --fix 'the compact test answered it'")
	ta.step(2 * time.Second)
	select {
	case l := <-lines:
		if strings.HasPrefix(l, "OVERDUE ") {
			t.Fatalf("an answered judgment was named again: %q", l)
		}
	case <-time.After(3 * time.Second):
	}

	// the hook is still running; end it cleanly.
	select {
	case code := <-done:
		t.Fatalf("the compact hook exited (code %d) while it held the seat", code)
	default:
	}
	var uout, uerrs bytes.Buffer
	require.Equal(t, 0, runInto(client, strings.Fields("unhook --actor "+name+" --redis mem:0"), &uout, &uerrs), uout.String()+uerrs.String())
	select {
	case code := <-done:
		require.Equal(t, 0, code)
	case <-time.After(10 * time.Second):
		t.Fatal("the compact hook did not end after unhook")
	}
}
