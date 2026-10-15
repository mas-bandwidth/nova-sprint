//go:build functional

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/config"
)

// The stall and stuck classes of 2026-10-11, clipped in (docs/TESTS.md, "Stall and stuck
// coverage"; make release-accept). Each test here is the functional half of a class whose fix
// is on main: a real store (redis-server on a port of its own, the function library loaded),
// the workers' and readers' verbs and the machine's tick run by the binary under acceptance
// (NOVA_SPRINT_BIN, else this tree's), and the coordinator's verbs in this process, as the
// seat's verbs reach the server (the binary arms the seat's hook gate for every coordinator
// verb; a test cannot hook in, so a coordinator verb run by the binary would be refused).
// Each was run red against the parent of its fix (the PR body names the commit and the line).

// stallRig is one class's sprint: a store of its own, the coordinator in process, the binary
// for everyone else.
type stallRig struct {
	t    *testing.T
	addr string
	a    *app
	do   func(args ...string) string
	run  func(args ...string) (int, string, string)
	bin  string
	home string
}

func newStallRig(t *testing.T) *stallRig {
	t.Helper()
	addr, a, do, run := releaseStore(t, "coordinator")
	// The sprint row's git_identity, as nova-config's apply writes it: without it a land
	// refuses with exit 2 and the switched server counts that as a land fault in the window.
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Set(context.Background(), config.SprintKey(config.FieldGitIdentity), config.DefaultGitIdentity, 0).Err())
	return &stallRig{t: t, addr: addr, a: a, do: do, run: run, bin: releaseBinary(t), home: t.TempDir()}
}

// cli runs the binary as actor: its exit code, stdout and stderr. The environment is the
// store's and the actor's alone: no NOVA_SPRINT_SERVER, so nothing is forwarded anywhere.
func (r *stallRig) cli(actor string, args ...string) (int, string, string) {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.Env = r.env(actor)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		require.True(r.t, errors.As(err, &ee), "%s %v: %v", r.bin, args, err)
		code = ee.ExitCode()
	}
	return code, out.String(), errb.String()
}

func (r *stallRig) env(actor string) []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + r.home, "TMPDIR=" + os.TempDir(),
		"NOVA_SPRINT_REDIS=" + r.addr, "NOVA_SPRINT_ACTOR=" + actor,
		"GIT_CONFIG_PARAMETERS='maintenance.auto'='false'"}
}

// cliOK is cli, the exit 0.
func (r *stallRig) cliOK(actor string, args ...string) string {
	r.t.Helper()
	code, out, errs := r.cli(actor, args...)
	require.Equal(r.t, 0, code, "%s %v: exit %d\n%s%s", actor, args, code, out, errs)
	return out
}

// tick is one tick by the binary: its output, which never holds a refused drain.
func (r *stallRig) tick() string {
	r.t.Helper()
	code, out, errs := r.cli("coordinator", "tick")
	require.Equal(r.t, 0, code, "tick: exit %d\n%s%s", code, out, errs)
	require.NotContains(r.t, out+errs, "REFUSED drain", "the tick's drain was refused: the queue is poisoned\n%s%s", out, errs)
	return out + errs
}

// beat beats the members up, at load 0, by the binary.
func (r *stallRig) beat(members ...string) {
	r.t.Helper()
	for _, m := range members {
		r.cliOK(m, "fleet", "beat", m, "--load", "0")
	}
}

// take is a worker's take of everything dealt to it, by the binary.
func (r *stallRig) take(who string) []sprint.Packet {
	r.t.Helper()
	var take output
	raw := r.cliOK(who, "take", "--as", who, "--max", "100", "--epoch", "0", "--json")
	require.NoError(r.t, json.Unmarshal([]byte(raw), &take), raw)
	return take.Packets
}

