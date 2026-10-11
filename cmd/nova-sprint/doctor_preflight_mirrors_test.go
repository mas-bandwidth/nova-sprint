package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The mirror fact of the member probe (docs/SPEC-SPRINT.md, doctor: the preflight; the
// card doctor-preflight-checks-bench-mirrors): probeScript prints one "mirror <name>
// <path|->" line per repository the sprint's outstanding cards land on, from the same
// candidates pkg/swarm FindBenchMirror tries, and parseProbe reads them into MemberProbe.

// probeScript holds a mirror line per name, quotes each name, and checks the same
// candidates FindBenchMirror tries in the same order.
func TestProbeScriptMirrorsQuotesEachName(t *testing.T) {
	script := probeScript("b1", "swarm-b1", []string{"git"}, "nova-sprint", "nova-tools")
	assert.Contains(t, script, "mirror $n $m", script)
	assert.Contains(t, script, `'nova-sprint'`, script)
	assert.Contains(t, script, `'nova-tools'`, script)
	for _, c := range []string{
		`"$n"`,
		`"$HOME/nova-bench/mirror/$n.git"`,
		`"$HOME/nova-bench/mirror/$n"`,
		`"/tmp/$n-mirror.git"`,
		`"/tmp/$n.git"`,
	} {
		assert.Contains(t, script, c, "the candidates FindBenchMirror tries, in its order: %s", c)
	}
	assert.Less(t, strings.Index(script, `"$HOME/nova-bench/mirror/$n.git"`), strings.Index(script, `"/tmp/$n-mirror.git"`), script)
}

// parseProbe reads a mirror line per name: a path, or "" for the "-" that says none.
func TestParseProbeReadsMirrors(t *testing.T) {
	var p sprint.MemberProbe
	parseProbe(&p, []byte("os Linux\nmirror nova-sprint /home/nova/nova-bench/mirror/nova-sprint.git\nmirror nova-tools -\nend\n"))
	assert.Equal(t, "/home/nova/nova-bench/mirror/nova-sprint.git", p.Mirrors["nova-sprint"])
	assert.Equal(t, "", p.Mirrors["nova-tools"])
	assert.True(t, p.Complete)
}
