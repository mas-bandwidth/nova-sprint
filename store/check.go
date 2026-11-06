package store

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// checkID refuses an op with no id, or an id that is not text a store can
// hold. It is the only check made before the stored-receipt lookup: a known
// id returns its receipt whatever the body says.
func checkID(op Op) error {
	switch {
	case op.ID == "":
		return invalid("an op needs an id")
	case badText(op.ID):
		return invalid("op %q: the id is not valid UTF-8 or holds a NUL byte", op.ID)
	}
	return nil
}

// check refuses an op whose body can never enter the store: no epoch, an
// unknown write, two writes to one field, an integer outside 0..MaxInt32,
// text that is not valid UTF-8 or holds a NUL byte (PostgreSQL text cannot
// hold one), a time outside years 0..9999. An unknown condition is refused
// where conditions are read (expect). It runs after the lookup and before any
// write, and nothing is logged.
func check(op Op) error {
	switch {
	case op.Epoch < 1:
		return invalid("op %s needs an epoch", op.ID)
	case op.Epoch > math.MaxInt32:
		return invalid("op %s: epoch %d is outside the range 1..%d", op.ID, op.Epoch, math.MaxInt32)
	case op.At.IsZero() || op.At.UTC().Year() < 0 || op.At.UTC().Year() > 9999:
		return invalid("op %s: at %s is not a time in the years 0..9999", op.ID, op.At)
	case badText(op.Actor, op.Kind, op.Note):
		return invalid("op %s: the actor, kind or note is not valid UTF-8 or holds a NUL byte", op.ID)
	}
	for _, e := range op.Expect {
		if badText(texts(e)...) {
			return invalid("op %s: condition %T is not valid UTF-8 or holds a NUL byte", op.ID, e)
		}
	}
	seen := map[field]bool{}
	for _, w := range op.Writes {
		if err := checkWrite(op.ID, w, seen); err != nil {
			return err
		}
	}
	return nil
}

// checkWrite is check for one write; seen holds the fields written so far.
func checkWrite(id string, w Write, seen map[field]bool) error {
	fs, ok := fields(w)
	if !ok {
		return invalid("op %s: unknown write %T", id, w)
	}
	if badText(texts(w)...) {
		return invalid("op %s: write %T is not valid UTF-8 or holds a NUL byte", id, w)
	}
	rv := reflect.ValueOf(w)
	for i := range rv.NumField() {
		if f := rv.Field(i); f.Kind() == reflect.Int && (f.Int() < 0 || f.Int() > math.MaxInt32) {
			// every integer a write carries is a PostgreSQL integer column
			// with no meaning below 0
			return invalid("op %s: %s %d is outside the range 0..%d", id, strings.ToLower(rv.Type().Field(i).Name), f.Int(), math.MaxInt32)
		}
	}
	for _, f := range fs {
		if seen[f] {
			return invalid("op %s: two writes to %s %s field %s", id, f.table, f.key, f.name)
		}
		seen[f] = true
	}
	return nil
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

func badText(ss ...string) bool {
	return slices.ContainsFunc(ss, func(s string) bool { return !utf8.ValidString(s) || strings.IndexByte(s, 0) >= 0 })
}

// texts are the strings a condition or write carries: its string fields and
// the elements of its string-slice fields (both are flat structs).
func texts(v any) []string {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Struct {
		return nil
	}
	var out []string
	for i := range rv.NumField() {
		switch f := rv.Field(i); f.Kind() {
		case reflect.String:
			out = append(out, f.String())
		case reflect.Slice:
			if s, ok := f.Interface().([]string); ok {
				out = append(out, s...)
			}
		}
	}
	return out
}

func oneOf(what, v string, set []string) string {
	if slices.Contains(set, v) {
		return ""
	}
	return fmt.Sprintf("%s %q is not one of %s", what, v, strings.Join(set, ", "))
}
