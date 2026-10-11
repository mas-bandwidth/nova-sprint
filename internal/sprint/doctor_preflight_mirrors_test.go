package sprint

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The mirror row of the preflight (docs/SPEC-SPRINT.md, doctor: the preflight; the card
// doctor-preflight-checks-bench-mirrors): the repositories the sprint's not-yet-landed
// cards land on, each member that holds no bench mirror for one FAIL under
// doctor --preflight and WARN in member-env without.

// PendingRepos is the distinct repositories of the outstanding cards, by the name a
// member names its mirror: the landed card's repository is left out.
func TestPendingReposNamesTheOutstandingCardsRepositories(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 3}))
	for i, repo := range []string{"mas-bandwidth/nova-sprint", "mas-bandwidth/nova-tools", "mas-bandwidth/nova-roadmap"} {
		w.s.Work.Card("s1-" + strconv.Itoa(i+1)).Fields[FieldRepo] = repo
	}
	w.s.Work.Card("s1-1").Col = Ready
	w.s.Work.Card("s1-2").Col = Working
	w.s.Work.Card("s1-3").Col = Landed
	assert.Equal(t, []string{"nova-sprint", "nova-tools"}, PendingRepos(w.s))
}

// PendingRepos also names a stream's recorded repository (steps_work.go: every admitted
// card records its repository on its stream's control card, FieldRepo), so a repository
// whose outstanding cards name none still has a mirror checked.
func TestPendingReposNamesTheStreamsRecordedRepository(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 1, Repo: "mas-bandwidth/nova-roadmap"}))
	w.s.Work.Card("s1-1").Col = Landed
	assert.Equal(t, []string{"nova-roadmap"}, PendingRepos(w.s))
}

// one member holding one of two mirrors is one FAIL under --preflight, and a member
// holding none is one FAIL for each repository.
func TestPreflightMirrorRowsFailAMissingMirror(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	in := PreflightInput{Version: "v1.2.14", Repos: []string{"nova-sprint", "nova-tools"}}
	p := fullProbe("m1")
	p.Mirrors = map[string]string{"nova-sprint": "/home/nova/nova-bench/mirror/nova-sprint.git", "nova-tools": ""}
	in.Probes = map[string]MemberProbe{"m1": p}
	in.Members = PreflightMembers(w.s, in.Probes)
	cs := preflightMirrorRows(in)
	fails := failsOn(cs, CheckPreflightMirrors)
	require.Len(t, fails, 1, lines(cs))
	assert.Equal(t, "m1/nova-tools", fails[0].Subject, lines(cs))
	assert.Equal(t, mirrorRemedy("m1"), fails[0].Remedy, lines(cs))

	// a member the probe did not reach is left to the member check: never a mirror FAIL
	p2 := fullProbe("m1")
	p2.Reached = false
	in.Probes = map[string]MemberProbe{"m1": p2}
	in.Members = PreflightMembers(w.s, in.Probes)
	cs = preflightMirrorRows(in)
	assert.Empty(t, failsOn(cs, CheckPreflightMirrors), lines(cs))
}

// doctor --preflight names the mirrors row as measured: a member with no mirror for a
// repository the outstanding cards land on is one FAIL there.
func TestDoctorPreflightNamesTheMirrorRow(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	in := PreflightInput{Version: "v1.2.14", ShadowCode: 0, Repos: []string{"nova-sprint"}}
	p := fullProbe("m1")
	p.Mirrors = map[string]string{"nova-sprint": ""}
	in.Probes = map[string]MemberProbe{"m1": p}
	in.Members = PreflightMembers(w.s, in.Probes)
	cs := DoctorPreflight(in)
	assert.NotEmpty(t, failsOn(cs, CheckPreflightMirrors), lines(cs))
}

// without --preflight the same fact is one WARN per member in member-env, never a FAIL.
func TestDoctorMemberEnvWarnsOnAMissingMirror(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	in := healthyInput(w)
	p := fullProbe("m1")
	p.Mirrors = map[string]string{"nova-sprint": ""}
	in.Probes = map[string]MemberProbe{"m1": p}
	cs := DoctorMemberEnv(in)
	warns := 0
	for _, c := range cs {
		if c.Check == CheckMemberEnv && c.Status == DoctorWarn && strings.Contains(c.Evidence, "no bench mirror for nova-sprint") {
			warns++
		}
	}
	assert.Equal(t, 1, warns, lines(cs))
	assert.Empty(t, failsOn(cs, CheckMemberEnv), "a missing mirror is never a FAIL in member-env: %s", lines(cs))
}

// mirrorRemedy names the bench play that makes the mirror, so the FAIL and the WARN send a
// reader to it rather than to a target this tree does not carry (docs/SPEC-SPRINT.md,
// doctor: the preflight; the card doctor-preflight-checks-bench-mirrors, THE TASK item 3:
// name the play found in nova-tools fleet/, do not guess).
func TestMirrorRemedyNamesTheFleetPlay(t *testing.T) {
	t.Parallel()
	got := mirrorRemedy("b1")
	assert.Contains(t, got, "fleet/loops.yml", got)
	assert.Contains(t, got, "b1", got)
	assert.NotContains(t, got, "mirror HOST=", "no guessed make target: %s", got)
}

// a reached, complete probe that did not measure mirrors (nil Mirrors) judges none: the
// member check's own WARN says so, never every mirror missing.
func TestPreflightMirrorRowsSkipAnUnmeasuredProbe(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	in := PreflightInput{Version: "v1.2.14", Repos: []string{"nova-sprint"}}
	p := fullProbe("m1")
	p.Mirrors = nil
	in.Probes = map[string]MemberProbe{"m1": p}
	in.Members = PreflightMembers(w.s, in.Probes)
	cs := preflightMirrorRows(in)
	assert.Empty(t, failsOn(cs, CheckPreflightMirrors), lines(cs))
}
