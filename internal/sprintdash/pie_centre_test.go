package sprintdash

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The pie's slices meet at the centre (fix entry dashboard-pie-slices-meet-centre in
// docs/fixes.sexp). The border used to ride on each slice's own outline (index.html stroked
// .pie path), so the border ate every tip at the centre: the fills stopped 0.5 a unit short
// of it and the borders faded into a pale star there instead of crossing at the point (the
// owner 2026-10-10: the round join that replaced the miter notch still stops short). The
// fills are drawn bare now and the borders are drawn on top of them, one spoke a border from
// the centre to the rim, with the rim ring over them.
//
// The page runs under node on the DOM stub (jsDOMPrelude, tweaks_test.go): the pie is drawn
// for three tiers of spend and its children are read back as their tags, classes and
// attributes, so the test pins the geometry the browser is given.

// pieCentreCopy carries three tiers of spend: $15, $4 and $1 shown (the whole-dollar
// rounding of $14.10, $3.20 and $1.00), the pie's three slices and three borders.
const pieCentreCopy = `{"tables":{"work":{"s":{"cost":"$18.30","cost_by_tier":{"pro":"$14.10","flash":"$3.20","heavy":"$1.00"}}}}}`

// TestPieSliceBordersMeetAtTheCentre pins the pie the page is given: every slice a bare fill
// from the centre, every border a spoke that starts at the exact centre and ends at the
// shared coordinate of the two slices it separates, and the rim ring over them.
func TestPieSliceBordersMeetAtTheCentre(t *testing.T) {
	t.Parallel()
	nodePath, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("NOVA_CI") == "1" {
			require.NoError(t, err, "node is required for the dashboard JS behavioural test")
		}
		t.Skip("node is not installed on this machine; the dashboard JS behavioural test needs it")
	}
	appJS, err := os.ReadFile("page/app.js")
	require.NoError(t, err)

	script := jsDOMPrelude + `
context.renderPie(input.data);
const pie = getEl("pie");
process.stdout.write(JSON.stringify({
  kids: pie.children.map(c => ({
    tag: c.tagName,
    cls: (c.attributes && c.attributes.class) || "",
    attrs: Object.assign({}, c.attributes),
  })),
}));
`
	in, err := json.Marshal(map[string]any{"appJS": string(appJS), "data": json.RawMessage(pieCentreCopy)})
	require.NoError(t, err)
	cmd := exec.Command(nodePath, "-e", script)
	cmd.Stdin = bytes.NewReader(in)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	require.NoError(t, cmd.Run(), "node runner failed: %s", errBuf.String())
	var res struct {
		Kids []struct {
			Tag   string            `json:"tag"`
			Cls   string            `json:"cls"`
			Attrs map[string]string `json:"attrs"`
		} `json:"kids"`
	}
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &res), outBuf.String())

	// the fills: one path a slice, every one from the exact centre, none stroked
	var dRe = regexp.MustCompile(`^M50,50 L([^,]+),([^ ]+) A48,48 0 [01] 1 ([^,]+),([^ ]+) Z$`)
	type wedge struct{ x0, y0, x1, y1 string }
	var fills []wedge
	var seps []map[string]string
	var rims []map[string]string
	for _, k := range res.Kids {
		switch {
		case k.Tag == "PATH":
			_, stroked := k.Attrs["stroke"]
			require.False(t, stroked, "a slice's fill is drawn bare: the border on the outline ate the tip at the centre")
			m := dRe.FindStringSubmatch(k.Attrs["d"])
			require.NotNil(t, m, "a slice runs from the centre to the rim: %q", k.Attrs["d"])
			fills = append(fills, wedge{m[1], m[2], m[3], m[4]})
		case k.Tag == "LINE":
			require.Equal(t, "sep", k.Cls, "a border is a sep spoke")
			seps = append(seps, k.Attrs)
		case k.Tag == "CIRCLE":
			rims = append(rims, k.Attrs)
		default:
			t.Fatalf("unexpected pie child %s", k.Tag)
		}
	}
	require.Len(t, fills, 3, "a slice a tier with spend")
	require.Len(t, seps, 3, "a border spoke a slice: the boundary after it")
	require.Len(t, rims, 1, "the rim ring over the borders")

	// adjacent slices share their border coordinate exactly, and each border spoke runs
	// from the exact centre to that shared coordinate: this is the borders meeting at the
	// centre
	for i := range fills {
		j := (i + 1) % len(fills)
		assert.Equal(t, fills[i].x1, fills[j].x0, "slice %d ends where %d starts", i, j)
		assert.Equal(t, fills[i].y1, fills[j].y0, "slice %d ends where %d starts", i, j)
	}
	for _, sep := range seps {
		assert.Equal(t, "50", sep["x1"], "a border starts at the centre")
		assert.Equal(t, "50", sep["y1"], "a border starts at the centre")
		var at bool
		for _, f := range fills {
			if sep["x2"] == f.x1 && sep["y2"] == f.y1 {
				at = true
			}
		}
		assert.True(t, at, "a border ends at a shared slice coordinate, got %s,%s", sep["x2"], sep["y2"])
	}
	rim := rims[0]
	assert.Equal(t, "50", rim["cx"])
	assert.Equal(t, "50", rim["cy"])
	assert.Equal(t, "48", rim["r"])
	assert.Equal(t, "none", rim["fill"], "the rim is a ring, never a disc over the pie")

	// the page's style: the borders and the rim carry the surface colour, and no rule
	// strokes the slices themselves any more
	page, err := os.ReadFile("page/index.html")
	require.NoError(t, err)
	assert.Contains(t, string(page), ".pie .sep, .pie .rim { fill: none; stroke: var(--surface); stroke-width: 1; }",
		"the borders and the rim are the surface-coloured lines")
	assert.NotContains(t, string(page), ".pie path { stroke",
		"the border on a slice's own outline ate the tip at the centre")
}
