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

// TestThePageShowsExactlyTheOwnersElements is the parity check (docs/SPEC-SPRINT-DASHBOARD.md,
// "the page is this list"): the Go page shows exactly the elements the owner asked for since
// 2026-10-11, and none of the things he removed by name. It reads the page's own template
// (index.html and app.js, embedded) and renders app.js against a fixture envelope on a stubbed
// DOM (the fake clock is the fixture's at, no socket, no real time), then asserts every element
// of the list is present and that the removed items are absent.
func TestThePageShowsExactlyTheOwnersElements(t *testing.T) {
	t.Parallel()
	index := string(file("index.html"))
	app := string(file("app.js"))

	// --- removed, each by name (the owner) ---
	assert.NotContains(t, index, "machine-chip", "the red machine STOPPED chip is gone; STOP lives in the ETA tile")
	assert.NotContains(t, app, "Updated ", "the clock has no Updated word")
	assert.NotContains(t, index, `<span class="dot"></span>`, "the clock has no green dot")
	assert.NotContains(t, index, "All cards by state", "the progress label is Cards by state, not All cards by state")
	assert.NotContains(t, app, `"nothing in flight"`, "the in-flight tile is blank, not 'nothing in flight'")
	assert.NotContains(t, app, "no stream has spent anything yet", "top streams with no spend draw blank ruled rows, no message")

	// --- the friends panel is hidden by CSS, for the v1.2.x series ---
	assert.Contains(t, index, `<section class="panel friends">`, "the friends panel stays on the page")
	assert.NotContains(t, index, `<section class="panel friends" hidden`, "hidden by CSS, not by attribute")
	assert.Contains(t, index, `.panel.friends { display: none; }`, "the friends panel is hidden by CSS")

	// --- the header (P1-P7) ---
	assert.Contains(t, index, "nova-sprint", "the wordmark")
	assert.Contains(t, index, `<span class="chip" id="version-chip">version <b id="version">-</b></span>`, "the version chip")
	assert.Contains(t, index, `coordinator <b id="coord">-</b>`, "the coordinator chip")
	assert.Contains(t, index, `epoch <b id="epoch">-</b>`, "the epoch chip")
	assert.Contains(t, index, `id="release"`, "the release nav")
	assert.Contains(t, index, `<title>nova-sprint</title>`, "the tab title")
	assert.Contains(t, index, `data-theme="dark"`, "dark theme only")
	assert.Contains(t, index, `https://github.com/mas-bandwidth/nova-sprint`, "the footer link")

	// --- the hero tiles (P8-P14) ---
	assert.Contains(t, index, `<div class="label">Landed</div>`)
	assert.Contains(t, index, `<div class="label">ETA</div>`)
	assert.Contains(t, index, `<div class="label">Cost</div>`)
	assert.Contains(t, index, `<div class="label">In flight</div>`)
	assert.Contains(t, index, `<div class="label">Throughput</div>`)
	assert.Contains(t, index, "cards / hour", "the throughput sub-line")

	// --- panels (P15-P20) ---
	assert.Contains(t, index, `<h2>Cost breakdown</h2>`, "the cost breakdown panel")
	assert.Contains(t, index, `id="tier-sub"`, "the tier legend")
	assert.Contains(t, index, `id="pie"`, "the tier pie")
	assert.Contains(t, index, `id="top-streams"`, "the top streams table")
	assert.Contains(t, index, `<div class="label">Cards by state</div>`, "the Cards by state label")
	assert.Contains(t, index, `id="legend"`, "the state legend")
	assert.Contains(t, index, `id="overall"`, "the segmented bar")
	assert.Contains(t, index, `<h2>Work</h2>`, "the Work panel")
	assert.Contains(t, index, `<h2>Fleet</h2>`, "the Fleet panel")

	// --- the fold chevron on every panel (P16) ---
	assert.Contains(t, index, `.panel-head .fold`, "the fold chevron")

	// --- the coin stays (the owner 2026-10-11) ---
	assert.Contains(t, index, `id="coin"`, "the coin speaker glyph stays")

	// --- app.js: the ETA STOP / DONE words (P9-P11) and the version chip (P3) ---
	assert.Contains(t, app, `"STOP"`, "the ETA tile carries STOP")
	assert.Contains(t, app, `eta-stop`, "STOP is dark red")
	assert.Contains(t, app, `"DONE"`, "the ETA tile carries DONE")
	assert.Contains(t, app, `eta-done`, "DONE is green")
	assert.Contains(t, app, `server_version`, "app.js reads the server_version for the version chip")

	// --- the served page carries the inline data-PNG logo (P1) and the version chip ---
	r := newRig(t)
	r.s.Version = "v1.2.16"
	r.next = func() ([]byte, error) { return ownerFixture(), nil }
	served := get(t, r.s, "/").Body.String()
	assert.Contains(t, served, `data:image/png;base64,`, "the nova-sprint logo is an inline data PNG")

	// --- the page data carries the display names and the envelope carries server_version ---
	var env map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(get(t, r.s, "/api/sprint").Body.Bytes(), &env))
	assert.Equal(t, `"v1.2.16"`, strings.TrimSpace(string(env["server_version"])), "the envelope carries the server's version")
	var data struct {
		Streams []struct{ Stream, Display string }
	}
	require.NoError(t, json.Unmarshal(env["data"], &data))
	byName := map[string]string{}
	for _, s := range data.Streams {
		byName[s.Stream] = s.Display
	}
	assert.Equal(t, "schema c", byName["schema-c"], "the stream's display name rides in streams[]")

	// --- the rendered page, on the stubbed DOM with the fake clock ---
	rendered := renderOwnerFixture(t, app)
	assert.Equal(t, "0", rendered.Landed, "P8 landed count")
	assert.Equal(t, "84", rendered.All, "P8 of 84")
	assert.Equal(t, "0.0%", rendered.Pct, "P8 0.0% complete")
	assert.Equal(t, "STOP", rendered.Eta, "P10 STOP while the machine is STOPPED")
	assert.Equal(t, "big eta-stop", rendered.EtaClass, "P10 STOP is dark red")
	assert.Equal(t, "rowan", rendered.Coord, "P4 coordinator")
	assert.Equal(t, "20", rendered.Epoch, "P5 epoch")
	assert.Equal(t, "v1.2.16", rendered.Version, "P3 version")
	assert.Equal(t, " ", rendered.InflightSub, "P13 the in-flight subline is blank when nothing is in flight")
	assert.Equal(t, "\u2014", rendered.Tput, "P14 throughput is an em dash with no samples")
}