// finish finishes a taken work card ok with head, by the binary.
func (r *stallRig) finish(who string, p sprint.Packet, head string) {
	r.t.Helper()
	r.cliOK(who, "finish", "--as", who, p.Card+"@"+strconv.Itoa(p.Gen), "--epoch", "0", "--head", head, "--report", "done")
}

// work takes everything dealt to who and finishes each work card ok, reading each read card
// ok: the primaries it finished, in the order taken.
func (r *stallRig) work(who string) []string {
	r.t.Helper()
	var done []string
	for _, p := range r.take(who) {
		if p.Kind == "read" {
			r.cliOK(who, "read", "--as", who, "--ok", p.Card+"@"+strconv.Itoa(p.Gen), "--epoch", "0")
			continue
		}
		r.finish(who, p, stallHead(p.Primary))
		done = append(done, p.Primary)
	}
	return done
}

// readAll is a reader's read of everything asked of it, ok, by the binary ("nothing asked" is
// no failure).
func (r *stallRig) readAll(readers ...string) {
	r.t.Helper()
	for _, rd := range readers {
		r.cli(rd, "queue", "--as", rd) // its queue begins what it was asked
		r.cli(rd, "read", "--as", rd, "--ok", "--max", "100", "--epoch", "0")
	}
}

// card is a card's view on the store.
func (r *stallRig) card(id string) cardView {
	r.t.Helper()
	return releaseCard(r.t, r.do, id)
}

func (r *stallRig) col(id string) string {
	r.t.Helper()
	return r.card(id).Primary.Col
}

// stallHead is a well-formed head for a card's finish.
func stallHead(id string) string {
	h := strings.Repeat("0", 40) + strconv.Itoa(len(id)) + strings.Map(func(c rune) rune {
		if c >= '0' && c <= '9' {
			return c
		}
		return -1
	}, id)
	return h[len(h)-40:]
}

// toMerging ticks, and has the readers read every card asked, until each card is merging.
func (r *stallRig) toMerging(readers []string, ids ...string) {
	r.t.Helper()
	merging := func() bool {
		for _, id := range ids {
			if r.col(id) != "merging" {
				return false
			}
		}
		return true
	}
	for i := 0; i < 6 && !merging(); i++ {
		r.tick()
		r.readAll(readers...)
	}
	for _, id := range ids {
		require.Equal(r.t, "merging", r.col(id), "%s is queued to merge", id)
	}
}

// server is a process of the binary at path the test started (and alone kills): its output
// as it comes.
type server struct {
	cmd  *exec.Cmd
	mu   sync.Mutex
	out  bytes.Buffer
	done chan error
}

func (s *server) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String()
}

func (s *server) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.Write(p)
}

// startServer starts path with args as actor on the rig's store; the test's cleanup kills
// it (this process's own child, by its pid) if it still runs.
func (r *stallRig) startServer(path, actor string, args ...string) *server {
	r.t.Helper()
	s := &server{done: make(chan error, 1)}
	s.cmd = exec.Command(path, args...)
	s.cmd.Env = r.env(actor)
	s.cmd.Dir = r.home
	s.cmd.Stdout, s.cmd.Stderr = s, s
	require.NoError(r.t, s.cmd.Start())
	go func() { s.done <- s.cmd.Wait() }()
	r.t.Cleanup(func() {
		if s.cmd.ProcessState == nil {
			_ = s.cmd.Process.Kill() // ignored: the test's own child; it may have exited
		}
	})
	return s
}

// until waits up to d for the server's output to say any of words, or the server to exit:
// the word found ("" for none) and whether it exited.
func (s *server) until(d time.Duration, words ...string) (string, bool) {
	deadline := time.After(d)
	for {
		text := s.text()
		for _, w := range words {
			if strings.Contains(text, w) {
				return w, false
			}
		}
		select {
		case err := <-s.done:
			s.done <- err
			return "", true
		case <-deadline:
			return "", false
		case <-time.After(50 * time.Millisecond):
		}
	}
}
