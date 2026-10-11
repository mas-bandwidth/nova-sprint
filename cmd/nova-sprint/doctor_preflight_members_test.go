package main

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
)

// parseProbe reads the two facts the member probe gained for the preflight's member
// check (docs/SPEC-SPRINT.md, doctor): java on the loop's PATH with the TLA+ tools'
// jar, and the key names the member's secrets seat holds, by name only. The name
// lines are the transcript `nova-secrets names` prints (pkg/secrets/names.go): one
// SECRETS NAME row per key and the OK line, passed through by probeScript with a
// `name ` prefix, so the fact is pinned to the probe's real output and not a
// made-up one.

func TestParseProbeJavaTLAAndSecretNames(t *testing.T) {
	t.Parallel()
	var p sprint.MemberProbe
	parseProbe(&p, []byte("os Linux\npathfrom loop\ntla /usr/bin/java present\n"+
		"name SECRETS NAME key=DEEPSEEK_API_KEY clear=false\n"+
		"name SECRETS NAME key=OPENAI_API_KEY clear=false\n"+
		"name SECRETS NAMES OK as=seat keys=2 shown=2 sealed=2 clear=0\n"+
		"names measured\n"+
		"version nova-sprint v1.2.14 x\nend\n"))
	assert.Equal(t, "/usr/bin/java", p.Java)
	assert.Equal(t, "present", p.TLA)
	assert.Equal(t, []string{"DEEPSEEK_API_KEY", "OPENAI_API_KEY"}, p.SecretNames)
	assert.True(t, p.Complete)
}

func TestParseProbeMissingJavaAndJarNotMeasured(t *testing.T) {
	t.Parallel()
	var p sprint.MemberProbe
	parseProbe(&p, []byte("tla - absent\nnames -\nend\n"))
	assert.Equal(t, "", p.Java)
	assert.Equal(t, "absent", p.TLA)
	assert.Nil(t, p.SecretNames, "an unmeasured seat's names stay nil")
}

// a seat that opens and holds no key is measured empty, never unmeasured, so a route
// whose key it lacks is a FAIL and not the not-measured WARN.
func TestParseProbeSeatHoldingNoKeyIsMeasuredEmpty(t *testing.T) {
	t.Parallel()
	var p sprint.MemberProbe
	parseProbe(&p, []byte("name SECRETS NAMES OK as=seat keys=0 shown=0 sealed=0 clear=0\nnames measured\nend\n"))
	assert.NotNil(t, p.SecretNames, "a seat that answered is measured, even with no key")
	assert.Empty(t, p.SecretNames)
}
