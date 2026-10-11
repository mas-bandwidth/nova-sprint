package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The probe's link fact (docs/SPEC-SPRINT.md, doctor: member-env; the card
// doctor-names-a-member-whose-link-flapped): the probe reads the kernel's link changes in the
// last hour before its end marker, a name from nova-config quoted into nothing, and parseProbe
// reads the three readings. No test here runs ssh or journalctl: the script's text and canned
// output alone are held.

// probeScript carries the journalctl fact, reads a short window, says Darwin is not measured,
// and quotes no name from nova-config into the fact.
func TestProbeScriptHasTheLinkFactAndQuotesNothingFromConfig(t *testing.T) {
	t.Parallel()
	script := probeScript("b1;id", "swarm-b1", []string{"git;id"})
	assert.Contains(t, script, `journalctl -k -q --no-pager --since "-1h"`, "the kernel journal, one short window, never the whole journal")
	assert.Contains(t, script, "link - not measured on darwin")
	assert.Contains(t, script, "insufficient permissions")
	assert.Less(t, strings.Index(script, "journalctl"), strings.Index(script, "echo "+probeEnd), "the link fact is printed before the end marker")
	_, fact, _ := strings.Cut(script, "journalctl")
	assert.NotContains(t, fact, "b1;id", "the link fact quotes no member name from nova-config")
	assert.NotContains(t, fact, "swarm-b1", "the link fact quotes no seat from nova-config")
}

// parseProbe reads the three readings: a count with the last line, not measured with why, and
// zero changes with no last line.
func TestParseProbeReadsTheLinkFact(t *testing.T) {
	t.Parallel()
	cases := []struct {
		line   string
		change int
		last   string
		why    string
	}{
		{"link 4 igc 0000:01:00.0 enp1s0: NIC Link is Up 2500 Mbps", 4, "igc 0000:01:00.0 enp1s0: NIC Link is Up 2500 Mbps", ""},
		{"link - not measured on darwin", -1, "", "not measured on darwin"},
		{"link 0 ", 0, "", ""},
	}
	for _, tc := range cases {
		var p sprint.MemberProbe
		parseProbe(&p, []byte("os Linux\n"+tc.line+"\nend\n"))
		assert.Equal(t, tc.change, p.LinkChanges, tc.line)
		assert.Equal(t, tc.last, p.LinkLast, tc.line)
		assert.Equal(t, tc.why, p.LinkWhy, tc.line)
	}
}
