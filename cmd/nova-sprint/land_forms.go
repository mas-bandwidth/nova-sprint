package main

// land_forms.go is the lander's merge of a shared s-expression ledger form by form
// (docs/SPEC-SPRINT.md section 7, the shared ledgers; tla/LandLedgers.tla), the fallback when
// the line merge (landsexp.go, land_ledgers.go) refuses a stretch: two cards that each put
// their fix first in docs/fixes.sexp's :items change one line, though they add two forms.
// Each keyed list (a list whose members are forms with an id: (fix "id" ...), (release "id"
// ...), (fixed "id" ...), or (:id "id" ...)) is merged by id against the base: a form one side
// changed is that side's, one a side added is there, one a side removed is gone, and one both
// sides changed or added differently is a real conflict, refused naming it. A form both sides
// hold alike is kept once. The tip's text is kept as it stands and the card's added forms go
// after the tip's in its list. A file with no wrapping form (the schema repository's
// evidence ledgers, one (:id ...) form a line) is one keyed list. The merged text is read
// back and must hold exactly the forms the merge wants, or it is refused.

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/roadmapdoc"
	"github.com/mas-bandwidth/nova-sprint/internal/worklang"
)

// keyedForm is one keyed member of a ledger: its key, its canonical text (formString), and
// its bytes in the file.
type keyedForm struct {
	key, canon string
	off, end   int
}

// formList is one keyed list of a ledger: its name (the keyword before it, "" for a file
// with no wrapping form), its bytes in the file, and its members in file order.
type formList struct {
	name     string
	off, end int // the list's own parentheses ([off, end)); for a bare file, the whole file
	bare     bool
	items    []keyedForm
}

// formDoc is a ledger read for a merge by form: its keyed lists and its scalar values
// (keyword values that are not keyed lists, keyed "top:<keyword>").
type formDoc struct {
	data    []byte
	lists   []formList
	scalars []keyedForm
}

// all is every keyed member and scalar of the doc by key; an id met twice is an error.
func (d formDoc) all() (map[string]keyedForm, map[string]string, error) {
	out, list := map[string]keyedForm{}, map[string]string{}
	for _, s := range d.scalars {
		if _, dup := out[s.key]; dup {
			return nil, nil, fmt.Errorf("%s is named twice", s.key)
		}
		out[s.key] = s
	}
	for _, l := range d.lists {
		for _, it := range l.items {
			k := l.name + "/" + it.key
			if prev, dup := out[k]; dup {
				if prev.canon == it.canon {
					continue // an exact duplicate is one form
				}
				return nil, nil, fmt.Errorf("%s is named twice", it.key)
			}
			out[k], list[k] = it, l.name
		}
	}
	return out, list, nil
}

// formKey is a keyed list member's key: (<kind> "<id>" ...) is <kind>:<id>, (:id "<id>" ...)
// is id:<id>, and any other form is its canonical text.
func formKey(f worklang.Form) string {
	if f.Kind == worklang.List && len(f.List) >= 2 {
		if f.List[0].Kind == worklang.Symbol && f.List[1].Kind == worklang.String {
			return f.List[0].Value + ":" + f.List[1].Value
		}
		for i := 0; i+1 < len(f.List); i++ {
			if f.List[i].Kind == worklang.Keyword && f.List[i].Value == "id" && f.List[i+1].Kind == worklang.String {
				return "id:" + f.List[i+1].Value
			}
		}
	}
	return formString(f)
}

// keyedList says a list value is a keyed list: every member is a list with a key.
func keyedList(f worklang.Form) bool {
	if f.Kind != worklang.List {
		return false
	}
	for _, it := range f.List {
		if it.Kind != worklang.List {
			return false
		}
	}
	return true
}

