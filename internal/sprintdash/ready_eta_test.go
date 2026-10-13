package sprintdash

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// etaShimTail renders the shipped app.js against each snapshot in turn, one poll each (scrollShim's DOM,
// cut before its own driver) and reports the ETA cell's text and colour after each.
const etaShimTail = `
(async () => {
  const out = [];
  for (const snap of input.snaps) {
    nextBody = { build: 'b1', data: snap }; context.poll(); await new Promise(r => setImmediate(r)); await new Promise(r => setImmediate(r));
    const eta = doc.getElementById('eta');
    out.push({ text: eta.textContent || eta.innerHTML || '', color: eta.style.color || '', cls: eta.className || '' });
  }
  process.stdout.write(JSON.stringify(out));
})();
`

type etaCell struct {
	Text  string `json:"text"`
	Color string `json:"color"`
	Class string `json:"cls"`
}

// The ETA cell says READY before the first start since a clear (the owner, 2026-10-13:
// "When you are in it print 'READY' to ETA", "or if you want, green."): the machine line
// READY draws READY in the page's green (var(--good), never an alarm colour); a running
// machine's ETA after it draws the ETA again in the cell's own colour; and STOPPED is
// never READY.
func TestTheETACellSaysReadyBeforeTheFirstStart(t *testing.T) {
	t.Parallel()
	nodePath, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("NOVA_CI") == "1" {
			require.NoError(t, err, "node is required for the dashboard JS behavioural test")
		}
		t.Skip("node is not installed on this machine; the dashboard JS behavioural test needs it")
	}
	snap := func(at, machine, summary string) map[string]any {
		var s map[string]any
		require.NoError(t, json.Unmarshal(fixture(t), &s))
		s["at"], s["machine"], s["summary"] = at, machine, summary // each newer: the page shows only a newer snapshot
		return s
	}
	snaps := []map[string]any{
		snap("2026-10-03T11:20:01-04:00", "machine: READY", "0/5 0.0% -> ETA -"),
		snap("2026-10-03T11:20:02-04:00", "machine: running", "1/5 20.0% -> ETA 2h20m"),
		snap("2026-10-03T11:20:03-04:00", "machine: STOPPED", "1/5 20.0% -> ETA -"),
	}
	in, err := json.Marshal(map[string]any{"appJS": string(file("app.js")), "snaps": snaps})
	require.NoError(t, err)
	cmd := exec.Command(nodePath, "-e", strings.SplitN(scrollShim, "let draws = 0;", 2)[0]+etaShimTail)
	cmd.Stdin = bytes.NewReader(in)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	require.NoError(t, cmd.Run(), "node runner failed: %s", errBuf.String())
	var cells []etaCell
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &cells), outBuf.String())
	require.Len(t, cells, 3)

	assert.Equal(t, "READY", cells[0].Text, "READY in the ETA cell")
	assert.Equal(t, "var(--good)", cells[0].Color, "in the page's existing green")
	assert.NotContains(t, cells[1].Text, "READY", "running: the ETA again")
	assert.Contains(t, cells[1].Text, "2h", "running: the ETA again")
	assert.Empty(t, cells[1].Color, "running: the cell's own colour")
	assert.Equal(t, "STOP", cells[2].Text, "STOPPED is STOP, never READY")
	assert.Contains(t, cells[2].Class, "eta-stop", "STOP keeps its own colour class")
	assert.Empty(t, cells[2].Color)

	css := string(file("index.html"))
	assert.Contains(t, css, "--good:", "the green is the page's own token")
}
