package policy

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// diffLines is the most lines a body may have for diff: above
// it the diff is refused, naming the body. The diff itself is in linear
// space, so the bound is on its time, about two comparisons per pair of
// lines (a guess at what a verb may take: 10,000 x 10,000 in about a second).
const diffLines = 10_000

// Diff is the text diff of two bodies: a and b are 7, v7 or default; b ""
// is the active version.
func Diff(ctx context.Context, st store.Store, a, b string) (string, error) {
	if b == "" {
		snap, err := st.Snapshot(ctx)
		if err != nil || snap.Policy == 0 {
			return "", upFirst("diff", err)
		}
		b = strconv.Itoa(snap.Policy)
	}
	var bodies [2][]string
	for i, s := range []string{a, b} {
		n, ok := versionArg(s)
		body := defaultBody
		switch {
		case !ok:
			return "", &Refusal{"diff", "unknown version " + s, "nova-sprint policy show --list"}
		case n > 0:
			pv, err := version(ctx, st, "diff", n)
			if err != nil {
				return "", err
			}
			body = pv.Body
		}
		if bodies[i] = strings.Split(body, "\n"); len(bodies[i]) > diffLines {
			return "", &Refusal{"diff", fmt.Sprintf("%s has %d lines, over the %d diff compares", s, len(bodies[i]), diffLines),
				"nova-sprint policy show " + s + " > a.star, then compare with a diff tool"}
		}
	}
	var out strings.Builder
	out.WriteString("--- " + a + "\n+++ " + b + "\n")
	lineDiff(&out, bodies[0], bodies[1])
	return out.String(), nil
}

// lineDiff writes the line diff of x and y, " " kept, "-" only in x, "+"
// only in y: the common prefix and suffix as they are, the lines between
// by a longest common subsequence in linear space (hirschberg).
func lineDiff(out *strings.Builder, x, y []string) {
	pre, suf := 0, 0
	for pre < len(x) && pre < len(y) && x[pre] == y[pre] {
		pre++
	}
	for suf < len(x)-pre && suf < len(y)-pre && x[len(x)-1-suf] == y[len(y)-1-suf] {
		suf++
	}
	write(out, "  ", x[:pre])
	hirschberg(out, x[pre:len(x)-suf], y[pre:len(y)-suf])
	write(out, "  ", x[len(x)-suf:])
}

func write(out *strings.Builder, mark string, lines []string) {
	for _, l := range lines {
		out.WriteString(mark + l + "\n")
	}
}

// hirschberg writes the diff of x and y: x split at its middle, y where the
// lengths of the common subsequences of the two halves, one forward and one
// backward, sum highest; each half then in turn. Space is linear in y.
func hirschberg(out *strings.Builder, x, y []string) {
	switch {
	case len(x) == 0:
		write(out, "+ ", y)
	case len(y) == 0:
		write(out, "- ", x)
	case len(x) == 1:
		j := slices.Index(y, x[0])
		if j < 0 {
			write(out, "- ", x)
			write(out, "+ ", y)
			return
		}
		write(out, "+ ", y[:j])
		write(out, "  ", x)
		write(out, "+ ", y[j+1:])
	default:
		mid := len(x) / 2
		fwd, bwd := lcsLengths(x[:mid], y), lcsLengths(reversed(x[mid:]), reversed(y))
		k, best := 0, int32(-1)
		for j := range len(y) + 1 {
			if s := fwd[j] + bwd[len(y)-j]; s > best {
				k, best = j, s
			}
		}
		hirschberg(out, x[:mid], y[:k])
		hirschberg(out, x[mid:], y[k:])
	}
}

func reversed(s []string) []string {
	s = slices.Clone(s)
	slices.Reverse(s)
	return s
}

// lcsLengths is, for each j, the length of the longest common subsequence
// of x and the first j lines of y. Two rows of len(y)+1.
func lcsLengths(x, y []string) []int32 {
	prev, cur := make([]int32, len(y)+1), make([]int32, len(y)+1)
	for _, xi := range x {
		for j := range y {
			if xi == y[j] {
				cur[j+1] = prev[j] + 1
			} else {
				cur[j+1] = max(prev[j+1], cur[j])
			}
		}
		prev, cur = cur, prev
	}
	return prev
}