// readFormDoc reads a ledger for a merge by form: one wrapping form whose keyword values are
// keyed lists or scalars, or a bare sequence of keyed forms (no wrapping form). Empty data
// (a side that does not hold the file) is a doc with nothing in it.
func readFormDoc(file string, data []byte) (formDoc, error) {
	d := formDoc{data: data}
	if strings.TrimSpace(stripComments(string(data))) == "" {
		return d, nil
	}
	// one wrapping (<kind> ...) form; a file of one bare (:id ...) form is read as a sequence
	if top, err := worklang.Read(file, data, roadmapdoc.Limits); err == nil && top.Kind == worklang.List && len(top.List) > 0 && top.List[0].Kind == worklang.Symbol {
		for i := 1; i < len(top.List); i++ {
			kw := top.List[i]
			if kw.Kind != worklang.Keyword || i+1 >= len(top.List) {
				continue
			}
			v := top.List[i+1]
			i++
			if v.Kind == worklang.List && keyedList(v) {
				l := formList{name: kw.Value, off: v.Offset, end: v.End}
				for _, it := range v.List {
					l.items = append(l.items, keyedForm{key: formKey(it), canon: formString(it), off: it.Offset, end: it.End})
				}
				d.lists = append(d.lists, l)
				continue
			}
			d.scalars = append(d.scalars, keyedForm{key: "top:" + kw.Value, canon: formString(v), off: v.Offset, end: v.End})
		}
		return d, nil
	}
	// a bare sequence of forms: read as one list, the offsets taken back to the file's
	wrapped := append(append([]byte("(\n"), data...), []byte("\n)")...)
	top, err := worklang.Read(file, wrapped, roadmapdoc.Limits)
	if err != nil {
		return d, err
	}
	if !keyedList(top) {
		return d, errors.New("a top-level form is not a list")
	}
	l := formList{off: 0, end: len(data), bare: true}
	for _, it := range top.List {
		l.items = append(l.items, keyedForm{key: formKey(it), canon: formString(it), off: it.Offset - 2, end: it.End - 2})
	}
	d.lists = append(d.lists, l)
	return d, nil
}

