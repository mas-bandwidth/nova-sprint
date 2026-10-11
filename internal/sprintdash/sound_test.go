package sprintdash

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// coinShim is the minimal DOM the coin test owns (no browser is available to the page tests): the
// shipped app.js runs against it and its AudioContext stub counts every oscillator started, so a
// "coin" is the two partials of one chirp. localStorage is a plain map, so the glyph's choice
// survives a toggle the way the browser keeps it.
const coinShim = `
const fs = require('fs');
const vm = require('vm');
const input = JSON.parse(fs.readFileSync(0, 'utf8'));
let osc = 0; const freqs = [], peaks = [], stops = [];
class Node {
  constructor(tag, id) {
    this.tagName = String(tag).toUpperCase(); this.id = id || ''; this.parentNode = null; this.children = [];
    this._classes = []; this._text = ''; this.attributes = {}; this._title = ''; this._listeners = {};
    this.dataset = {}; this.hidden = false; this.scrollWidth = 50; this.clientWidth = 100; this.offsetWidth = 0; this.offsetParent = {};
    this.style = { setProperty() {}, getPropertyValue() { return ''; }, removeProperty() {} };
  }
  get className() { return this._classes.join(' '); } set className(v) { this._classes = String(v).split(/\s+/).filter(Boolean); }
  get classList() { const s = this; return {
    add(...c) { c.forEach(x => { if (!s._classes.includes(x)) s._classes.push(x); }); },
    remove(...c) { c.forEach(x => { const i = s._classes.indexOf(x); if (i >= 0) s._classes.splice(i, 1); }); },
    toggle(c, on) { if (on === undefined) on = !s._classes.includes(c); if (on) this.add(c); else this.remove(c); return on; },
    contains(c) { return s._classes.includes(c); },
  }; }
  get title() { return this._title; } set title(v) { this._title = v; }
  setAttribute(k, v) { this.attributes[k] = String(v); } getAttribute(k) { return k in this.attributes ? this.attributes[k] : null; }
  addEventListener(t, f) { (this._listeners[t] = this._listeners[t] || []).push(f); } removeEventListener() {}
  closest(s) { let n = this.parentNode; while (n) { if (n.tagName.toLowerCase() === s) return n; n = n.parentNode; } return null; }
  _detach() { if (this.parentNode) { const i = this.parentNode.children.indexOf(this); this.parentNode.children.splice(i, 1); this.parentNode = null; } }
  appendChild(c) { c._detach(); c.parentNode = this; this.children.push(c); return c; }
  insertBefore(c, r) { c._detach(); c.parentNode = this; if (r) this.children.splice(this.children.indexOf(r), 0, c); else this.children.push(c); return c; }
  removeChild(c) { c._detach(); return c; } remove() { this._detach(); }
  get lastChild() { return this.children[this.children.length - 1] || null; } get firstChild() { return this.children[0] || null; }
  get nextSibling() { if (!this.parentNode) return null; const s = this.parentNode.children; return s[s.indexOf(this) + 1] || null; }
  get isConnected() { let n = this; while (n.parentNode) n = n.parentNode; return n === doc.root; }
  get textContent() { return this.children.length ? this.children.map(c => c.textContent).join('') : this._text; }
  set textContent(v) { this._text = String(v); this.children = []; }
  get innerHTML() { return this._html === undefined ? this.textContent : this._html; } set innerHTML(v) { this._html = String(v); }
  cloneNode() { const n = new Node(this.tagName, this.id); n._classes = this._classes.slice(); n._text = this._text; return n; }
  all() { return this.children.flatMap(c => [c, ...c.all()]); }
  querySelector(sel) { const cl = sel.split('.').filter(Boolean); return this.all().find(n => cl.every(c => n._classes.includes(c))) || null; }
  querySelectorAll() { return []; }
}
const doc = { root: new Node('html'), byId: new Map(), activeElement: null, _l: {}, documentElement: {} };
doc.body = doc.root.appendChild(new Node('body'));
doc.getElementById = id => {
  if (!doc.byId.has(id)) {
    const n = new Node('div', id); doc.byId.set(id, n); doc.body.appendChild(n);
    if (['streams', 'fleet', 'friends', 'lanes'].includes(id)) { const h = new Node('div'); h.className = 'row head'; h.appendChild(new Node('div')); n.appendChild(h); }
  }
  return doc.byId.get(id);
};
doc.createElement = t => new Node(t); doc.createElementNS = (ns, t) => new Node(t); doc.createTextNode = t => { const n = new Node('#text'); n._text = t; return n; };
doc.querySelector = () => null; doc.addEventListener = (t, f) => { (doc._l[t] = doc._l[t] || []).push(f); };
const store = {};
function AudioContext() {
  this.currentTime = 0; this.state = 'running'; this.destination = {};
  this.createOscillator = function () {
    osc++;
    const freq = {};
    Object.defineProperty(freq, 'value', { set(v) { freqs.push(v); }, get() { return 0; } });
    return { type: '', frequency: freq, connect() {}, start() {}, stop(t) { stops.push(t); } };
  };
  this.createGain = function () { return { gain: { setValueAtTime(v) { peaks.push(v); }, exponentialRampToValueAtTime() {} }, connect() {} }; };
  this.resume = function () {};
}
let timer = null, nextBody = null;
const context = {
  window: { addEventListener() {}, AudioContext: AudioContext, webkitAudioContext: AudioContext },
  document: doc,
  localStorage: { getItem: k => (k in store ? store[k] : null), setItem: (k, v) => { store[k] = String(v); }, removeItem: k => { delete store[k]; } },
  getComputedStyle: () => ({ fontSize: '16px' }), location: { search: '' },
  fetch: () => Promise.resolve({ ok: true, json: () => Promise.resolve(nextBody) }), setInterval: f => { timer = f; }, clearInterval() {}, setTimeout: () => 0, clearTimeout() {},
  console, Intl, Math, Date, String, Number, parseInt, parseFloat, isNaN, Array, Object, RegExp, Map, Set, JSON, Promise,
};
vm.createContext(context);
vm.runInContext(input.appJS, context);
const steps = [];
for (const s of input.steps) {
  if (s.toggle) context.coinToggle(); else context.coinSnapshot({ landed: s.landed, at: s.at });
  steps.push({ name: s.name, osc: osc });
}
process.stdout.write(JSON.stringify({ steps, osc, freqs, peaks, stops, on: context.coinOn }));
`

