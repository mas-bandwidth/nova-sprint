package policy

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// betweenStore lands between, as one op by another actor, right before the
// first op of kind reaches the store: a write landing between a verb's read
// and its commit.
type betweenStore struct {
	store.Store
	kind    string
	between []store.Write
	done    bool
}

func (s *betweenStore) Apply(ctx context.Context, op store.Op) (store.Receipt, error) {
	if op.Kind == s.kind && !s.done {
		s.done = true
		r, err := s.Store.Apply(ctx, store.Op{ID: op.ID + "-between", Epoch: op.Epoch, Actor: "r1", Kind: "between", At: op.At, Writes: s.between})
		if err != nil || !r.Accepted {
			return r, fmt.Errorf("the write between was refused: %s: %v", r.Reason, err)
		}
	}
	return s.Store.Apply(ctx, op)
}

// TestPolicyVerbsFencedOnPoliciesNotTheSeq: up, set and activate were
// fenced on the store's seq, so a worker's beat between the read and the
// op refused them ("the store is at seq N+1, expected seq N"). Each is now
// fenced on the policies (and set and activate on the judgements) it read:
// a beat between and the verb lands; another version stored, or a
// judgement opened, between and it is still refused, naming it.
func TestPolicyVerbsFencedOnPoliciesNotTheSeq(t *testing.T) {
	t.Parallel()
	beat := store.SetRowLoad{Row: "r1", Load1Centi: 7, Cores: 8}
	data := store.PutPolicy{Kind: "data", Name: "setup:run-on", Body: "studio"}
	judged := store.PutJudgement{ID: "j9", Kind: "red"}
	verbs := []struct {
		name, kind string
		run        func(w *world, st store.Store) error
	}{
		{"up", "policy-set", func(w *world, st store.Store) error {
			_, err := Up(w.ctx(), st, w.caller())
			return err
		}},
		{"set", "policy-set", func(w *world, st store.Store) error {
			w.up()
			_, err := Set(w.ctx(), st, w.ro, testEngine(), w.caller(), w.file("f.star", DefaultBody()+"\n# f\n"), "test")
			return err
		}},
		{"activate", "policy-activate", func(w *world, st store.Store) error {
			w.up()
			v := w.set("f.star", DefaultBody()+"\n# f\n")
			_, err := Activate(w.ctx(), st, w.ro, testEngine(), w.caller(), v, "test")
			return err
		}},
	}
	for _, v := range verbs {
		t.Run(v.name+": a beat between", func(t *testing.T) {
			t.Parallel()
			w := fakeWorld(t)
			require.NoError(t, v.run(w, &betweenStore{Store: w.st, kind: v.kind, between: []store.Write{beat}}))
			assert.Equal(t, 7, w.snap().Rows["r1"].Load1Centi, "the beat landed between")
		})
		for _, m := range []struct {
			what  string
			w     store.Write
			wrote string
		}{{"a version", data, "wrote policies"}, {"a judgement", judged, "wrote judgements"}} {
			if v.name == "up" && m.what == "a judgement" {
				continue // up answers no judgement, and reads none
			}
			t.Run(v.name+": "+m.what+" between", func(t *testing.T) {
				t.Parallel()
				w := fakeWorld(t)
				err := v.run(w, &betweenStore{Store: w.st, kind: v.kind, between: []store.Write{m.w}})
				require.Error(t, err)
				assert.Contains(t, err.Error(), "the store changed since seq")
				assert.Contains(t, err.Error(), m.wrote)
			})
		}
	}
}
