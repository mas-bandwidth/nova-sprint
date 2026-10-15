package member

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"io"
	"strings"
	"testing"
	"time"
)

func TestFinishScopeUsesStagedBaseAndPushedHead(t *testing.T) {
	base, head := strings.Repeat("1", 40), strings.Repeat("2", 40)
	p := Packet{Card: "p.w1", Gen: 3, Epoch: 7}
	for _, fail := range []string{"", "merge-base", "diff", "ls-tree"} {
		t.Run(fail, func(t *testing.T) {
			calls := []string{}
			run := func(_ context.Context, dir string, args ...string) (string, string, error) {
				require.Equal(t, "fixture", dir)
				// gitPlain prefixes configuration; examine the command after that prefix.
				joined := strings.Join(args, " ")
				calls = append(calls, joined)
				require.NotContains(t, joined, "HEAD")
				require.Contains(t, joined, base)
				if fail != "" && strings.Contains(joined, fail) {
					return "", "fixture failure", errors.New("unreadable")
				}
				if strings.Contains(joined, "ls-tree") {
					return "src/a.c\n", "", nil
				}
				require.Contains(t, joined, head)
				if strings.Contains(joined, "diff") {
					return "diff --git a/src/a.c b/src/a.c\n", "", nil
				}
				return "", "", nil
			}
			wire, err := collectScope(context.Background(), p, "fixture", base, head, run)
			if fail != "" {
				require.Error(t, err)
				require.Empty(t, wire)
				return
			}
			require.NoError(t, err)
			require.Len(t, calls, 3)
			var e FinishScopeEvidence
			require.NoError(t, json.Unmarshal([]byte(wire), &e))
			require.Equal(t, p.Card, e.Card)
			require.Equal(t, p.Gen, e.Gen)
			require.Equal(t, p.Epoch, e.Epoch)
			require.Equal(t, base, e.Base)
			require.Equal(t, head, e.Head)
			require.Equal(t, []string{"src/a.c"}, e.Tracked)
		})
	}
}

type scopeSprint struct{ args []string }

func (s *scopeSprint) Run(args ...string) (int, []byte) {
	s.args = append([]string(nil), args...)
	return 0, nil
}

func TestMachineFinishCarriesEvidenceAndFailsClosed(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "bound", true: "collection-error"}[failed], func(t *testing.T) {
			now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
			sp := &scopeSprint{}
			p := Packet{Card: "p.w1", Gen: 1, Epoch: 7}
			m := New(Config{As: "m1", Now: func() time.Time { return now }, ScopeCollect: func(got Packet, head string) (string, error) {
				require.Equal(t, p, got)
				require.Equal(t, strings.Repeat("2", 40), head)
				if failed {
					return "", errors.New("git unavailable")
				}
				return `{"card":"p.w1"}`, nil
			}}, sp, nil, nil, io.Discard)
			pu := Push{Sha: strings.Repeat("2", 40)}
			m.collectFinishScope(p, &pu)
			r := Result{Ran: true, OK: true, Shaped: true, Verdict: "ok"}
			l := launch{packet: p, gen: 1, epoch: 7, res: &r, push: &pu}
			m.running[p.Card] = l
			require.True(t, m.reportOne(p.Card, l, now, nil, func(string, []byte) { t.Fatal("unexpected retry") }))
			joined := strings.Join(sp.args, " ")
			if failed {
				require.Contains(t, joined, "--failed")
				require.Contains(t, joined, "git unavailable")
				require.NotContains(t, joined, "--scope")
			} else {
				require.Contains(t, joined, `--scope {"card":"p.w1"}`)
				require.NotContains(t, joined, "--failed")
			}
		})
	}
	// No checkout is a collection error, never a successful evidence-free finish.
	m := New(Config{StepClone: func(Packet) string { return "" }}, nil, nil, nil, io.Discard)
	pu := Push{Sha: strings.Repeat("2", 40)}
	m.collectFinishScope(Packet{}, &pu)
	require.Contains(t, pu.ScopeError, "no staged checkout")
}

func TestFinishScopeCompactsBodiesButKeepsRenameAndCatalogFacts(t *testing.T) {
	raw := "diff --git a/private.c b/other.c\nrename from private.c\nrename to other.c\n@@ -1 +1 @@\n-secret source body\n+new source body\ndiff --git a/internal/docs/catalog.go b/internal/docs/catalog.go\n@@ -1 +1 @@\n-old catalog row\n+new catalog row\n"
	compact, err := scopeFileDiff(raw)
	require.NoError(t, err)
	require.NotContains(t, compact, "source body")
	require.Contains(t, compact, "rename from private.c\nrename to other.c")
	require.Contains(t, compact, "-old catalog row\n+new catalog row")
	_, err = scopeFileDiff("unreadable output")
	require.Error(t, err)
}
