package store

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxName is the most bytes an id or a name may hold (op id, card id, row,
// stream and policy names, actor, the op's kind label, and every field that
// names one of them), so no index can ever refuse one. Brief, reason, body,
// head, branch, note and the other text fields have no index and no limit.
const MaxName = 128

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// checkID refuses an op with no id, or an id that is not a name a store can
// hold. It is the only check made before the stored-receipt lookup: a known
// id returns its receipt whatever the body says.
func checkID(op Op) error {
	if op.ID == "" {
		return invalid("an op needs an id")
	}
	c := checker{op: op.ID, in: "the op"}
	c.name(op.ID)
	return c.err
}

// check refuses an op whose body can never enter the store, before the
// epoch or any condition is looked at: no epoch, a condition or write
// outside the closed sets (a pointer, an embedding type or nil included),
// two writes to one field, an integer outside its column, a name over
// MaxName bytes, text that is not valid UTF-8 or holds a NUL byte
// (PostgreSQL text cannot hold one), a time outside the years 0..9999.
// Nothing is logged.
func check(op Op) error {
	switch {
	case op.Epoch < 1:
		return invalid("op %s needs an epoch", op.ID)
	case op.Epoch > math.MaxInt32:
		return invalid("op %s: epoch %d is outside the range 1..%d", op.ID, op.Epoch, math.MaxInt32)
	case op.At.IsZero() || op.At.UTC().Year() < 0 || op.At.UTC().Year() > 9999:
		return invalid("op %s: at %s is not a time in the years 0..9999", op.ID, op.At)
	}
	c := checker{op: op.ID, in: "the op"}
	c.name(op.Actor, op.Kind).text(op.Note)
	for _, e := range op.Expect {
		c.in = fmt.Sprintf("condition %T", e)
		if cd := condOf(e); cd.check == nil {
			c.fail("it is not a condition of the closed set")
		} else {
			cd.check(&c)
		}
	}
	seen := map[field]bool{}
	for _, w := range op.Writes {
		c.in = fmt.Sprintf("write %T", w)
		sp := specOf(w)
		if sp.check == nil {
			c.fail("it is not a write of the closed set")
			continue
		}
		for _, f := range sp.fields {
			if seen[f] {
				c.fail("a second write to %s", f)
			}
			seen[f] = true
		}
		sp.check(&c)
	}
	return c.err
}

// checkClear is check for a clear: kind clear, no writes, at most AtSeq.
func checkClear(op Op) error {
	if err := check(op); err != nil {
		return err
	}
	if op.Kind != "clear" || len(op.Writes) > 0 {
		return invalid("op %s: a clear is kind clear with no writes", op.ID)
	}
	for _, e := range op.Expect {
		if _, ok := e.(AtSeq); !ok {
			return invalid("op %s: a clear takes only an AtSeq condition, not %T", op.ID, e)
		}
	}
	return nil
}

// checker keeps the first invalid field of an op. Each call checks fields of
// one kind and returns the checker, so one line checks one write.
type checker struct {
	op, in string // the op id, and the condition or write being checked
	err    error
}

func (c *checker) fail(format string, args ...any) {
	if c.err == nil {
		c.err = invalid("op %s: %s: %s", c.op, c.in, fmt.Sprintf(format, args...))
	}
}

// text is free text: valid UTF-8 with no NUL byte, any length.
func (c *checker) text(ss ...string) *checker {
	if slices.ContainsFunc(ss, badText) {
		c.fail("text that is not valid UTF-8 or holds a NUL byte")
	}
	return c
}

// name is an id or a name: text of at most MaxName bytes ("" is none).
func (c *checker) name(ss ...string) *checker {
	if slices.ContainsFunc(ss, func(s string) bool { return len(s) > MaxName }) {
		c.fail("an id or name over %d bytes", MaxName)
	}
	return c.text(ss...)
}

// num is an int that fits a PostgreSQL integer, none meaningful below 0.
func (c *checker) num(what string, n int) *checker {
	if n < 0 || n > math.MaxInt32 {
		c.fail("%s %d is outside the range 0..%d", what, n, math.MaxInt32)
	}
	return c
}

// num64 is num for a PostgreSQL bigint.
func (c *checker) num64(what string, n int64) *checker {
	if n < 0 {
		c.fail("%s %d is outside the range 0..%d", what, n, int64(math.MaxInt64))
	}
	return c
}

// time is a time in the years 0..9999; the zero time is none when it may be.
func (c *checker) time(what string, t time.Time, none bool) *checker {
	if (!none || !t.IsZero()) && (t.IsZero() || t.UTC().Year() < 0 || t.UTC().Year() > 9999) {
		c.fail("%s %s is not a time in the years 0..9999", what, t)
	}
	return c
}

// oneOf refuses a value outside its closed set: a condition on a state no card
// or row can be in can never hold, so it is a caller bug, not a refusal.
func (c *checker) oneOf(what, v string, set []string) *checker {
	if r := oneOf(what, v, set); r != "" {
		c.fail("%s", r)
	}
	return c
}

// hook is a row's hook: session and role both set, the role one of
// RowRoles, or both "" (unhooked).
func (c *checker) hook(session, role string) *checker {
	switch {
	case session == "" && role == "":
	case session == "" || role == "":
		c.fail("a hook with session %q and role %q; set both, or neither to unhook", session, role)
	default:
		c.oneOf("row role", role, RowRoles)
	}
	return c
}

// key is the id or name a condition or write is about: never empty, so a
// reason never names nothing ("card  does not exist").
func (c *checker) key(table, k string) *checker {
	if k == "" {
		what := "a name"
		if table == "card" || table == "judgement" {
			what = "an id"
		}
		c.fail("a %s needs %s", table, what)
	}
	return c.name(k)
}

// keys is key for each name a write points at: a need, an after.
func (c *checker) keys(table string, ks ...string) *checker {
	for _, k := range ks {
		c.key(table, k)
	}
	return c
}

func badText(s string) bool { return !utf8.ValidString(s) || strings.IndexByte(s, 0) >= 0 }

// oneOf is the reason a value is outside its closed set, quoting at most
// MaxName bytes of it, or "".
func oneOf(what, v string, set []string) string {
	if slices.Contains(set, v) {
		return ""
	}
	if len(v) > MaxName {
		v = v[:MaxName] + "..."
	}
	return fmt.Sprintf("%s %q is not one of %s", what, v, strings.Join(set, ", "))
}
