package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/goenv"
	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
	"github.com/mas-bandwidth/nova-sprint/pkg/testredis"
	"github.com/stretchr/testify/require"
)

// functionalTestApp is the one way a functional test runs a nova-sprint verb. It
// always adds --redis <store> so the verb runs on the test's own store and never
// forwards (forward.go, server), and it runs with a home and config directory of
// the test's own (t.TempDir) and no NOVA_* variable from the machine: a verb run
// through it cannot reach a live sprint server however the machine is set up.
type functionalTestApp struct {
	t     *testing.T
	a     *app
	Env   map[string]string
	home  string
	store string
}

// newFunctionalTestApp makes the helper: a scrubbed app whose verbs run on the
// test's own store, with no NOVA_* variable and a home and config directory of
// the test's.
func newFunctionalTestApp(t *testing.T, redisAddr string) *functionalTestApp {
	t.Helper()
	home := t.TempDir()
	env := map[string]string{
		"HOME":            home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		// the actor is recorded with every change; the default needs both
		// hooks proven live. This is not a server variable, so it does not forward. Every server-reaching
		// variable (NOVA_SPRINT_SERVER and the seat record) is otherwise
		// scrubbed.
		"NOVA_SPRINT_ACTOR": "coordinator",
	}
	a := newApp(func(k string) string { return env[k] })
	a.home = func() (string, error) { return home, nil }
	// the functional tests' bare starts run on briefless --count cards: the card check gate
	// is off here, and every gate test turns it on (check_cards_test.go)
	a.cardCheckGate = false
	a.forward = func(_ context.Context, addr string, verbs ...[]string) ([]sprintwire.Result, error) {
		t.Fatalf("a verb was forwarded to a sprint server at %s: %q", addr, verbs)
		return nil, errors.New("forwarding is refused in a functional test")
	}
	t.Cleanup(a.close)
	return &functionalTestApp{t: t, a: a, Env: env, home: home, store: redisAddr}
}

// app is the app the verbs run on; a test reaches for it when it needs the store
// or a verb's setup, never to run a verb.
func (ta *functionalTestApp) app() *app { return ta.a }

// runVerb runs a verb through the helper on the test's own store and returns
// stdout; it fails the test when the verb exits nonzero.
func (ta *functionalTestApp) runVerb(argv ...string) string {
	ta.t.Helper()
	out, code, errb := ta.runVerbOut(argv...)
	require.Zero(ta.t, code, "%s: exit %d\n%s%s", strings.Join(argv, " "), code, out, errb)
	return out
}

// runVerbOut runs a verb through the helper on the test's own store and returns
// its stdout and exit code, without failing on a nonzero code.
func (ta *functionalTestApp) runVerbOut(argv ...string) (string, int, string) {
	ta.t.Helper()
	if len(argv) == 1 && strings.ContainsAny(argv[0], " \t") {
		argv = strings.Fields(argv[0])
	}
	if !slices.Contains(argv, "--redis") {
		argv = append(argv, "--redis", ta.store)
	}
	var out, errb bytes.Buffer
	code := runInto(ta.a, argv, &out, &errb)
	return out.String(), code, errb.String()
}

// newScrubbedApp makes a scrubbed app on the test's own store, for a functional
// test that runs several apps over one store (the world/coordinator/machines
// pattern). Each app has HOME and XDG_CONFIG_HOME of the test's own and no
// NOVA_* variable, so none can forward; its verbs run through runOnStore, which
// adds --redis.
func newScrubbedApp(t *testing.T, addr string) *app {
	t.Helper()
	return newFunctionalTestApp(t, addr).a
}

// runOnStore runs a verb on the app a with --redis <store>, so the verb runs on
// the test's own store and never forwards, and writes its stdout and stderr
// into out and errb, returning its code. It is how a functional test's several
// apps run their verbs.
//
// A start goes as the real flow does: the epoch's cards are checked first
// (recordCleanCardCheck), since start refuses until a clean card check of the
// epoch matches the cards (start-refuses-until-a-clean-check).
func runOnStore(a *app, store string, argv []string, out, errb io.Writer) int {
	if !slices.Contains(argv, "--redis") {
		argv = append(argv, "--redis", store)
	}
	if len(argv) > 0 && argv[0] == "start" {
		if err := recordCleanCardCheck(a, store); err != nil {
			fmt.Fprintf(errb, "the rig's card check before start: %v\n", err)
			return 2
		}
	}
	return runInto(a, argv, out, errb)
}

