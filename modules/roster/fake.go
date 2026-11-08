package roster

import (
	"context"
	"slices"
)

// Fake is a Source held in memory, for the tests of every layer that reads
// a roster: no network, no time. Err, when set, is returned by both reads.
type Fake struct {
	Ms  []Machine
	Fs  []Friend
	Err error
}

// Machines returns a copy of the fake's machines.
func (f Fake) Machines(context.Context) ([]Machine, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return slices.Clone(f.Ms), nil
}

// Friends returns a copy of the fake's friends.
func (f Fake) Friends(context.Context) ([]Friend, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return slices.Clone(f.Fs), nil
}
