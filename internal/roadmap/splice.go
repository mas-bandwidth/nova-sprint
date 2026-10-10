package roadmap

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/roadmapdoc"
	"github.com/mas-bandwidth/nova-sprint/internal/worklang"
)

// The edits below read the file through worklang, whose forms carry each one's
// first byte and the byte past its last, and splice only the bytes of the
// records they change. Every byte outside those spans is kept.

// top reads f's one form.
func top(f *file) (worklang.Form, error) {
	return worklang.Read(f.rel, f.data, roadmapdoc.Limits)
}

// value returns the value form of :key in a record or the top form, and its
// index in the record's list; -1 when the key is absent.
func value(rec worklang.Form, key string) (worklang.Form, int) {
	for i := 1; i+1 < len(rec.List); i++ {
		if rec.List[i].Kind == worklang.Keyword && rec.List[i].Value == key {
			return rec.List[i+1], i + 1
		}
	}
	return worklang.Form{}, -1
}

// record finds the record whose identity is id in the list :list holds, and
// its index in that list.
func record(t worklang.Form, list, id string) (worklang.Form, worklang.Form, int, error) {
	l, at := value(t, list)
	if at < 0 || l.Kind != worklang.List {
		return worklang.Form{}, worklang.Form{}, -1, fmt.Errorf("no :%s list to find %s in", list, id)
	}
	for i, r := range l.List {
		if r.Kind == worklang.List && len(r.List) > 1 && r.List[1].Kind == worklang.String && r.List[1].Value == id {
			return l, r, i, nil
		}
	}
	return worklang.Form{}, worklang.Form{}, -1, fmt.Errorf("unknown id %q in :%s", id, list)
}

func (f *file) splice(start, end int, with string) {
	out := make([]byte, 0, len(f.data)-(end-start)+len(with))
	out = append(out, f.data[:start]...)
	out = append(out, with...)
	out = append(out, f.data[end:]...)
	f.data, f.changed = out, true
}

// appendRecord puts rec after the last record of :list, on its own line; a
// list that is empty becomes (rec), and an absent list is added at the end of
// the top form.
func (s *Set) appendRecord(f *file, list, rec string) error {
	if !f.present {
		return fmt.Errorf("this repository has no %s", f.rel)
	}
	t, err := top(f)
	if err != nil {
		return err
	}
	l, at := value(t, list)
	switch {
	case at < 0:
		f.splice(t.End-1, t.End-1, "\n :"+list+"\n ("+rec+")")
	case l.Kind != worklang.List:
		return fmt.Errorf("%s: :%s is not a list", f.rel, list)
	case len(l.List) == 0:
		f.splice(l.Offset, l.End, "("+rec+")")
	default:
		last := l.List[len(l.List)-1]
		f.splice(last.End, last.End, "\n  "+rec)
	}
	return nil
}

// removeRecord takes the record id out of :list with the blank before it; the
// list's first record takes the blank after it instead, so the list's opening
// parenthesis meets the next record.
func (s *Set) removeRecord(f *file, list, id string) error {
	t, err := top(f)
	if err != nil {
		return err
	}
	l, r, i, err := record(t, list, id)
	if err != nil {
		return err
	}
	switch {
	case len(l.List) == 1:
		f.splice(l.Offset, l.End, "()")
	case i == 0:
		f.splice(r.Offset, l.List[1].Offset, "")
	default:
		f.splice(l.List[i-1].End, r.End, "")
	}
	return nil
}

// setKey sets :key of record id in :list to the string v: the value's bytes
// are replaced, or the key is added before the record's closing parenthesis.
func (s *Set) setKey(f *file, list, id, key, v string) error {
	t, err := top(f)
	if err != nil {
		return err
	}
	_, r, _, err := record(t, list, id)
	if err != nil {
		return err
	}
	if old, at := value(r, key); at >= 0 {
		f.splice(old.Offset, old.End, quote(v))
		return nil
	}
	f.splice(r.End-1, r.End-1, "\n   :"+key+" "+quote(v))
	return nil
}

// quote writes s as a worklang string: a double quote and a backslash are
// escaped, nothing else.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// itemRecord is a new roadmap item, dated today.
func itemRecord(it Item, today string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "(item %s :group %s :date %s\n   :title %s\n   :text %s", quote(it.ID), quote(it.Group), quote(today), quote(flat(it.Title)), quote(flat(it.Text)))
	if it.Why != "" {
		fmt.Fprintf(&b, "\n   :why %s", quote(flat(it.Why)))
	}
	if it.Origin != "" {
		fmt.Fprintf(&b, "\n   :origin %s", quote(flat(it.Origin)))
	}
	return b.String() + ")"
}

// fixRecord is a new fix.
func fixRecord(fx Fix) string {
	var b strings.Builder
	fmt.Fprintf(&b, "(fix %s :release %s :status %s\n   :title %s", quote(fx.ID), quote(fx.Release), quote(fx.Status), quote(flat(fx.Title)))
	if t := flat(fx.Text); t != "" {
		fmt.Fprintf(&b, "\n   :text %s", quote(t))
	}
	fmt.Fprintf(&b, "\n   :origin %s)", quote(flat(fx.Origin)))
	return b.String()
}
