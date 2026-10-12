package friend

import (
	"context"
	"io"
	"sync"
	"time"
)

// CardRunner is a Deliverer whose one-shot lane runs each card as a process
// of its own: no session to open or keep, the card's brief the whole prompt,
// no bus message riding along, and the card's result read from its outbox,
// never from the process's output (docs/SPEC-FRIEND.md, one-shot lanes; the
// owner, 2026-10-04: four Claude accounts as heavy-tier friends). Claude is
// one.
type CardRunner interface {
	Deliverer
	// RunCard runs card c to its end: its exit, and an error naming what the
	// outbox lacks when the run wrote no REPORT.md or RESULT.md there.
	RunCard(ctx context.Context, c Card) (LaneTurn, error)
	// Refusal is why no card can run now, with its remedy; empty when one can.
	Refusal() string
}

// Claude is the claude harness: no deliver command in batch (Stub, passive:
// the daemon reads nothing for it), and in one-shot mode each card run as
// `env CLAUDE_CONFIG_DIR=<dir> claude -p <brief> --output-format stream-json
// --verbose` and the trim (ClaudeTrim) in Dir with stdin from /dev/null,
// priced and its limit read from its stream-json (adapter_claude.go). The config directory is the
// friend row's config_dir, so each friend is its own account's login and
// settings (its permission mode among them); stream-json prints as the run
// works, so the daemon's silence watch sees a working run.
type Claude struct {
	Stub
	Friend, Dir string
	// ConfigDir is the friend row's config_dir as the daemon last read it;
	// nil or empty refuses every card.
	ConfigDir func() string
	Run       Exec
	Program   string           // "claude" when empty
	Out       io.Writer        // where the run's output goes, when set: the daemon's record
	Now       func() time.Time // time.Now when nil: the clock a limit's reset is read against
	// TokenCap is the friend row's per-card token cap as the daemon last read
	// it (TokenCapOf; 0 none); nil is DefaultTokenCap (tokencap.go).
	TokenCap func() int64

	mu    sync.Mutex
	runs  int      // the runs priced so far
	cost  float64  // every run's cost so far, in US dollars
	usage Usage    // the last usage a run measured
	cards cardRuns // each card's tokens over its finished runs
}