// ownerFixture is a where --json envelope the page renders: epoch 20, 0/84, STOPPED, two
// machines up, eleven held streams, twelve friends held or down, thirteen streams with
// display names. The at is a fixed instant (the fake clock).
func ownerFixture() []byte {
	return []byte(`{"at":"2026-10-13T01:52:20Z","landed":"0","all":"84","summary":"0/84 0.0% held=0 -> ETA -","coordinator":"rowan","epoch":"20","machine":"machine: STOPPED",
"streams":[
{"Stream":"schema-c","Display":"schema c","State":"stopped"},
{"Stream":"s-2","Display":"two","State":"stopped"},
{"Stream":"s-3","Display":"three","State":"stopped"},
{"Stream":"s-4","Display":"four","State":"stopped"},
{"Stream":"s-5","Display":"five","State":"stopped"},
{"Stream":"s-6","Display":"six","State":"stopped"},
{"Stream":"s-7","Display":"seven","State":"stopped"},
{"Stream":"s-8","Display":"eight","State":"stopped"},
{"Stream":"s-9","Display":"nine","State":"stopped"},
{"Stream":"s-10","Display":"ten","State":"stopped"},
{"Stream":"s-11","Display":"eleven","State":"stopped"},
{"Stream":"s-12","Display":"twelve","State":"working"},
{"Stream":"s-13","Display":"thirteen","State":"working"}],
"tables":{
"work":{
"schema-c":{"waiting":"7","ready":"0","working":"0","review":"0","fix":"0","merging":"0","landed":"0","cost":"$0.00"},
"s-2":{"waiting":"7","ready":"0","working":"0","review":"0","fix":"0","merging":"0","landed":"0","cost":"$0.00"},
"s-3":{"waiting":"7","ready":"0","working":"0","review":"0","fix":"0","merging":"0","landed":"0","cost":"$0.00"},
"s-4":{"waiting":"7","ready":"0","working":"0","review":"0","fix":"0","merging":"0","landed":"0","cost":"$0.00"},
"s-5":{"waiting":"7","ready":"0","working":"0","review":"0","fix":"0","merging":"0","landed":"0","cost":"$0.00"},
"s-6":{"waiting":"7","ready":"0","working":"0","review":"0","fix":"0","merging":"0","landed":"0","cost":"$0.00"},
"s-7":{"waiting":"7","ready":"0","working":"0","review":"0","fix":"0","merging":"0","landed":"0","cost":"$0.00"},
"s-8":{"waiting":"7","ready":"0","working":"0","review":"0","fix":"0","merging":"0","landed":"0","cost":"$0.00"},
"s-9":{"waiting":"7","ready":"0","working":"0","review":"0","fix":"0","merging":"0","landed":"0","cost":"$0.00"},
"s-10":{"waiting":"7","ready":"0","working":"0","review":"0","fix":"0","merging":"0","landed":"0","cost":"$0.00"},
"s-11":{"waiting":"7","ready":"0","working":"0","review":"0","fix":"0","merging":"0","landed":"0","cost":"$0.00"},
"s-12":{"waiting":"0","ready":"0","working":"0","review":"0","fix":"0","merging":"0","landed":"0","cost":"$0.00"},
"s-13":{"waiting":"0","ready":"0","working":"0","review":"0","fix":"0","merging":"0","landed":"0","cost":"$0.00"}},
"merge":{},
"fleet":{
"machine-a":{"status":"up","ready":"1","working":"0","width":"16","done":"0","okpct":"100.0","load":"0.0"},
"machine-b":{"status":"up","ready":"2","working":"0","width":"16","done":"0","okpct":"100.0","load":"0.0"}},
"friends":{
"friend-1":{"status":"held","working":"0","width":"8"},
"friend-2":{"status":"held","working":"0","width":"8"},
"friend-3":{"status":"held","working":"0","width":"8"},
"friend-4":{"status":"held","working":"0","width":"8"},
"friend-5":{"status":"held","working":"0","width":"8"},
"friend-6":{"status":"held","working":"0","width":"8"},
"friend-7":{"status":"down","working":"0","width":"8"},
"friend-8":{"status":"down","working":"0","width":"8"},
"friend-9":{"status":"down","working":"0","width":"8"},
"friend-10":{"status":"down","working":"0","width":"8"},
"friend-11":{"status":"down","working":"0","width":"8"},
"friend-12":{"status":"down","working":"0","width":"8"}}}}`)
}

