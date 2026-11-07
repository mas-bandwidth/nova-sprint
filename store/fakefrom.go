package store

import (
	"fmt"
	"maps"
)

// NewFakeFrom is a Fake whose state is s: its streams, cards, rows,
// judgements and cursors, at its epoch, with the next event at s.Seq+1, and
// policies, every policy version the store holds (Policy for 1, 2, ... until
// ErrNotFound), the one s.Policy names active. A plan built from s can be
// applied to it as a dry run with the Fake's checks, which are PG's
// (conformance), and its Snapshot equals s. The log before s is not carried:
// Events starts after s.Seq, and Replay of an op id from before it reports
// unknown, so a dry run cannot see that an id was already used. It is
// ErrInvalid when s is not a snapshot (an epoch below 1, a negative seq, a
// map key that is not its entry's name, a policy version below 1 or given
// twice, or an active version that is not given).
func NewFakeFrom(s Snapshot, policies ...PolicyVersion) (*Fake, error) {
	if s.Epoch < 1 || s.Seq < 0 {
		return nil, fmt.Errorf("%w: a snapshot at epoch %d and seq %d (epochs start at 1, seqs at 0)", ErrInvalid, s.Epoch, s.Seq)
	}
	f := NewFake()
	f.epoch, f.seq0 = s.Epoch, s.Seq
	if err := from(f.streams, s.Streams, s.Epoch, "stream", func(v Stream) string { return v.Name }); err != nil {
		return nil, err
	}
	if err := from(f.cards, s.Cards, s.Epoch, "card", func(v Card) string { return v.ID }); err != nil {
		return nil, err
	}
	if err := from(f.rows, s.Rows, s.Epoch, "row", func(v Row) string { return v.Name }); err != nil {
		return nil, err
	}
	if err := from(f.judges, s.Judgements, s.Epoch, "judgement", func(v Judgement) string { return v.ID }); err != nil {
		return nil, err
	}
	for k, c := range f.cards {
		f.cards[k] = cardCopy(c) // the Fake's own lists: editing s never changes it
	}
	maps.Copy(f.cursors, s.Cursors)
	for _, p := range policies {
		if _, twice := f.policies[p.Version]; twice || p.Version < 1 {
			return nil, fmt.Errorf("%w: policy version %d is below 1 or given twice", ErrInvalid, p.Version)
		}
		p.Active = p.Version == s.Policy
		f.policies[p.Version] = p
	}
	if _, ok := f.policies[s.Policy]; s.Policy != 0 && !ok {
		return nil, fmt.Errorf("%w: the snapshot's active policy version %d is not among the versions given", ErrInvalid, s.Policy)
	}
	return f, nil
}

// from copies one table of a snapshot into the Fake's, keyed at epoch, and
// refuses an entry whose map key is not its own name.
func from[V any](dst map[key]V, src map[string]V, epoch int, what string, name func(V) string) error {
	for k, v := range src {
		if name(v) != k {
			return fmt.Errorf("%w: the snapshot's %s %q is filed under %q", ErrInvalid, what, name(v), k)
		}
		dst[key{epoch, k}] = v
	}
	return nil
}
