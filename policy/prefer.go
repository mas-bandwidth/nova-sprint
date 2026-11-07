package policy

import (
	"fmt"
	"strings"
	"unsafe"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// prefer(candidates, rank) is the default's choice of rows, in Go so that a
// tick's rows work is linear in its producers, not producers x rows: the
// names of the candidates at rank and up, nearest first, then those below
// it, highest first; those with room (working < width) first, every one
// when none has room. Candidates come by rank, lowest first, then by name.
// It is memoised for the thread (one version's tick) per candidate list
// and rank, and the list it returns is frozen.
func prefer(th *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cands starlark.Tuple
	var rank string
	if err := starlark.UnpackPositionalArgs("prefer", args, kwargs, 2, &cands, &rank); err != nil {
		return nil, err
	}
	if rung(rank) < 0 {
		return nil, fmt.Errorf("prefer: rank %s is not one of %s", brief(starlark.String(rank)), strings.Join(store.Tiers, " "))
	}
	type key struct {
		list *starlark.Value // the tuple's array: the memo's key holds it, so it is not reused while memoised
		n    int
		rank string
	}
	memo, _ := th.Local("prefer").(map[key]*starlark.List)
	if memo == nil {
		memo = map[key]*starlark.List{}
		th.SetLocal("prefer", memo)
	}
	k := key{unsafe.SliceData(cands), len(cands), rank}
	if l, ok := memo[k]; ok {
		return l, nil
	}
	l, err := preferred(cands, rung(rank))
	if err == nil {
		memo[k] = l
	}
	return l, err
}

// preferred is prefer's answer: the candidates by the default's order,
// those with room first.
func preferred(cands starlark.Tuple, want int) (*starlark.List, error) {
	var up, below, room []starlark.Value
	roomy := map[starlark.Value]bool{}
	for _, c := range cands {
		s, ok := c.(*starlarkstruct.Struct)
		if !ok {
			return nil, fmt.Errorf("prefer: a candidate is %s, not a row", kind(c))
		}
		name, r, has, err := attrs(s)
		if err != nil {
			return nil, err
		}
		if rung(r) >= want {
			up = append(up, name)
		} else {
			below = append(below, name)
		}
		roomy[name] = has
	}
	for i, j := 0, len(below)-1; i < j; i, j = i+1, j-1 {
		below[i], below[j] = below[j], below[i]
	}
	all := append(up, below...)
	for _, n := range all {
		if roomy[n] {
			room = append(room, n)
		}
	}
	if len(room) == 0 {
		room = all
	}
	l := starlark.NewList(room)
	l.Freeze()
	return l, nil
}

// attrs are a candidate's name, rank and whether it has room.
func attrs(s *starlarkstruct.Struct) (starlark.Value, string, bool, error) {
	var v [4]starlark.Value
	for i, a := range []string{"name", "rank", "working", "width"} {
		x, err := s.Attr(a)
		if err != nil || x == nil {
			return nil, "", false, fmt.Errorf("prefer: a candidate has no %s", a)
		}
		v[i] = x
	}
	rank, okRank := v[1].(starlark.String)
	working, okWorking := v[2].(starlark.Int)
	width, okWidth := v[3].(starlark.Int)
	if !okRank || !okWorking || !okWidth {
		return nil, "", false, fmt.Errorf("prefer: a candidate's rank, working or width is of the wrong type")
	}
	w, _ := working.Int64() // ignored: the engine's counts always fit
	n, _ := width.Int64()   // ignored: as above
	return v[0], string(rank), w < n, nil
}