// stripComments is s without its ; comments (outside strings), for the empty-file test.
func stripComments(s string) string {
	var b strings.Builder
	in, comment := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case comment:
			if c == '\n' {
				comment = false
			}
			continue
		case in && c == '\\' && i+1 < len(s):
			b.WriteByte(c)
			i++
			c = s[i]
		case c == '"':
			in = !in
		case !in && c == ';':
			comment = true
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// mergeByForm is a shared s-expression ledger merged form by form: base (nil when the base
// does not hold the file), the tip's side (ours) and the card's (theirs). It is the tip's
// text with the card's changes, additions and removals applied, or why the sides do not
// merge.
func mergeByForm(file string, base, ours, theirs []byte) ([]byte, error) {
	var docs [3]formDoc
	var forms [3]map[string]keyedForm
	var lists [3]map[string]string
	for i, side := range [][]byte{base, ours, theirs} {
		d, err := readFormDoc(file, side)
		if err != nil {
			return nil, fmt.Errorf("a side does not read as forms: %s", oneLineErr(err))
		}
		m, l, err := d.all()
		if err != nil {
			return nil, fmt.Errorf("a side does not read as forms: %s", oneLineErr(err))
		}
		docs[i], forms[i], lists[i] = d, m, l
	}
	b, o, t := forms[0], forms[1], forms[2]
	text := func(d formDoc, f keyedForm) string { return string(d.data[f.off:f.end]) }
	replace := map[string]string{} // a tip key whose text becomes the card's
	drop := map[string]bool{}      // a tip key the card removed
	var added []string             // card keys the tip lacks, in the card's order
	want := map[string]string{}    // key -> canonical text the merged file must hold
	keys := unionKeysForms(b, o, t)
	for _, k := range keys {
		bv, inB := b[k]
		ov, inO := o[k]
		tv, inT := t[k]
		switch {
		case inB && inO && inT:
			switch {
			case ov.canon == bv.canon && tv.canon != bv.canon:
				replace[k], want[k] = text(docs[2], tv), tv.canon
			case tv.canon == bv.canon || tv.canon == ov.canon:
				want[k] = ov.canon
			default:
				return nil, fmt.Errorf("both sides change %s", trimList(k))
			}
		case inB:
			if inO && ov.canon != bv.canon || inT && tv.canon != bv.canon {
				return nil, fmt.Errorf("one side removes %s and the other changes it", trimList(k))
			}
			if inO {
				drop[k] = true
			}
		case inO && inT && ov.canon != tv.canon:
			return nil, fmt.Errorf("both sides add %s, differently", trimList(k))
		case inO:
			want[k] = ov.canon
		default:
			want[k] = tv.canon
		}
	}
	// the card's additions, in its order
	for _, l := range docs[2].lists {
		for _, it := range l.items {
			k := l.name + "/" + it.key
			if _, inO := o[k]; inO || slices.Contains(added, k) {
				continue
			}
			if _, inB := b[k]; inB {
				continue
			}
			added = append(added, k)
		}
	}
	for _, s := range docs[2].scalars {
		if _, inO := o[s.key]; !inO {
			if _, inB := b[s.key]; !inB {
				return nil, fmt.Errorf("the card adds %s, which the tip lacks", s.key)
			}
		}
	}
	// the edits on the tip's text, each list rebuilt whole, applied from the back
	type edit struct {
		off, end int
		text     string
	}
	var edits []edit
	for _, s := range docs[1].scalars {
		if r, ok := replace[s.key]; ok {
			edits = append(edits, edit{s.off, s.end, r})
		}
	}
	data := docs[1].data
	listed := map[string]bool{}
	for _, l := range docs[1].lists {
		listed[l.name] = true
		var items []string
		changed := false
		for _, it := range l.items {
			k := l.name + "/" + it.key
			switch {
			case drop[k]:
				changed = true
			case replace[k] != "":
				items, changed = append(items, replace[k]), true
			default:
				items = append(items, string(data[it.off:it.end]))
			}
		}
		for _, k := range added {
			if lists[2][k] == l.name {
				items, changed = append(items, text(docs[2], t[k])), true
			}
		}
		if !changed {
			continue
		}
		head, sep, foot := listFrame(data, l)
		edits = append(edits, edit{l.off, l.end, head + strings.Join(items, sep) + foot})
	}
	for _, k := range added {
		if !listed[lists[2][k]] {
			return nil, fmt.Errorf("the card adds %s to a list the tip lacks", trimList(k))
		}
	}
	slices.SortFunc(edits, func(a, b edit) int { return b.off - a.off })
	out := slices.Clone(data)
	for _, e := range edits {
		out = append(out[:e.off:e.off], append([]byte(e.text), out[e.end:]...)...)
	}
	// the merged text, read back, holds exactly what the merge wants
	d, err := readFormDoc(file, out)
	if err != nil {
		return nil, fmt.Errorf("the merged data does not read: %s", oneLineErr(err))
	}
	got, _, err := d.all()
	if err != nil {
		return nil, fmt.Errorf("the merged data does not read: %s", oneLineErr(err))
	}
	for _, k := range unionKeysForms(got, toForms(want)) {
		g, inG := got[k]
		w, inW := want[k]
		switch {
		case !inG:
			return nil, fmt.Errorf("the merged data lacks %s", trimList(k))
		case !inW:
			return nil, fmt.Errorf("the merged data holds %s, which a side removed", trimList(k))
		case g.canon != w:
			return nil, fmt.Errorf("the merged data changes %s", trimList(k))
		}
	}
	return out, nil
}

// listFrame is what a rebuilt list keeps of the tip's: the text before its first member,
// the separator between members, and the text after its last.
func listFrame(data []byte, l formList) (head, sep, foot string) {
	sep = "\n  "
	if l.bare {
		sep = "\n"
	}
	switch n := len(l.items); {
	case n == 0 && l.bare:
		body := strings.TrimRight(string(data[l.off:l.end]), " \t\r\n")
		if body != "" {
			body += "\n"
		}
		return body, sep, "\n"
	case n == 0:
		return string(data[l.off : l.end-1]), sep, string(data[l.end-1 : l.end])
	case n > 1:
		sep = string(data[l.items[n-2].end:l.items[n-1].off])
	}
	return string(data[l.off:l.items[0].off]), sep, string(data[l.items[len(l.items)-1].end:l.end])
}

// trimList is a merge key without its list name.
func trimList(k string) string {
	if _, rest, ok := strings.Cut(k, "/"); ok && !strings.HasPrefix(k, "top:") {
		return rest
	}
	return k
}

// unionKeysForms is the keys of the maps, sorted, each once.
func unionKeysForms(ms ...map[string]keyedForm) []string {
	var out []string
	for _, m := range ms {
		for k := range m {
			if !slices.Contains(out, k) {
				out = append(out, k)
			}
		}
	}
	slices.Sort(out)
	return out
}

// toForms is a want map as forms, for unionKeysForms.
func toForms(m map[string]string) map[string]keyedForm {
	out := make(map[string]keyedForm, len(m))
	for k, v := range m {
		out[k] = keyedForm{key: k, canon: v}
	}
	return out
}