// ownerRender is what the stubbed-DOM render reads back.
type ownerRender struct {
	Landed      string `json:"landed"`
	All         string `json:"all"`
	Pct         string `json:"pct"`
	Eta         string `json:"eta"`
	EtaClass    string `json:"etaClass"`
	Coord       string `json:"coord"`
	Epoch       string `json:"epoch"`
	Version     string `json:"version"`
	InflightSub string `json:"inflightSub"`
	Tput        string `json:"tput"`
}

// renderOwnerFixture runs the shipped app.js against ownerFixture on a stubbed DOM with a
// fake clock and reads the rendered elements back. The stub is self-contained here (its
// style has removeProperty, which render's fleetLike calls).
func renderOwnerFixture(t *testing.T, appJS string) ownerRender {
	t.Helper()
	nodePath, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("NOVA_CI") == "1" {
			require.NoError(t, err, "node is required for the dashboard JS parity test")
		}
		t.Skip("node is not installed on this machine; the dashboard JS parity test needs it")
	}

	const runner = ownerDOMStub + `
const fixture = {"at":"2026-10-13T01:52:20Z","landed":"0","all":"84","summary":"0/84 0.0% held=0 -> ETA -","coordinator":"rowan","epoch":"20","machine":"machine: STOPPED","server_version":"v1.2.16","tables":{"work":{"schema-c":{"waiting":"7","landed":"0","cost":"$0.00"}},"merge":{},"fleet":{"machine-a":{"status":"up","width":"16","done":"0"},"machine-b":{"status":"up","width":"16","done":"0"}},"friends":{"friend-1":{"status":"held","width":"8"}}},"streams":[{"Stream":"schema-c","Display":"schema c","State":"stopped"}]};
try { context.accept({data: fixture, build: "x", throughput: null, throughputMinutes: 0}); } catch (e) { console.error(e); }
process.stdout.write(JSON.stringify({
  landed: getEl("landed")._val,
  all: getEl("all")._val,
  pct: getEl("pct")._val,
  eta: getEl("eta")._val,
  etaClass: getEl("eta").className,
  coord: getEl("coord")._val,
  epoch: getEl("epoch")._val,
  version: getEl("version")._val,
  inflightSub: getEl("inflight-sub").textContent,
  tput: getEl("tput")._val
}));
`

	cmd := exec.Command(nodePath, "-e", runner)
	cmd.Stdin = bytes.NewReader(mustJSON(map[string]string{"appJS": appJS}))
	cmd.Env = append(os.Environ(), "TZ=America/New_York")
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	require.NoError(t, cmd.Run(), "node runner failed: %s", errBuf.String())

	var res ownerRender
	require.NoError(t, json.Unmarshal(out.Bytes(), &res), "unmarshaling node output: %s", out.String())
	return res
}

