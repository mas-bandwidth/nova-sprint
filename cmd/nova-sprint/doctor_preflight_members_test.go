package main

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
)

// parseProbe reads the two facts the member probe gained for the preflight's member
// check (docs/SPEC-SPRINT.md, doctor): java on the loop's PATH with the TLA+ tools'
// jar, and the key names the member's secrets seat holds, by name only.

func TestParseProbeJavaTLAAndSecretNames(t *testing.T) {
	t.Parallel()
	var p sprint.MemberProbe
	parseProbe(&p, []byte("os Linux\npathfrom loop\ntla /usr/bin/java present\nnames OPENAI_API_KEY,DEEPSEEK_API_KEY\nversion nova-sprint v1.2.14 x\nend\n"))
	assert.Equal(t, "/usr/bin/java", p.Java)
	assert.Equal(t, "present", p.TLA)
	assert.Equal(t, []string{"OPENAI_API_KEY", "DEEPSEEK_API_KEY"}, p.SecretNames)
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
