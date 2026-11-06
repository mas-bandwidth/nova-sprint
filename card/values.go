package card

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Layer 1's value rules, applied as refusals before a plan leaves the
// machine, so no plan it emits is ErrInvalid (the first cold read of layer 2,
// D10): every key names something and fits store.MaxName bytes; every text is
// valid UTF-8 with no NUL; every count fits a PostgreSQL integer, a cost a
// bigint, at or above 0. The rules mirror store/check.go, which exports none.

// vals collects the first value a plan breaks.
type vals struct{ r *Refusal }

func (v *vals) fail(format string, args ...any) {
	if v.r == nil {
		v.r = no(format, args...)
	}
}

func (v *vals) text(what string, ss ...string) {
	for _, s := range ss {
		if !utf8.ValidString(s) || strings.IndexByte(s, 0) >= 0 {
			v.fail("%s %q is not valid UTF-8 or holds a NUL byte", what, short(s))
		}
	}
}

func (v *vals) name(what string, ss ...string) {
	for _, s := range ss {
		if len(s) > store.MaxName {
			v.fail("%s %q is over %d bytes", what, short(s), store.MaxName)
		}
	}
	v.text(what, ss...)
}

// key is a name that must not be empty: the record a write or condition is
// about.
func (v *vals) key(table string, ks ...string) {
	for _, k := range ks {
		if k == "" {
			v.fail("a %s needs a name", table)
		}
	}
	v.name(table, ks...)
}

func (v *vals) num(what string, n int) {
	if n < 0 || n > math.MaxInt32 {
		v.fail("%s %d is outside the range 0..%d", what, n, math.MaxInt32)
	}
}

func short(s string) string {
	if len(s) > store.MaxName {
		return s[:store.MaxName] + "..."
	}
	return s
}

// checked is plan, or the first layer 1 value rule it breaks.
func checked(p Plan) (Plan, *Refusal) {
	v := &vals{}
	for _, e := range p.Expect {
		v.expect(e)
	}
	for _, w := range p.Writes {
		v.write(w)
	}
	if v.r != nil {
		return Plan{}, v.r
	}
	return p, nil
}

func (v *vals) expect(e store.Expect) {
	switch x := e.(type) {
	case store.CardGen:
		v.key("card", x.Card)
	case store.CardState:
		v.key("card", x.Card)
	case store.CardAbsent:
		v.key("card", x.Card)
	case store.NeedsLanded:
		v.key("card", x.Card)
	case store.RowGen:
		v.key("row", x.Row)
	case store.RowIs:
		v.key("row", x.Row)
	case store.RowHeld:
		v.key("row", x.Row)
	case store.StreamHeld:
		v.key("stream", x.Stream)
	default:
		v.fail("condition %T is not one the machine makes", e)
	}
}

func (v *vals) write(w store.Write) {
	switch x := w.(type) {
	case store.PutCard:
		v.putCard(x)
	case store.SetStreamHeld:
		v.key("stream", x.Stream)
	case store.SetRowState:
		v.key("row", x.Row)
	case store.SetRowWidth:
		v.key("row", x.Row)
		v.num("width", x.Width)
	case store.SetRowFlaps:
		v.key("row", x.Row)
		v.num("flaps", x.Flaps)
	case store.SetRowHeld:
		v.key("row", x.Row)
		v.text("reason", x.Reason)
	default:
		v.cardWrite(w)
	}
}

func (v *vals) putCard(x store.PutCard) {
	v.key("card", x.ID)
	v.key("stream", x.Stream)
	v.name("card", x.Of, x.Holder)
	v.key("card", x.Needs...)
	v.key("card", x.After...)
	v.text("text", x.Brief, x.Prose, x.Finding)
	v.text("path", x.Paths...)
	v.text("done-when", x.DoneWhen...)
	v.num("attempt", x.Attempt)
	v.num("review", x.Review)
	v.num("score", x.Score)
	if !slices.Contains(store.Tiers, x.Tier) {
		v.fail("tier %q is not one of %s", short(x.Tier), orList(store.Tiers))
	}
}

// cardWrite checks the set-card writes: the card's key, then the value.
func (v *vals) cardWrite(w store.Write) {
	id, check := cardValue(w)
	if check == nil {
		v.fail("write %s is not one the machine makes", fmt.Sprintf("%T", w))
		return
	}
	v.key("card", id)
	check(v)
}

func cardValue(w store.Write) (string, func(v *vals)) {
	switch x := w.(type) {
	case store.SetCardState:
		return x.Card, func(*vals) {}
	case store.SetCardReview:
		return x.Card, func(*vals) {}
	case store.SetCardResult:
		return x.Card, func(*vals) {}
	case store.SetCardPriority:
		return x.Card, func(*vals) {}
	case store.SetCardReason:
		return x.Card, func(v *vals) { v.text("reason", x.Reason) }
	case store.SetCardFinding:
		return x.Card, func(v *vals) { v.text("finding", x.Finding) }
	case store.SetCardHead:
		return x.Card, func(v *vals) { v.text("head", x.Head) }
	case store.SetCardBranch:
		return x.Card, func(v *vals) { v.text("branch", x.Branch) }
	case store.SetCardBaseSHA:
		return x.Card, func(v *vals) { v.text("base", x.SHA) }
	case store.SetCardPriorHead:
		return x.Card, func(v *vals) { v.text("prior head", x.Head) }
	case store.SetCardAuthor:
		return x.Card, func(v *vals) { v.name("author", x.Author) }
	case store.SetCardProse:
		return x.Card, func(v *vals) { v.text("prose", x.Prose) }
	case store.SetCardPaths:
		return x.Card, func(v *vals) { v.text("path", x.Paths...) }
	case store.SetCardDoneWhen:
		return x.Card, func(v *vals) { v.text("done-when", x.DoneWhen...) }
	case store.SetCardNeeds:
		return x.Card, func(v *vals) { v.key("card", x.Needs...) }
	case store.SetCardScore:
		return x.Card, func(v *vals) { v.num("score", x.Score) }
	case store.SetCardAttempt:
		return x.Card, func(v *vals) { v.num("attempt", x.Attempt) }
	case store.SetCardBounces:
		return x.Card, func(v *vals) { v.num("bounces", x.Bounces) }
	case store.SetCardCost:
		return x.Card, func(v *vals) {
			if x.Cost < 0 {
				v.fail("cost %d is below 0", x.Cost)
			}
		}
	}
	return "", nil
}