// recordCleanCardCheck is the functional rig's check before a start: it writes the
// epoch's check record for the cards on the store's work table as they are, clean,
// with their digest (store.PutCardCheck, sprint.CheckedCardsOf), as a clean
// nova-sprint check cards writes it. The functional tests' cards are briefless
// --count cards, which the real check's lint kind refuses; what they test is the
// machine after the start, and the gate itself is the unit tests' (check_cards_test.go,
// start_needs_test.go). A change to the cards after this and before the start is
// still refused, as the gate refuses it anywhere.
func recordCleanCardCheck(a *app, addr string) error {
	ctx := context.Background()
	b, err := a.backend(ctx, addr, sprint.Names{})
	if err != nil {
		return err
	}
	st := &store.Store{B: b, Names: sprint.Names{}, Actor: "functional-rig", Now: a.now, NewID: store.NewID}
	snap, err := st.Load(ctx, []string{sprint.Work}, nil)
	if err != nil {
		return err
	}
	// Existing rig starts exercise briefless card lifecycles. Declare fixture
	// funding only when none was applied; never overwrite explicit provider rows.
	if r, ok := b.(*store.Redis); ok {
		if err := st.RoutesInto(ctx, snap); err != nil {
			return err
		}
		if len(snap.Providers) == 0 {
			names := map[string]bool{}
			for _, route := range snap.Routes {
				if route.Provider != "" {
					names[route.Provider] = true
				}
			}
			if len(names) == 0 {
				names["functional-fixture"] = true
			}
			for name := range names {
				if err := r.C.SAdd(ctx, config.ProvidersKey, name).Err(); err != nil {
					return err
				}
				if err := r.C.HSet(ctx, config.ProviderKey(name), "billing", "plan", "funded", "true").Err(); err != nil {
					return err
				}
			}
		}
	}
	cards := sprint.CheckedCardsOf(snap)
	_, err = st.PutCardCheck(ctx, store.CheckRecord{At: a.now().UTC(), By: "functional-rig", Clean: true, Cards: cards, Digest: sprint.CardsDigest(cards)})
	return err
}

// runInto is the one direct app.run in a test file: every test that runs a verb
// on an app it built itself reaches it here. A functional test's runVerbOut and
// runOnStore add --redis to a scrubbed app; a unit test whose store is
// in-memory (mem:0, no server named) runs its verbs here too. When the test has
// not taken forwarding over (a.forward is nil), a verb that names no --redis and
// an app that names a server (NOVA_SPRINT_SERVER or the seat's record,
// forward.go) would dial it, so runInto goes through the same scrub as the
// functional helper: a fresh home and config directory for the run, so the verb
// runs on this process's own store and can never reach a live server. A server
// that came in from the machine's environment is hidden too (namesMachineEnv);
// one a test set itself, to reach its own controlled listener, is left to be
// used. An app that names no server is left alone, and a test that sets
// a.forward has taken forwarding over and reaches only its own server. The
// direct-run scan (internal/ci/no_verb_run_outside_helper_test.go) sees no verb
// run anywhere else.
func runInto(a *app, argv []string, out, errb io.Writer) int {
	if a.forward == nil && !slices.Contains(argv, "--redis") {
		if addr, _ := a.namedServer(); addr != "" {
			restore := scrubForTest(a, namesMachineEnv(a))
			defer restore()
		}
	}
	return a.run(argv, out, errb)
}

// namesMachineEnv reports whether the server the app names in its environment is
// this process's own (os.Getenv): the NOVA_SPRINT_SERVER that leaks in from the
// machine. A server a test set itself — in a getenv of its own, to reach its own
// controlled listener — reads differently from the machine's and is not the one
// to hide; runInto scrubs the machine's server, never the test's.
func namesMachineEnv(a *app) bool {
	v := a.getenv(ServerEnv)
	return v != "" && v == os.Getenv(ServerEnv)
}

// scrubForTest gives the app a home and config directory of its own (a fresh
// temporary directory) for the run, so a verb that names no --redis cannot find
// and forward to a live sprint server through the seat's record (forward.go,
// namedServer); hideServer also hides NOVA_SPRINT_SERVER when the server came
// from the machine's environment (namesMachineEnv), while a server a test set
// itself is left for the test to reach. The app's environment and home are put
// back when the returned function is called, and the temporary home is removed.
func scrubForTest(a *app, hideServer bool) func() {
	home, err := os.MkdirTemp("", "nova-sprint-test-home")
	oldHome := a.home
	if err == nil {
		a.home = func() (string, error) { return home, nil }
	}
	getenv := a.getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	a.getenv = func(k string) string {
		if hideServer && k == ServerEnv {
			return ""
		}
		return getenv(k)
	}
	return func() {
		a.getenv = getenv
		a.home = oldHome
		if err == nil {
			_ = os.RemoveAll(home)
		}
	}
}

// runBuiltSprint runs a built nova-sprint binary's verb through a scrubbed
// environment: no NOVA_* variable from the machine in a home and config
// directory of the test's own, so it cannot reach a live sprint server however
// the machine is set up (forward.go). It is how a test that must run a real
// binary (a build's own output) does so without forwarding.
func runBuiltSprint(t *testing.T, bin string, args ...string) (string, error) {
	t.Helper()
	home := t.TempDir()
	env := append(withoutNOVAEnv(goenv.Clean(os.Environ())),
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
	)
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// withoutNOVAEnv is the environment minus every NOVA_* variable: no variable
// from the machine (a server named by NOVA_SPRINT_SERVER, a seat credential, a
// NOVA_* provider key) reaches the built binary. The values are never read.
func withoutNOVAEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if ok && strings.HasPrefix(name, "NOVA_") {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// quotedSocket is the proof's unix socket, named so the reply command must
// quote it. It stays under the test directory when that path fits a Unix
// socket, and otherwise in a short private directory, as twinSocket does when
// a long TMPDIR cannot hold one. The directory the fallback makes is the
// caller's to remove.
func quotedSocket(t *testing.T) string {
	t.Helper()
	sock, err := testredis.SocketPath(t.TempDir(), "emma's canary.sock")
	require.NoError(t, err)
	if strings.HasPrefix(sock, os.TempDir()) || strings.HasPrefix(sock, "/tmp") {
		dir := filepath.Dir(sock)
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
	}
	return sock
}
