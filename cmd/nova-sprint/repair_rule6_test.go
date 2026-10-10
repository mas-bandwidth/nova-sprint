package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// proToMerging drives one pro card (two readers) to merging queued: dealt,
// worked, read ok by reader-a then reader-b, and accepted.
func (ta *testApp) proToMerging() {
	ta.t.Helper()
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 1 --one --brief-file " + proBriefFile(ta.t))
	ta.deal(1)
	ta.ok("take --as m1 --limit 100")
	var q struct{ Cards []queueCard }
	ta.json("queue --as m1", &q)
	require.NotEmpty(ta.t, q.Cards)
	ta.ok("finish --as m1 " + q.Cards[0].ID + "@" + fmt.Sprint(q.Cards[0].Gen))
	ta.ok("ask --limit 100")
	ta.ok("read --as reader-a --ok --limit 100")
	ta.ok("ask --limit 100")
	ta.ok("read --as reader-b --ok --limit 100")
	ta.ok("accept s1-1")
	ta.clean()
}

// shortReads leaves the merging primary with one ok read of the two it was
// accepted on: it records one reader in the primary's readers field and
// removes the other reader's ok read card from the readers table, as a writer
// outside the fence would, producing a rule 6 card.
func (ta *testApp) shortReads(id, keep string) {
	ta.t.Helper()
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now}
	s, err := st.Load(context.Background(), []string{sprint.Work, sprint.Readers}, nil)
	require.NoError(ta.t, err)
	c := s.Work.Placed(id)
	require.NotNil(ta.t, c, "no primary %s", id)
	var members []ntable.BatchMemberEntry
	members = append(members, ntable.BatchMemberEntry{ID: c.ID, Expect: &ntable.MemberExpect{Revision: fmt.Sprint(c.Rev)},
		Set: map[string]string{"readers": keep}})
	for _, rc := range s.Readers.Of(id) {
		if rc.Col == sprint.OK && rc.F("reader") != keep {
			members = append(members, ntable.BatchMemberEntry{ID: rc.ID, Expect: &ntable.MemberExpect{Revision: fmt.Sprint(rc.Rev)}, Remove: true})
		}
	}
	_, err = ta.m.Apply(context.Background(), ntable.BatchManifest{Schema: 1, Table: st.Names.Table(sprint.Work), Epoch: "0",
		ExpectedTableRevision: fmt.Sprint(s.Work.Revision), OperationID: "outside-short",
		Members: []ntable.BatchMemberEntry{members[0]}})
	require.NoError(ta.t, err)
	if len(members) > 1 {
		_, err = ta.m.Apply(context.Background(), ntable.BatchManifest{Schema: 1, Table: st.Names.Table(sprint.Readers), Epoch: "0",
			ExpectedTableRevision: fmt.Sprint(s.Readers.Revision), OperationID: "outside-short-readers",
			Members: members[1:]})
		require.NoError(ta.t, err)
	}
}

// rule6Violation says check judges the primary a rule 6 violation (fewer ok
// reads at its head than it needs).
func (ta *testApp) rule6Violation() []string {
	ta.t.Helper()
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(ta.t, err)
	rep, _, err := st.Check(context.Background(), 3)
	require.NoError(ta.t, err)
	var out []string
	for _, v := range rep.Violations {
		if v.Rule == 6 {
			out = append(out, v.String())
		}
	}
	return out
}

// A merging card accepted on two readers' ok reads but holding one is a rule 6
// judgment: repair --dry-run prints the move back to review, repair makes it,
// and the rule 6 judgment clears. (docs/SPEC-CARD.md: repair says what it will
// change and takes --dry-run; repair learns rule 6.)
func TestRepairReturnsARule6MergingCardToReview(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.proToMerging()
	// s1-1 holds both readers' ok reads at accept; drop one, leaving one of two.
	ta.shortReads("s1-1", "reader-a")
	require.NotEmpty(t, ta.rule6Violation(), "s1-1 with one read is not a rule 6 violation")

	code, out, errs := ta.do("repair --dry-run")
	require.Equal(t, 0, code, "repair --dry-run: %s%s", out, errs)
	require.Contains(t, out, "WOULD s1-1 merging -> review", "the dry run does not print the move back to review: %s%s", out, errs)
	require.Equal(t, sprint.Merging, ta.primary("s1-1").Col, "--dry-run moved the card: %s", out)

	code, out, errs = ta.do("repair")
	require.Equal(t, 0, code, "repair: %s%s", out, errs)
	require.Equal(t, sprint.Review, ta.primary("s1-1").Col, "repair did not move s1-1 back to review: %s%s", out, errs)
	require.Equal(t, "rule 6: ok reads at its head from fewer readers than it needs; returned to review for its missing read", ta.primary("s1-1").F("return_reason"), "the reason is not recorded: %s", out)
	require.Empty(t, ta.rule6Violation(), "the rule 6 judgment did not clear: %s%s", out, errs)
}

// With nothing to finish and no rule 6 card, repair refuses and names the
// check that shows what is broken, rather than saying OK with nothing done.
func TestRepairRefusesWhenItCanDoNothing(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.proToMerging()
	code, out, errs := ta.do("repair")
	require.NotZero(t, code, "repair said OK with nothing to do: %s%s", out, errs)
	require.Contains(t, errs, "nothing to repair", "the refusal does not name the reason: %s%s", out, errs)
	require.Contains(t, errs, strings.TrimSpace("nova-sprint check"), "the refusal does not name the remedy: %s%s", out, errs)
}

// repair --dry-run writes nothing: with an operation pending it prints the
// finish it would make and leaves the operation open, never finishing it
// (its help says --dry-run writes nothing).
func TestRepairDryRunDoesNotFinishThePendingOperation(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)
	ta.ok("take --as m1 s1-1.w1@1")
	// a finish cut after its first manifest applied leaves its operation open
	applied := 0
	ta.m.Fail = func(p string) error {
		if strings.HasPrefix(p, "apply ") && strings.HasSuffix(p, " before") {
			if applied++; applied > 1 {
				return errors.New("cut")
			}
		}
		return nil
	}
	code, out, errs := ta.bare("finish --as m1 s1-1.w1@1 --failed --report x")
	require.NotZero(t, code, "the cut finish: %s%s", out, errs)
	require.NotNil(t, ta.m.Pending(), "the cut left no operation open")
	ta.m.Fail = nil
	code, out, errs = ta.do("repair --dry-run")
	require.Equal(t, 0, code, "repair --dry-run: %s%s", out, errs)
	require.Contains(t, out, "WOULD finish OPERATION", "the dry run does not name the operation it would finish: %s%s", out, errs)
	require.NotNil(t, ta.m.Pending(), "--dry-run finished the pending operation: %s%s", out, errs)
}
