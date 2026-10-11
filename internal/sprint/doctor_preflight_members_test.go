package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The preflight's member rows are a pure function over the facts the verb read, the
// package's own MemberProbe among them (docs/SPEC-SPRINT.md, doctor, the
// preflight's member check): the candidate's version and each member's probe, so the
// rule is held to its line with no store and no host.

// memberRows is every member row of the preflight, the later-check WARNs aside.
func memberRows(checks []DoctorCheck) []DoctorCheck {
	var out []DoctorCheck
	for _, c := range checks {
		if c.Check == CheckPreflightMembers {
			out = append(out, c)
		}
	}
	return out
}

// rowOf is the members row for member, found by subject.
func memberRowOf(t *testing.T, checks []DoctorCheck, member string) DoctorCheck {
	t.Helper()
	for _, c := range memberRows(checks) {
		if c.Subject == member {
			return c
		}
	}
	t.Fatalf("no members row for %s: %+v", member, checks)
	return DoctorCheck{}
}

// allTools is a probe's tools with every MemberTool found.
func allTools() map[string]string {
	m := map[string]string{}
	for _, t := range MemberTools {
		m[t] = "/usr/bin/" + t
	}
	return m
}

// goodProbe is a member whose every fact is in place, so a test names one fault.
func goodProbe() MemberProbe {
	return MemberProbe{Member: "b1", Host: "b1", Reached: true, Complete: true,
		Tools: allTools(), SecretNames: []string{}, Java: "/usr/bin/java", TLA: "present",
		PushOK: true, SecretOK: true, FreeKB: 50 << 20, Version: "v1.2.14"}
}

func memberInput(p MemberProbe) PreflightInput {
	return PreflightInput{Binary: "/b/nova-sprint", Version: "v1.2.14", ShadowCode: 0,
		Members: []PreflightMember{{Probe: p}}}
}

// a member whose loop's PATH does not find go is one FAIL naming the tool.
func TestDoctorPreflightMemberMissingGoFails(t *testing.T) {
	t.Parallel()
	p := goodProbe()
	p.Tools["go"] = ""
	c := memberRowOf(t, DoctorPreflight(memberInput(p)), "b1")
	assert.Equal(t, DoctorFail, c.Status)
	assert.Contains(t, c.Evidence, "go")
}

// a route whose provider key name the member's seat does not hold is one FAIL naming
// both the route and the key's name.
func TestDoctorPreflightMemberRouteKeyAbsentFailsNamingBoth(t *testing.T) {
	t.Parallel()
	in := memberInput(goodProbe())
	in.Members[0].Probe.SecretNames = []string{"OPENAI_API_KEY"}
	in.Members[0].Routes = []PreflightRouteKey{{Route: "ablit", Key: "ABLITERATION_API_KEY"}}
	c := memberRowOf(t, DoctorPreflight(in), "b1")
	assert.Equal(t, DoctorFail, c.Status)
	assert.Contains(t, c.Evidence, "ablit")
	assert.Contains(t, c.Evidence, "ABLITERATION_API_KEY")
}

// a member without java and /opt/tla/tla2tools.jar is one WARN, since only some
// benches run TLC.
func TestDoctorPreflightMemberJavaAbsentWarns(t *testing.T) {
	t.Parallel()
	in := memberInput(goodProbe())
	in.Members[0].Probe.Java = ""
	in.Members[0].Probe.TLA = "absent"
	c := memberRowOf(t, DoctorPreflight(in), "b1")
	assert.Equal(t, DoctorWarn, c.Status)
	assert.Contains(t, c.Evidence, "java")
	assert.Contains(t, c.Evidence, "tla2tools")
}

// a held member is one WARN naming the cause and the command that releases it.
func TestDoctorPreflightMemberHeldWarnsWithItsCommand(t *testing.T) {
	t.Parallel()
	in := memberInput(goodProbe())
	in.Members[0].Held = "the coordinator holds it"
	c := memberRowOf(t, DoctorPreflight(in), "b1")
	assert.Equal(t, DoctorWarn, c.Status)
	assert.Contains(t, c.Evidence, "the coordinator holds it")
	assert.Contains(t, c.Remedy, "nova-sprint")
}

// a member two releases back from the candidate is one FAIL; one release back is OK;
// newer than the candidate is a FAIL (a switch backwards past a member).
func TestDoctorPreflightMemberVersionSkew(t *testing.T) {
	t.Parallel()
	two := memberInput(goodProbe())
	two.Members[0].Probe.Version = "v1.2.12"
	assert.Equal(t, DoctorFail, memberRowOf(t, DoctorPreflight(two), "b1").Status)

	one := memberInput(goodProbe())
	one.Members[0].Probe.Version = "v1.2.13"
	assert.Empty(t, failsOf(DoctorPreflight(one)), "one release back is OK")

	newer := memberInput(goodProbe())
	newer.Members[0].Probe.Version = "v1.2.15"
	assert.Equal(t, DoctorFail, memberRowOf(t, DoctorPreflight(newer), "b1").Status)
}

// a member whose host does not answer is one WARN (a switch with a member away is
// allowed), and a probe the budget cut judges nothing.
func TestDoctorPreflightMemberDownWarns(t *testing.T) {
	t.Parallel()
	in := memberInput(goodProbe())
	in.Members[0].Probe.Reached = false
	in.Members[0].Probe.Err = "ssh timed out"
	c := memberRowOf(t, DoctorPreflight(in), "b1")
	assert.Equal(t, DoctorWarn, c.Status)
	assert.Contains(t, c.Evidence, "does not answer")
}