// ownerDOMStub loads app.js into a minimal DOM, as jsDOMPrelude does, with the style
// stub carrying removeProperty (fleetLike calls it). It reads {"appJS": ...} on stdin and
// leaves the page's functions on context and its elements reachable by getEl.
const ownerDOMStub = `
const fs = require('fs');
const vm = require('vm');
const appCode = JSON.parse(fs.readFileSync(0, 'utf8')).appJS;
function createElement(tag, id = '') {
  const _classes = new Set();
  const el = {
    tagName: tag.toUpperCase(), id: id, _classes,
    get className() { return Array.from(_classes).join(' '); },
    set className(v) { _classes.clear(); if (v) String(v).split(/\s+/).filter(Boolean).forEach(c => _classes.add(c)); },
    get classList() { const s = this; return { add(...c){c.forEach(x=>_classes.add(x));}, remove(...c){c.forEach(x=>_classes.delete(x));}, toggle(c,f){if(f===undefined)f=!_classes.has(c); if(f)_classes.add(c); else _classes.delete(c); return f;}, contains(c){return _classes.has(c);} }; },
    children: [],
    style: { setProperty(k,v){this[k]=v;}, getPropertyValue(k){return this[k]||'';}, removeProperty(k){delete this[k];} },
    attributes: {}, addEventListener(){}, setAttribute(k,v){this.attributes[k]=String(v);}, getAttribute(k){return this.attributes[k]||null;},
    appendChild(c){ if(typeof c==='string') c={textContent:c,children:[]}; this.children.push(c); c.parentNode=this; return c; },
    insertBefore(c,ref){ if(c.parentNode)c.parentNode.removeChild(c); const i=ref?this.children.indexOf(ref):-1; if(i>=0)this.children.splice(i,0,c); else this.children.push(c); c.parentNode=this; return c; },
    get nextSibling(){ if(!this.parentNode)return null; const s=this.parentNode.children; return s[s.indexOf(this)+1]||null; },
    get lastChild(){ return this.children[this.children.length-1]||null; }, dataset: {},
    removeChild(c){ const i=this.children.indexOf(c); if(i>=0)this.children.splice(i,1); c.parentNode=null; return c; },
    get firstChild(){ return this.children[0]||null; },
    get innerHTML(){ return ''; }, set innerHTML(v){ this.children=[]; },
    get textContent(){ if(this.children.length>0) return this.children.map(c=>c.textContent).join(''); return this._text||''; },
    set textContent(v){ this._text=v; this.children=[]; },
    querySelector(){ return createElement('div'); }, querySelectorAll(){ return []; },
    closest(){ return createElement('section'); },
    remove(){ if(this.parentNode)this.parentNode.removeChild(this); }, cloneNode(){ return createElement(tag,id); },
    scrollWidth:50, clientWidth:100, offsetWidth:50, offsetHeight:20, offsetParent:{}
  };
  return el;
}
const elements = new Map();
function getEl(id){ if(!elements.has(id)) elements.set(id, createElement('div', id)); return elements.get(id); }
const context = {
  window: { addEventListener(){} },
  document: { getElementById: getEl, createElement, createElementNS: (ns,tag)=>createElement(tag), createTextNode: (t)=>({textContent:t,children:[]}), documentElement:{fontSize:'16px'} },
  getComputedStyle: () => ({ fontSize:'16px' }), location: { search: '' },
  fetch: () => Promise.resolve({ok:true, json:()=>Promise.resolve({})}), setInterval: ()=>{}, setTimeout: ()=>{},
  console, Intl, Math, Date, String, Number, parseInt, parseFloat, isNaN, Array, Object, RegExp, JSON
};
vm.createContext(context);
vm.runInContext(appCode, context);
`