type coinStep struct {
	Name   string `json:"name"`
	Landed string `json:"landed,omitempty"`
	At     string `json:"at,omitempty"`
	Toggle bool   `json:"toggle,omitempty"`
	Osc    int    `json:"osc"`
}

type coinRun struct {
	Steps []coinStep `json:"steps"`
	Osc   int        `json:"osc"`
	Freqs []float64  `json:"freqs"`
	Peaks []float64  `json:"peaks"`
	Stops []float64  `json:"stops"`
	On    bool       `json:"on"`
}

// TestTheCoinPlaysOncePerLanding is the coin's rule (docs/SPEC-SPRINT-DASHBOARD.md, "The coin";
// the owner 2026-10-11): one short coin when a refresh's landed rises, once per refresh however
// many landed, never on the first load, never on a fall, and silent until the glyph is clicked
// once. A machine's node runs the shipped app.js against the coinShim; each coin is two sine
// partials (a fifth apart) at peak gain 0.15 that stop within a quarter second.
func TestTheCoinPlaysOncePerLanding(t *testing.T) {
	t.Parallel()
	nodePath, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("NOVA_CI") == "1" {
			require.NoError(t, err, "node is required for the dashboard JS behavioural test")
		}
		t.Skip("node is not installed on this machine; the dashboard JS behavioural test needs it")
	}
	steps := []coinStep{
		{Name: "first-load", Landed: "5", At: "t1"},
		{Name: "same-refresh", Landed: "9", At: "t1"},
		{Name: "rise-while-silent", Landed: "9", At: "t2"},
		{Name: "click-on", Toggle: true},
		{Name: "rise", Landed: "10", At: "t3"},
		{Name: "same-refresh-again", Landed: "10", At: "t3"},
		{Name: "fall", Landed: "4", At: "t4"},
		{Name: "rise-again", Landed: "6", At: "t5"},
	}
	in, err := json.Marshal(map[string]any{"appJS": string(file("app.js")), "steps": steps})
	require.NoError(t, err)
	cmd := exec.Command(nodePath, "-e", coinShim)
	cmd.Stdin = bytes.NewReader(in)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	require.NoError(t, cmd.Run(), "node runner failed: %s", errBuf.String())
	var run coinRun
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &run))

	got := map[string]int{}
	for _, s := range run.Steps {
		got[s.Name] = s.Osc
	}
	// one coin is two oscillators; the sequence pins every rule of the section
	assert.Equal(t, 0, got["first-load"], "the first load plays nothing")
	assert.Equal(t, 0, got["same-refresh"], "a tick already heard plays nothing, however the count grew")
	assert.Equal(t, 0, got["rise-while-silent"], "the glyph is off: a rise plays nothing")
	assert.Equal(t, 2, got["click-on"], "clicked on, the glyph plays the coin once, as the test")
	assert.Equal(t, 4, got["rise"], "a rise plays exactly one coin")
	assert.Equal(t, 4, got["same-refresh-again"], "one coin per refresh, never twice")
	assert.Equal(t, 4, got["fall"], "a fall (a clear) never plays")
	assert.Equal(t, 6, got["rise-again"], "the next rise plays the next coin")
	assert.True(t, run.On, "clicked on, the choice is kept (localStorage)")
	assert.Contains(t, run.Freqs, 880.0, "the first partial")
	assert.Contains(t, run.Freqs, 1320.0, "the second partial, a fifth above")
	assert.Contains(t, run.Peaks, 0.15, "peak gain 0.15")
	for _, s := range run.Stops {
		assert.InDelta(t, 0.25, s, 1e-9, "the coin decays within a quarter second")
	}
}
