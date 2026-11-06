package store

import (
	"fmt"
	"math"
	"slices"
	"strings"
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
		c.expect(e)
	}
	seen := map[field]bool{}
	for _, w := range op.Writes {
		c.in = fmt.Sprintf("write %T", w)
		fs, ok := fields(w)
		if !ok {
			c.fail("it is not a write of the closed set")
		}
		for _, f := range fs {
			if seen[f] {
				c.fail("a second write to %s %s field %s", f.table, f.key, f.name)
			}
			seen[f] = true
		}
		c.write(w)
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

// expect checks one condition: a type of the closed set, its names and text.
func (c *checker) expect(e Expect) {
	switch v := e.(type) {
	case CardGen:
		c.name(v.Card)
	case CardState:
		c.name(v.Card).text(v.State)
	case CardHolder:
		c.name(v.Card, v.Holder)
	case NeedsLanded:
		c.name(v.Card)
	case RowIs:
		c.name(v.Row).text(v.State)
	case RowGen:
		c.name(v.Row)
	case RowHeld:
		c.name(v.Row)
	case StreamHeld:
		c.name(v.Stream)
	case CardAbsent:
		c.name(v.Card)
	case AtSeq:
	default:
		c.fail("it is not a condition of the closed set")
	}
}

// write checks one write's fields; fields has already refused a write
// outside the closed set.
func (c *checker) write(w Write) {
	switch v := w.(type) {
	case PutStream:
		c.name(v.Name).text(v.Repo, v.Base).num("pos", v.Pos)
	case SetStreamHeld:
		c.name(v.Stream)
	case PutCard:
		c.name(v.ID, v.Of, v.Stream, v.Holder).name(v.Needs...).name(v.After...).text(v.Kind, v.Brief, v.Priority).num("attempt", v.Attempt).num("score", v.Score)
	case SetCardState:
		c.name(v.Card).text(v.State)
	case SetCardHolder:
		c.name(v.Card, v.Holder)
	case SetCardResult:
		c.name(v.Card).text(v.Result)
	case SetCardAttempt:
		c.name(v.Card).num("attempt", v.Attempt)
	case SetCardBranch:
		c.name(v.Card).text(v.Branch)
	case SetCardHead:
		c.name(v.Card).text(v.Head)
	case SetCardPR:
		c.name(v.Card).num("pr", v.PR)
	case SetCardReason:
		c.name(v.Card).text(v.Reason)
	case SetCardScore:
		c.name(v.Card).num("score", v.Score)
	case SetCardAuthor:
		c.name(v.Card, v.Author)
	case SetCardBaseSHA:
		c.name(v.Card).text(v.SHA)
	case SetCardNeeds:
		c.name(v.Card).name(v.Needs...)
	case SetCardAfter:
		c.name(v.Card).name(v.After...)
	case SetCardPriorHead:
		c.name(v.Card).text(v.Head)
	case SetCardBounces:
		c.name(v.Card).num("bounces", v.Bounces)
	case SetCardCost:
		if c.name(v.Card); v.Cost < 0 {
			c.fail("cost %d is outside the range 0..%d", v.Cost, int64(math.MaxInt64))
		}
	case SetCardPriority:
		c.name(v.Card).text(v.Priority)
	case PutRow:
		c.name(v.Name).text(v.Kind, v.Session).num("width", v.Width)
	case SetRowWidth:
		c.name(v.Row).num("width", v.Width)
	case SetRowState:
		c.name(v.Row).text(v.State)
	case SetRowSession:
		c.name(v.Row).text(v.Session)
	case SetRowFlaps:
		c.name(v.Row).num("flaps", v.Flaps)
	case SetRowHeld:
		c.name(v.Row)
	case PutPolicy:
		c.name(v.Name).text(v.Kind, v.Body)
	case SetActivePolicy:
		c.num("version", v.Version)
	}
}

func badText(s string) bool { return !utf8.ValidString(s) || strings.IndexByte(s, 0) >= 0 }

func oneOf(what, v string, set []string) string {
	if slices.Contains(set, v) {
		return ""
	}
	return fmt.Sprintf("%s %q is not one of %s", what, v, strings.Join(set, ", "))
}
