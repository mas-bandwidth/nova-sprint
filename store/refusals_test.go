package store

import (
	"cmp"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refusalForms is the refusal inventory (L1 resecure item 15): every form a
// refused receipt's reason can take, one per format the rules write it
// with. TestRefusalInventoryIsComplete holds it to the code, and
// RunConformance fails a store whose suite logs no refusal of some form, so
// TestFunctionalConformance proves each one on PostgreSQL. ErrInvalid is
// not a refusal (nothing is logged) and neither is a Migrate or CheckSchema
// error; neither is listed.
var refusalForms = []string{
	// the epoch and the conditions (apply.go)
	"the op is for epoch %d, the store is at epoch %d: take a new snapshot",
	"card %s is at gen %d, expected gen %d",
	"card %s is %s, expected %s",
	"card %s has holder %s, expected %s",
	"card %s exists",
	"card %s does not exist",
	"need %s of card %s is %s, not landed",
	"row %s is %s, expected %s",
	"row %s is at gen %d, expected gen %d",
	"row %s does not exist",
	"stream %s is %s, expected %s",
	"stream %s does not exist",
	"judgement %s is %s, expected open",
	"judgement %s does not exist",
	"the sprint is stopped (%s): nova-sprint start",
	"the store is at seq %d, expected seq %d",
	// a set write that changes nothing (writes.go, ops.go)
	"%s %s is already %s",
	"%s %s already has the given %s",
	"stream %s is already %s",
	"stream %s's %s is already %q",
	"row %s already has the given load",
	"row %s is already %s",
	"cursor %s is already at %d",
	"policy version %d is already active",
	"the sprint already commits as %s <%s>",
	"the sprint is already %s",
	// the writes' rules (writes.go, ops.go)
	"row %s is hooked by session %s; unhook it first",
	"card %s is of kind %s; priority is a producer's",
	"card %s is of kind %s; %s is only for %s",
	"card %s is of kind sentinel; SetCardRed is only for a producer, and a sentinel is never read",
	"card %s is of kind %s; SetCardRed is only for a producer: record the verdict on its producer %s",
	"card %s is a %s; a %s never has a holder",
	"card %s is a consumer; a consumer names its holder row at add, and it never changes",
	"card %s is a consumer; a consumer has no %s",
	"card %s cannot %s",
	"card %s names %s %s twice",
	"%s %s of card %s does not exist",
	"card %s is a sentinel; a sentinel has no brief",
	"card %s is a %s; its attempt starts at 0 and is raised by SetCardAttempt",
	"card %s is a %s; only a work consumer is cut with a finding (a producer's is set by SetCardFinding)",
	"card %s is a %s; only a consumer is cut with a model (the model its deal named)",
	"card %s is a %s; only a read consumer is cut at a review (a producer's starts at 0 and is raised by SetCardReview)",
	"card %s is a %s; a %s has no of",
	"card %s is a consumer; a consumer names its producer in of",
	"card %s is a consumer of %s, which does not exist",
	"card %s is a consumer of %s, which is not a producer",
	"card %s is in stream %s, its producer %s is in stream %s; a consumer takes its producer's stream",
	"card %s is tier %s, its producer %s is tier %s; a consumer takes its producer's tier",
	"card %s is cut with finding %q, its producer %s has %q; a work consumer takes its producer's finding",
	"card %s is cut at review %d, its producer %s is at review %d; a read takes its producer's review",
	"card %s is at review %d, the most there is",
	"judgement %s exists",
	"judgement %s is %s; only an open judgement can be %s",
	"judgement %s opened at %s, cannot close at %s",
	"cursor %s is at %d, cannot move back to %d",
	"cursor %s cannot pass the log at %d",
	"policy version %d does not exist",
}

// refusalFiles are the files that write refusal reasons: the rules Fake and
// PG share. Every reason is a when or fmt.Sprintf format there.
var refusalFiles = []string{"apply.go", "writes.go", "ops.go"}

// composed are the formats whose reason is composed with a spec's words:
// spec.is is "is already <state>".
var composed = map[string]string{"%s %s %s": "%s %s is already %s"}

// TestRefusalInventoryIsComplete: the formats in refusalFiles and the
// inventory are one set, so a new refusal cannot ship without a form here
// (and so a case on PostgreSQL) and a deleted one leaves no stale form.
func TestRefusalInventoryIsComplete(t *testing.T) {
	var code []string
	for _, name := range refusalFiles {
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		require.NoError(t, err)
		ast.Inspect(f, func(n ast.Node) bool {
			if format, ok := reasonFormat(n); ok {
				code = append(code, cmp.Or(composed[format], format))
			}
			return true
		})
	}
	assert.ElementsMatch(t, slices.Compact(slices.Sorted(slices.Values(code))), refusalForms)
}

// reasonFormat is the format literal of a when or fmt.Sprintf call.
func reasonFormat(n ast.Node) (string, bool) {
	c, ok := n.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	i := -1
	switch fn := c.Fun.(type) {
	case *ast.Ident:
		if fn.Name == "when" {
			i = 1
		}
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok && x.Name == "fmt" && fn.Sel.Name == "Sprintf" {
			i = 0
		}
	}
	if i < 0 || len(c.Args) <= i {
		return "", false
	}
	lit, ok := c.Args[i].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// refusedSeen is the refusal reasons a conformance run logged.
type refusedSeen struct {
	mu      sync.Mutex
	reasons map[string]bool
	ran     int
}

// keep adds the refusals in h's log once its scenario has run.
func (r *refusedSeen) keep(h *harness) {
	h.t.Helper()
	var seq int64
	for {
		events, err := h.s.Events(h.ctx, seq, MaxEvents)
		require.NoError(h.t, err)
		if len(events) == 0 {
			break
		}
		r.mu.Lock()
		for _, e := range events {
			if !e.Accepted {
				r.reasons[e.Reason] = true
			}
		}
		r.mu.Unlock()
		seq = events[len(events)-1].Seq
	}
	r.mu.Lock()
	r.ran++
	r.mu.Unlock()
}

// check fails t for each form no logged reason took, once all scenarios
// ran (a -run that picks some of them proves nothing either way).
func (r *refusedSeen) check(t *testing.T, scenarios int) {
	if r.ran != scenarios {
		return
	}
	covered := map[string]bool{}
	for reason := range r.reasons {
		forms := formsOf(reason)
		if len(forms) == 0 {
			t.Errorf("refusal %q has no form in refusalForms", reason)
		}
		for _, f := range forms {
			covered[f] = true
		}
	}
	for _, f := range refusalForms {
		if !covered[f] {
			t.Errorf("no conformance scenario logs a refusal of the form %q", f)
		}
	}
}

var verb = regexp.MustCompile(`%[sdq]`)

// formsOf is the forms reason takes: of the forms it matches, those with
// the most fixed text, so "row r1 is already up" is "row %s is already %s"
// and not also "%s %s is already %s".
func formsOf(reason string) []string {
	var best []string
	most := -1
	for _, f := range refusalForms {
		if !formRegexp(f).MatchString(reason) {
			continue
		}
		fixed := len(verb.ReplaceAllString(f, ""))
		switch {
		case fixed > most:
			best, most = []string{f}, fixed
		case fixed == most:
			best = append(best, f)
		}
	}
	return best
}

// formRegexp is a form as an anchored pattern: %d a number, %q a quoted
// string, %s any text.
func formRegexp(f string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	last := 0
	for _, m := range verb.FindAllStringIndex(f, -1) {
		b.WriteString(regexp.QuoteMeta(f[last:m[0]]))
		b.WriteString(map[string]string{"%s": `.*`, "%d": `-?[0-9]+`, "%q": `".*"`}[f[m[0]:m[1]]])
		last = m[1]
	}
	b.WriteString(regexp.QuoteMeta(f[last:]) + "$")
	return regexp.MustCompile(b.String())
}

// fullReview is the conformance scenario of the one refusal no op can
// reach from an empty store: a producer at the most review its column
// holds (a restored store can hold one). Each store puts it there by hand,
// outside the log, and the next SetCardReview is refused.
func fullReview(h *harness) {
	h.seed()
	s, ok := h.s.(interface {
		forceReview(ctx context.Context, card string, review int) error
	})
	require.True(h.t, ok, "every store can be put at a full review by its test")
	require.NoError(h.t, s.forceReview(h.ctx, "c1", math.MaxInt32))
	h.refused(h.op("review", nil, SetCardReview{"c1"}), "card c1 is at review 2147483647, the most there is")
}

// forceReview sets a card's review outside the log, for fullReview only.
func (f *Fake) forceReview(_ context.Context, card string, review int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := key{f.epoch, card}
	c := f.cards[k]
	c.Review = review
	f.cards[k] = c
	return nil
}

// forceReview is the Fake's, by SQL.
func (p *PG) forceReview(ctx context.Context, card string, review int) error {
	_, err := p.pool.Exec(ctx, "UPDATE sprint.cards SET review = $1 WHERE id = $2 AND epoch = (SELECT n FROM sprint.epoch WHERE id = 1)", review, card)
	return err
}
