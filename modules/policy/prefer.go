package policy

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unsafe"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

type needPair struct {
	key string
	val int64
}

// prefer(candidates, rank[, needs]) is the default's choice of rows, in Go so
// that a tick's rows work is linear in its producers, not producers x rows:
// the names of the candidates at rank and up, nearest first, then those below
// it, highest first; every candidate, those with room first (working < width
// on kin, free >= needs on benches). Candidates come by rank, lowest first,
// then by name. It is memoised for the thread per candidate list, rank and
// resource needs, and the list it returns is frozen.
func prefer(th *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cands starlark.Tuple
	var rank string
	var needsVal starlark.Value
	if err := starlark.UnpackPositionalArgs("prefer", args, kwargs, 2, &cands, &rank, &needsVal); err != nil {
		return nil, err
	}
	if rung(rank) < 0 {
		return nil, fmt.Errorf("prefer: rank %s is not one of %s", brief(starlark.String(rank)), strings.Join(store.Tiers, " "))
	}
	needs, nKey, err := unpackNeeds(needsVal)
	if err != nil {
		return nil, err
	}
	type key struct {
		list  *starlark.Value // the tuple's array: the memo's key holds it, so it is not reused while memoised
		n     int
		rank  string
		needs string
	}
	memo, _ := th.Local("prefer").(map[key]*starlark.List)
	if memo == nil {
		memo = map[key]*starlark.List{}
		th.SetLocal("prefer", memo)
	}
	k := key{unsafe.SliceData(cands), len(cands), rank, nKey}
	if l, ok := memo[k]; ok {
		return l, nil
	}
	l, err := preferred(cands, rung(rank), needs)
	if err == nil {
		memo[k] = l
	}
	return l, err
}

func unpackNeeds(v starlark.Value) ([]needPair, string, error) {
	if v == nil || v == starlark.None {
		return nil, "", nil
	}
	d, ok := v.(*starlark.Dict)
	if !ok {
		return nil, "", fmt.Errorf("prefer: needs is %s, not a dict of resource to int", kind(v))
	}
	if d == emptyResourcesDict || d == defaultResourcesDict || d.Len() == 0 {
		return nil, "", nil
	}
	pairs := make([]needPair, 0, d.Len()+2)
	hasCPU, hasMem := false, false
	for _, kv := range d.Items() {
		ks, okK := kv[0].(starlark.String)
		vi, okV := kv[1].(starlark.Int)
		if !okK || !okV {
			return nil, "", fmt.Errorf("prefer: needs entry (%s: %s) is not string: int", kind(kv[0]), kind(kv[1]))
		}
		n, okI := vi.Int64()
		if !okI || n < 0 {
			return nil, "", fmt.Errorf("prefer: needs %s is negative or out of range", string(ks))
		}
		name := string(ks)
		hasCPU = hasCPU || name == "cpu"
		hasMem = hasMem || name == "memory"
		pairs = append(pairs, needPair{key: name, val: n})
	}
	if !hasCPU {
		pairs = append(pairs, needPair{key: "cpu", val: 1})
	}
	if !hasMem {
		pairs = append(pairs, needPair{key: "memory", val: 1})
	}
	if len(pairs) == 2 && ((pairs[0].key == "cpu" && pairs[0].val == 1 && pairs[1].key == "memory" && pairs[1].val == 1) ||
		(pairs[1].key == "cpu" && pairs[1].val == 1 && pairs[0].key == "memory" && pairs[0].val == 1)) {
		return nil, "", nil
	}
	slices.SortFunc(pairs, func(a, b needPair) int { return cmp.Compare(a.key, b.key) })
	var b strings.Builder
	for _, p := range pairs {
		fmt.Fprintf(&b, "%s=%d,", p.key, p.val)
	}
	return pairs, b.String(), nil
}

// preferred is prefer's answer: the candidates by the default's order,
// those with room first.
func preferred(cands starlark.Tuple, want int, needs []needPair) (*starlark.List, error) {
	var up, below []starlark.Value
	roomy := map[starlark.Value]bool{}
	for _, c := range cands {
		s, ok := c.(*starlarkstruct.Struct)
		if !ok {
			return nil, fmt.Errorf("prefer: a candidate is %s, not a row", kind(c))
		}
		name, r, has, err := attrs(s, needs)
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
	slices.Reverse(below)
	all := append(up, below...)
	slices.SortStableFunc(all, func(a, b starlark.Value) int { return b2i(roomy[b]) - b2i(roomy[a]) })
	l := starlark.NewList(all)
	l.Freeze()
	return l, nil
}

func dictInt64(d *starlark.Dict, key string) (int64, error) {
	v, found, err := d.Get(starlark.String(key))
	if err != nil {
		return 0, err
	}
	if !found || v == nil {
		return 0, nil
	}
	iv, ok := v.(starlark.Int)
	if !ok {
		return 0, fmt.Errorf("prefer: free[%q] is %s, not an int", key, kind(v))
	}
	n, ok := iv.Int64()
	if !ok {
		return 0, fmt.Errorf("prefer: free[%q] does not fit int64", key)
	}
	return n, nil
}

func reqAttr(s *starlarkstruct.Struct, attr string) (starlark.Value, error) {
	x, err := s.Attr(attr)
	if err != nil {
		return nil, fmt.Errorf("prefer: a candidate's %s: %w", attr, err)
	}
	if x == nil {
		return nil, fmt.Errorf("prefer: a candidate has no %s", attr)
	}
	return x, nil
}

// attrs are a candidate's name, rank and whether it has room.
func attrs(s *starlarkstruct.Struct, needs []needPair) (starlark.Value, string, bool, error) {
	name, err := reqAttr(s, "name")
	if err != nil {
		return nil, "", false, err
	}
	rankVal, err := reqAttr(s, "rank")
	if err != nil {
		return nil, "", false, err
	}
	rank, okRank := rankVal.(starlark.String)
	if !okRank {
		return nil, "", false, fmt.Errorf("prefer: a candidate's rank is of the wrong type")
	}
	if slices.Contains(s.AttrNames(), "width") {
		widthVal, err := reqAttr(s, "width")
		if err != nil {
			return nil, "", false, err
		}
		workingVal, err := reqAttr(s, "working")
		if err != nil {
			return nil, "", false, err
		}
		working, okWorking := workingVal.(starlark.Int)
		width, okWidth := widthVal.(starlark.Int)
		if !okWorking || !okWidth {
			return nil, "", false, fmt.Errorf("prefer: a candidate's working or width is of the wrong type")
		}
		w, _ := working.Int64() // ignored: the engine's counts always fit
		n, _ := width.Int64()   // ignored: as above
		return name, string(rank), w < n, nil
	}
	freeVal, err := reqAttr(s, "free")
	if err != nil {
		return nil, "", false, err
	}
	freeDict, ok := freeVal.(*starlark.Dict)
	if !ok {
		return nil, "", false, fmt.Errorf("prefer: a candidate's free is %s, not a dict", kind(freeVal))
	}
	if needs == nil {
		cpu, err := dictInt64(freeDict, "cpu")
		if err != nil {
			return nil, "", false, err
		}
		mem, err := dictInt64(freeDict, "memory")
		if err != nil {
			return nil, "", false, err
		}
		return name, string(rank), cpu >= 1 && mem >= 1, nil
	}
	for _, np := range needs {
		have, err := dictInt64(freeDict, np.key)
		if err != nil {
			return nil, "", false, err
		}
		if have < np.val {
			return name, string(rank), false, nil
		}
	}
	return name, string(rank), true, nil
}
