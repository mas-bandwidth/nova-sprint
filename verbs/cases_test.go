package verbs

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// tcase is one command line against the world: snap edits the sprint
// before the store is made, prep runs commands first (each must succeed)
// or changes a fake, then as runs args and gets exit and every want part.
type tcase struct {
	name string
	snap func(*store.Snapshot)
	prep func(t *testing.T, w *world)
	as   string
	args []string
	exit int
	want []string
}

func runCases(t *testing.T, cases []tcase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := sprint()
			if c.snap != nil {
				c.snap(&s)
			}
			w := worldFrom(t, s)
			if c.prep != nil {
				c.prep(t, w)
			}
			want(t, w.do(c.as, c.args...), c.exit, c.want...)
		})
	}
}

// ok runs a command that must succeed.
func (w *world) ok(t *testing.T, name string, args ...string) string {
	t.Helper()
	r := w.do(name, args...)
	if r.Exit != 0 {
		t.Fatalf("%s %v: exit %d\n%s", name, args, r.Exit, r.Output)
	}
	return r.Output
}

func setCard(id string, f func(*store.Card)) func(*store.Snapshot) {
	return func(s *store.Snapshot) {
		c := s.Cards[id]
		f(&c)
		s.Cards[id] = c
	}
}

func setRow(name string, f func(*store.Row)) func(*store.Snapshot) {
	return func(s *store.Snapshot) {
		r := s.Rows[name]
		f(&r)
		s.Rows[name] = r
	}
}

func both(fs ...func(*store.Snapshot)) func(*store.Snapshot) {
	return func(s *store.Snapshot) {
		for _, f := range fs {
			f(s)
		}
	}
}
