package friend

import (
	toolsfriend "github.com/mas-bandwidth/nova-tools/pkg/friend"
	"strings"
	"sync/atomic"
)

type Pronouns = toolsfriend.Pronouns

const (
	SheHer          = toolsfriend.SheHer
	HeHim           = toolsfriend.HeHim
	TheyThem        = toolsfriend.TheyThem
	DefaultPronouns = toolsfriend.DefaultPronouns
)

var AllPronouns = toolsfriend.AllPronouns

func ParsePronouns(s string) (Pronouns, bool) { return toolsfriend.ParsePronouns(s) }
func PronounList() string                     { return toolsfriend.PronounList() }

// RowPronouns reads the friend row's pronouns off the beat's answer
// (row_pronouns=she/her, beside row_mode and row_width); ok is false when the
// answer carries none or names a set that is not one of the three.
func RowPronouns(answer string) (Pronouns, bool) {
	for _, w := range strings.Fields(answer) {
		if v, found := strings.CutPrefix(w, "row_pronouns="); found {
			p, ok := ParsePronouns(v)
			return p, ok && v != ""
		}
	}
	return DefaultPronouns, false
}

// PronounsRef holds a daemon's pronouns where several goroutines read them and
// a beat's answer may change them: the flag's (or the plist's) first, the row's
// once the beat names them. A nil ref and a ref never set are they/them.
type PronounsRef struct{ v atomic.Pointer[Pronouns] }

// NewPronounsRef is a ref holding p.
func NewPronounsRef(p Pronouns) *PronounsRef {
	r := &PronounsRef{}
	r.Set(p)
	return r
}

// Get is the pronouns held, they/them when none were set.
func (r *PronounsRef) Get() Pronouns {
	if r == nil {
		return DefaultPronouns
	}
	if p := r.v.Load(); p != nil {
		return *p
	}
	return DefaultPronouns
}

// Set replaces the pronouns held.
func (r *PronounsRef) Set(p Pronouns) {
	if r != nil {
		r.v.Store(&p)
	}
}

// say and sayf are Say and Sprintf with the pronouns the daemon holds (Pro):
// the friend row's, as the flag or the beat last named them, else they/them.
func (d *Daemon) say(s string) string { return d.Pro.Get().Say(s) }

func (d *Daemon) sayf(format string, a ...any) string { return d.Pro.Get().Sprintf(format, a...) }
