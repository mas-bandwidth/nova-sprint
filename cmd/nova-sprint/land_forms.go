package main

// land_forms.go is the lander's merge of a shared s-expression ledger form by form
// (docs/SPEC-SPRINT.md section 7, the shared ledgers; tla/LandLedgers.tla), the fallback when
// the line merge (landsexp.go, land_ledgers.go) refuses a stretch: two cards that each put
// their fix first in docs/fixes.sexp's :items change one line, though they add two forms.
// Each keyed list (a list whose members are forms with an id: (fix "id" ...), (release "id"
// ...)) is merged by id against the base: a form one side
// changed is that side's, one a side added is there, one a side removed is gone, and one both
// sides changed or added differently is a real conflict, refused naming it. A form both sides
// hold alike is kept once. The tip's text is kept as it stands and the card's added forms go
// after the tip's in its list, the tip's text between members (comments, blank lines) kept
// byte for byte. An evidence ledger (docs/roadmap-evidence/*.sexp) is append-only history,
// not one current form per task: java/C7 done at two tested revisions is two forms, both
// kept, so its members are keyed by their whole text and only exact duplicates are one. A
// file with no wrapping form (the schema repository's evidence ledgers, one (:id ...) form a
// line) is one list. The merged text is read back and must hold exactly the forms the merge
// wants, and every comment of the tip outside the spans it rewrote, or it is refused.

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
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
	// an evidence ledger is append-only history: one task's forms at several revisions all
	// stand, so a member is keyed by its whole canonical text, never by its id
	key := formKey
	if cardgen.EvidenceLedger(file) {
		key = formString
	}
	if strings.TrimSpace(stripComments(string(data))) == "" {
		return d, nil
	}
	// one wrapping (<kind> ...) form; a file of one bare (:id ...) form is read as a sequence
	if top, err := worklang.Read(file, data, landDataLimits); err == nil && top.Kind == worklang.List && len(top.List) > 0 && top.List[0].Kind == worklang.Symbol {
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
					l.items = append(l.items, keyedForm{key: key(it), canon: formString(it), off: it.Offset, end: it.End})
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
	top, err := worklang.Read(file, wrapped, landDataLimits)
	if err != nil {
		return d, err
	}
	if !keyedList(top) {
		return d, errors.New("a top-level form is not a list")
	}
	l := formList{off: 0, end: len(data), bare: true}
	for _, it := range top.List {
		l.items = append(l.items, keyedForm{key: key(it), canon: formString(it), off: it.Offset - 2, end: it.End - 2})
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
// merge (tla/LandLedgers.tla, ResolveLand).
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
	keys := unionKeys(b, o, t)
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
	// the edits on the tip's text: only the replaced and dropped members' own spans, and the
	// card's added members after the tip's last; every byte between members (comments,
	// blank lines) is the tip's, kept as it stands
	var edits []formEdit
	for _, s := range docs[1].scalars {
		if r, ok := replace[s.key]; ok {
			edits = append(edits, formEdit{s.off, s.end, r})
		}
	}
	data := docs[1].data
	var touched [][2]int // the tip's spans the merge rewrites, outside which no text may change
	listed := map[string]bool{}
	for _, l := range docs[1].lists {
		listed[l.name] = true
		last := -1 // the tip's last member the merge keeps
		for i, it := range l.items {
			k := l.name + "/" + it.key
			switch {
			case drop[k]:
				from, to := memberLines(data, it)
				edits = append(edits, formEdit{from, to, ""})
				touched = append(touched, [2]int{from, to})
			case replace[k] != "":
				edits = append(edits, formEdit{it.off, it.end, replace[k]})
				touched = append(touched, [2]int{it.off, it.end})
				last = i
			default:
				last = i
			}
		}
		var add []string
		for _, k := range added {
			if lists[2][k] == l.name {
				add = append(add, text(docs[2], t[k]))
			}
		}
		if len(add) == 0 {
			continue
		}
		switch {
		case len(l.items) == 0 && l.bare:
			body := strings.TrimRight(string(data), " \t\r\n")
			if body != "" {
				body += "\n"
			}
			edits = append(edits, formEdit{0, len(data), body + strings.Join(add, "\n") + "\n"})
			touched = append(touched, [2]int{0, len(data)})
		case len(l.items) == 0:
			edits = append(edits, formEdit{l.end - 1, l.end - 1, strings.Join(add, "\n  ")})
		case last < 0:
			return nil, fmt.Errorf("the card removes every entry of %s and adds others", cmp.Or(l.name, "the file"))
		default:
			sep := memberSep(data, l, last)
			at := l.items[last].end
			edits = append(edits, formEdit{at, at, sep + strings.Join(add, sep)})
		}
	}
	for _, k := range added {
		if !listed[lists[2][k]] {
			return nil, fmt.Errorf("the card adds %s to a list the tip lacks", trimList(k))
		}
	}
	slices.SortFunc(edits, func(a, b formEdit) int { return cmp.Or(b.off-a.off, b.end-a.end) })
	out := slices.Clone(data)
	for i, e := range edits {
		if i > 0 && e.end > edits[i-1].off {
			return nil, errors.New("two of the merge's edits overlap")
		}
		out = append(out[:e.off:e.off], append([]byte(e.text), out[e.end:]...)...)
	}
	// every comment of the tip outside the spans the merge rewrites is still there
	if c := lostComment(data, out, touched); c != "" {
		return nil, fmt.Errorf("the merged data loses the comment %q", c)
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
	for _, k := range unionKeys(got, toForms(want)) {
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

// formEdit is one splice of the tip's text: [off, end) becomes text.
type formEdit struct {
	off, end int
	text     string
}

// memberLines is a dropped member's span widened to its own lines: the indentation before
// it when only whitespace stands there on its line, and the spaces and the newline after it
// when nothing else follows on its line. What surrounds it (comments, blank lines) stays.
func memberLines(data []byte, it keyedForm) (from, to int) {
	from, to = it.off, it.end
	for to < len(data) && (data[to] == ' ' || data[to] == '\t' || data[to] == '\r') {
		to++
	}
	if to < len(data) && data[to] == '\n' {
		to++
		for from > 0 && (data[from-1] == ' ' || data[from-1] == '\t') {
			from--
		}
		return from, to
	}
	return it.off, it.end
}

// memberSep is the separator an added member takes after the tip's member i: the gap before
// member i when it holds no comment, else a newline and member i's indentation.
func memberSep(data []byte, l formList, i int) string {
	it := l.items[i]
	if i > 0 {
		if gap := string(data[l.items[i-1].end:it.off]); strings.Contains(gap, "\n") && !strings.Contains(gap, ";") {
			return gap
		}
	}
	ls := it.off
	for ls > 0 && (data[ls-1] == ' ' || data[ls-1] == '\t') {
		ls--
	}
	if ls == 0 || data[ls-1] == '\n' {
		return "\n" + string(data[ls:it.off])
	}
	if l.bare {
		return "\n"
	}
	return "\n  "
}

// commentLines is the ; comment lines of data outside the spans, each trimmed, with how
// many times each stands there.
func commentLines(data []byte, spans [][2]int) map[string]int {
	out := map[string]int{}
	inSpan := func(i int) bool {
		return slices.ContainsFunc(spans, func(s [2]int) bool { return i >= s[0] && i < s[1] })
	}
	for i := 0; i < len(data); {
		j := i
		for j < len(data) && data[j] != '\n' {
			j++
		}
		if line := strings.TrimSpace(string(data[i:j])); strings.HasPrefix(line, ";") && !inSpan(i) {
			out[line]++
		}
		i = j + 1
	}
	return out
}

// lostComment is a comment line of the tip (outside the rewritten spans) that the merged
// text holds fewer times than the tip, "" for none.
func lostComment(tip, merged []byte, touched [][2]int) string {
	want, got := commentLines(tip, touched), commentLines(merged, nil)
	for _, c := range slices.Sorted(maps.Keys(want)) {
		if got[c] < want[c] {
			return c
		}
	}
	return ""
}

// trimList is a merge key without its list name.
func trimList(k string) string {
	if _, rest, ok := strings.Cut(k, "/"); ok && !strings.HasPrefix(k, "top:") {
		return rest
	}
	return k
}

// toForms is a want map as forms, so unionKeys can take it with the read-back forms
// (tla/LandLedgers.tla, ResolveLand).
func toForms(m map[string]string) map[string]keyedForm {
	out := make(map[string]keyedForm, len(m))
	for k, v := range m {
		out[k] = keyedForm{key: k, canon: v}
	}
	return out
}
