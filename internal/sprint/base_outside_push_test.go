package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Replays the October 15 gofmt incident: the known landed head is followed by
// an outside commit. The fake gate judges that exact commit before a card moves.
func TestABaseMovedOutsideTheLanderIsGated(t *testing.T) {
	for _, red := range []bool{false, true} {
		t.Run(map[bool]string{false: "green proceeds", true: "red opens immediately"}[red], func(t *testing.T) {
			w := setup(t, 2)
			accepted(w, "s1-1", "s1-2")
			const repo, base = "mas-bandwidth/nova-sprint", "v1.3/comms"
			landed, outside := strings.Repeat("a", 40), strings.Repeat("b", 40)
			w.must(RecordBaseLanding(w.s, MergeReq{Stream: "s1", Cards: []string{"s1-1"}}, repo, base, landed))
			require.False(t, BaseMovedOutside(w.s, repo, base, landed))
			require.True(t, BaseMovedOutside(w.s, "other/repo", base, outside), "another repository has unknown provenance")
			require.True(t, BaseMovedOutside(w.s, repo, "other", outside), "another branch has unknown provenance")
			require.True(t, BaseMovedOutside(w.s, repo, base, outside), "an outside descendant is not a lander push")
			gateCalls, attempts := 0, 0
			gate := func(tip string) string {
				gateCalls++
				require.Equal(t, outside, tip)
				if red {
					return "class gofmt: internal/sprint/example.go is not formatted"
				}
				return ""
			}
			if BaseMovedOutside(w.s, repo, base, outside) {
				if why := gate(outside); why != "" {
					w.must(OutsideBaseRed(w.s, BaseGreenReq{Stream: "s1", Base: base, Sha: outside, Who: "lander"}, why))
				} else {
					attempts++
				}
			}
			require.Equal(t, 1, gateCalls)
			if red {
				require.Zero(t, attempts)
				require.Equal(t, Merging, w.state("s1-2"))
				require.Empty(t, w.s.StreamCtl("s1").F(FieldBaseGateRefused), "no three-refusal clock")
				require.Equal(t, StreamStopped, w.s.StreamCtl("s1").F("state"))
				var notes []Note
				for _, o := range w.s.Open {
					if o.Note.Type == NBaseRed {
						notes = append(notes, o.Note)
					}
				}
				require.Len(t, notes, 1)
				require.Contains(t, notes[0].What, outside)
				require.Contains(t, notes[0].What, "gofmt")
			} else {
				require.Equal(t, 1, attempts)
			}
			require.True(t, BaseMovedOutside(w.s, repo, base, outside), "a gate is not a landing")
		})
	}
}

func TestAFailedLandingReportDoesNotAdvanceTheBase(t *testing.T) {
	w := setup(t, 1)
	const repo, base = "mas-bandwidth/nova-sprint", "v1.3/comms"
	p := RecordBaseLanding(w.s, MergeReq{Stream: "missing", Batch: 1}, repo, base, "outside")
	require.NotEmpty(t, p.Refused)
	require.Empty(t, p.Props)
	require.True(t, BaseMovedOutside(w.s, repo, base, "outside"))
}
