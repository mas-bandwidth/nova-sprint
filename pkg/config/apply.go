package config

import (
	"context"
)

// View is one row as Redis holds it: the kind's fields in the same canonical
// text as a Row, read back from the keys the runtime tools read. The diff is
// Row.Fields against View, field by field, so Redis is always a copy of
// Postgres and never the other way round (docs/SPEC-CONFIG.md, "Apply").
type View map[string]string

// Applier is the Redis side of apply. RedisApplier is the one over
// go-redis; the unit tests hand in a fake.
type Applier interface {
	// Read returns Redis's rows of the kind by name, and the revision of the
	// kind stamped in config:decl (0 when none). It writes nothing, so
	// --check can call it against a store whose function library is
	// missing.
	Read(ctx context.Context, kind string) (map[string]View, int64, error)
	// Prepare is called once before the first write: it installs the Redis
	// Function library when the store has none (fn.LoadMissing).
	Prepare(ctx context.Context) error
	// Write adds (prev nil) or updates one row through the runtime's own
	// functions. A refusal (a bench's CEILING, an actor without the role) is a
	// *RefusedError.
	Write(ctx context.Context, kind string, row Row, prev View, actor, idem string) error
	// Remove removes one row's keys. A row the runtime still holds (a
	// machine with consumers) is a *RefusedError naming them, and
	// nothing is written.
	Remove(ctx context.Context, kind, name, actor, idem string) error
	// Stamp records rev as the kind's applied revision, compare-and-set
	// against prev: a store whose stamp moved past prev is ErrConflict.
	Stamp(ctx context.Context, kind string, prev, rev int64) error
}

// Op is one line of a plan.
type Op struct {
	Op      string   // OpAdd, OpSet, OpRemove or OpHeld
	Name    string   // the row's name, or the whole held line for OpHeld (SaidLine prints it verbatim)
	Changed []string // the fields that differ, for OpSet
	Row     Row      // the row to write, for OpAdd and OpSet
	Prev    View     // Redis's row, for OpSet and OpRemove
}

// OpHeld is the held write a publish reports instead of moving the sprint
// seat (docs/SPEC-CONFIG.md, "sprint"): its Name is the one line. SaidLine
// prints that name verbatim. nova-config apply prints through OpLine, which
// wraps the name; OpLine is outside this change.
const OpHeld = "held"

// Result is one kind's apply.
type Result struct {
	Kind                string
	Rev                 int64 // Postgres's revision of the kind
	Add, Set, Remove    int
	Ops                 []Op
	Check               bool // --check: nothing was written
	RedisRev            int64
	PreparedForWritesOK bool
}
