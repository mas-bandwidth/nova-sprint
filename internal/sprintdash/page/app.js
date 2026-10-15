// Sprint dashboard: polls /api/sprint (the server's cached `where --json`)
// once a second whatever the round trip, and patches the DOM in place.
// Nothing is blanked on a failed poll; the page holds the last data and
// says nothing.
"use strict";

// Left to right in the bars: done first, so the bar fills like progress. fix is the cards awaiting
// rework, purple, between review and merging (docs/SPEC-SPRINT-DASHBOARD.md, "Fix"; the owner,
// 2026-10-07: "between review and merging"); the view (dashboard.go) counts it on the copy.
var STATES = ["landed", "merging", "fix", "review", "working", "ready", "waiting"];
// Columns of the streams table, in flow order.
var FLOW = ["waiting", "ready", "working", "review", "fix", "merging", "landed"];
// A Work row's children: stream, status, then FLOW's counts, then cost.
var LANDED_AT = 2 + FLOW.indexOf("landed"), COST_AT = 2 + FLOW.length;
var POLL_MS = 1000;
var MIN_CELL = 4;  // px: a cell never gets narrower; cards per cell grows instead
var GAP = 2;       // px between cells
var TRACK_CELL = 1.6875, TRACK_GAP = 0.25; // rem: a fleet track cell and its gap (27 px and 4 px on a desktop), never scaled
// the owner 2026-10-04 ~4:08 PM: the widest track always spans what 16 cells used to, "so no matter the size, it works out"
var TRACK_SPAN = 16 * (TRACK_CELL + TRACK_GAP) - TRACK_GAP;
function trackCell(scale) { scale = Math.max(1, scale); return (TRACK_SPAN - (scale - 1) * TRACK_GAP) / scale; }
var $ = function (id) { return document.getElementById(id); };
["streams", "fleet", "friends"].forEach(function (id) { var h = $(id).querySelector(".row.head"); if (h) h.remove(); });

// ---------- parsing (every value in the JSON is a string) ----------
function int(s) { var n = parseInt(s, 10); return isNaN(n) ? 0 : n; }
function pct(s) { var n = parseFloat(String(s || "").replace("%", "")); return isNaN(n) ? null : n; }
function cents(s) { // "$26.16" -> 2616, "-" -> null; rounded up to the cent
  var n = parseFloat(String(s || "").replace(/[$,]/g, ""));
  if (isNaN(n)) return null;
  var c = Math.ceil(n * 100 - 1e-6);
  return c === 0 ? 0 : c;  // never -0: "$0.00" must not print as "$-0.00"
}
function money(c) { return "$" + (c / 100).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 }); }
// whole dollars rounded up, for the Cost breakdown panel only (the owner 2026-10-04 2:45 PM: "Round up to nearest $", "for this case"); money() keeps the cent everywhere else
function dollars(c) { var d = Math.ceil(c / 100 - 1e-9); return "$" + (d === 0 ? 0 : d).toLocaleString("en-US"); }
// the sprint's duration once done: from the epoch's clear to the end of the last 10-minute bucket a card landed in (landedSeries)
function tookText(d) {
  var t0 = Date.parse(d.cleared || ""), s = d.landedSeries, last = -1;
  if (isNaN(t0) || !s || !s.buckets) return "\u00a0";
  for (var i = 0; i < s.buckets; i++) {
    var n = 0;
    ["fleet", "friends"].forEach(function (k) { if (s[k] && s[k][i]) n += s[k][i]; });
    if (n > 0) last = i;
  }
  var t1 = last >= 0 ? (s.start + (last + 1) * s.bucketSeconds) * 1000 : Date.parse(d.at);
  var m = Math.max(0, Math.round((t1 - t0) / 60000)), dd = Math.floor(m / 1440), hh = Math.floor((m % 1440) / 60), mm = m % 60;
  var parts = [];
  if (dd) parts.push(dd + (dd === 1 ? " day" : " days"));
  if (hh) parts.push(hh + (hh === 1 ? " hour" : " hours"));
  if (mm || !parts.length) parts.push(mm + (mm === 1 ? " minute" : " minutes"));
  return parts.join(" ");
}
function zoneAbbr(d) {
  try { var p = new Intl.DateTimeFormat("en-US", { timeZoneName: "short" }).formatToParts(d).filter(function (x) { return x.type === "timeZoneName"; })[0]; return p ? p.value : ""; }
  catch (e) { return ""; }
}
function timeParts(d) {
  var parts = new Intl.DateTimeFormat("en-US", { hour: "numeric", minute: "2-digit", second: "2-digit", hour12: true }).formatToParts(d);
  var get = function (t) { var x = parts.filter(function (q) { return q.type === t; })[0]; return x ? x.value : ""; };
  return { hms: get("hour") + ":" + get("minute") + ":" + get("second"), ampm: get("dayPeriod") };
}
// "around 11:36 PM EDT"; a day or more out it names the weekday, "around Sun 3:15 AM EDT" (SPEC.md)
function etaAround(when, ms) {
  var day = ms >= 86400000 ? when.toLocaleDateString("en-US", { weekday: "short" }) + " " : "";
  return ("around " + day + clockShort(when) + " " + zoneAbbr(when)).trim();
}
// a tile's subline stays on one line (the owner 2026-10-04 3:20 PM): when it does not fit the tile,
// the shorter forms in turn
function fits(e) { return e.scrollWidth <= e.clientWidth + 1; }
var etaAtLast = null;
function fitEtaAt() {
  var box = $("eta-at"); if (!box || !etaAtLast) return;
  var full = etaAround(etaAtLast[0], etaAtLast[1]), forms = [full, full.replace(/^around /, ""), full.replace(/^around /, "").replace(/ [A-Z]{2,5}$/, "")];
  for (var i = 0; i < forms.length; i++) { setText(box, forms[i]); if (fits(box)) return; }
}
function clock(d) { return d.toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit", second: "2-digit" }); }
function clockShort(d) { return d.toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit" }); }
function etaMs(s) { // "2h20m" or "2d17h" -> ms (the day form arrives with PR 5183; SPEC.md)
  var m = String(s).match(/^(?:(\d+)d)?(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/);
  if (!m || !(m[1] || m[2] || m[3] || m[4])) return null;
  return (((int(m[1]) * 24 + int(m[2])) * 60 + int(m[3])) * 60 + int(m[4])) * 1000;
}
// One ordinary space between the parts of a figure (SPEC.md, the owner 10:00 and 10:03 PM): the
// space is set in the proportional face, because a monospace space is a full digit wide and
// reads as a double space beside the digits.
var SP = "<span class=\"sp\"> </span>";
function etaText(s) { return String(s).replace(/(\d+[dhms])(?=\d)/g, "$1" + SP); }

// ---------- DOM helpers ----------
function el(tag, cls, text) {
  var e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
}
// Emphasis: a value flashes only when it differs from the value rendered the
// refresh before. Each node keeps its last rendered value (e._val); the first
// render stores without flashing. The highlight lives on an inline span
// (.fv) inside the node, so the tint hugs the digits, not the cell. A track
// cell flashes only when it goes lit <-> unlit. The clock never flashes
// (setLiveHTML does not use these helpers).
var flashCount = 0;
["all", "all2", "pct", "eta", "eta-at", "cost", "cost-per", "inflight", "inflight-sub", "tput",
 "streams-sub", "fleet-head", "friends-sub", "readers-sub"].forEach(function (id) { var e = document.getElementById(id); if (e) quiet(e); });
function valEl(e) {
  if (!e._fv) {
    var v = document.createElement("span"); v.className = "fv";
    while (e.firstChild) v.appendChild(e.firstChild);
    e.appendChild(v); e._fv = v;
  }
  return e._fv;
}
function flash(t) {
  flashCount++;
  t.classList.remove("flash"); void t.offsetWidth; t.classList.add("flash");
  if (!t._flashEnd) { t._flashEnd = function () { t.classList.remove("flash"); }; t.addEventListener("animationend", t._flashEnd); }
}
// What flashes: stream counts, the hero landed count, friends' counts, and
// track cells (the fleet's numbers never flash, only its cells). Everything else (ok%, load, the
// fleet's done, cost, the ETA, other hero tiles, legend, pills, subtitles) is
// marked quiet and updates without emphasis.
function quiet(e) { e._quiet = true; return e; }
function setText(e, s) {
  s = String(s);
  if (e._val === s) return;
  var first = e._val == null; e._val = s;
  if (e._quiet) { e.textContent = s; return; }
  valEl(e).textContent = s;
  if (!first) flash(e._fv);
}
function setHTML(e, s) {
  if (e._val === s) return;
  var first = e._val == null; e._val = s;
  if (e._quiet) { e.innerHTML = s; return; }
  valEl(e).innerHTML = s;
  if (!first) flash(e._fv);
}
function setClass(e, c) {
  if (e._cls === c) return;
  var first = e._cls == null; e._cls = c;
  var on = e.classList.contains("flash");
  e.className = c;
  if (on) e.classList.add("flash");
  if (!first && e.classList.contains("cell") && !(e.parentNode && e.parentNode.id === "overall")) flash(e);
}
function setTitle(e, t) { if (e.title !== t) e.title = t; }
function numCell(cls) { return el("div", cls === "frac" ? "frac" : "num " + (cls || "")); }
function setNum(e, v, extra) { setText(e, v); setClass(e, "num " + (extra || "") + (String(v) === "0" ? " zero" : "")); }

// Keyed rows between a fixed header and an optional total row; nodes are
// reused and only moved when out of order, so nothing flickers.
function syncRows(box, header, keys, build, update, total) {
  if (!box._map) { box._map = new Map(); box.textContent = ""; box.appendChild(header); }
  var map = box._map, seen = new Set(), prev = header;
  keys.forEach(function (k) {
    var r = map.get(k);
    if (!r) { r = build(k); map.set(k, r); }
    update(r, k);
    seen.add(k);
    if (prev.nextSibling !== r.node) box.insertBefore(r.node, prev.nextSibling);
    prev = r.node;
  });
  map.forEach(function (r, k) { if (!seen.has(k)) { r.node.remove(); map.delete(k); } });
  if (total && prev.nextSibling !== total) box.insertBefore(total, prev.nextSibling);
}
function headRow(labels) {
  var h = el("div", "row head");
  labels.forEach(function (l) { h.appendChild(el("div", l[1] || "", l[0])); });
  return h;
}

// ---------- segmented bars ----------
// setCells(box, classes, columns): one grid cell per entry of classes ("" is
// an empty slot); cells are reused, so a refresh only repaints what changed.
function setCells(box, classes, columns, cellRem) {
  // cellRem: fixed cell width (the fleet track, so the figure can sit right after it); else cells share the box
  var cols = "repeat(" + Math.max(1, columns) + ", " + (cellRem ? "minmax(0, " + cellRem + "rem)" : "minmax(0, 1fr)") + ")";
  if (box.style.gridTemplateColumns !== cols) box.style.gridTemplateColumns = cols;
  while (box.children.length < classes.length) box.appendChild(el("div", "cell"));
  while (box.children.length > classes.length) box.lastChild.remove();
  classes.forEach(function (c, i) { setClass(box.children[i], c ? "cell " + c : "cell"); });
}
// How many cells fit across a box at MIN_CELL px each.
function cellsThatFit(box) {
  var w = box.clientWidth || 600;
  return Math.max(1, Math.floor((w + GAP) / (MIN_CELL + GAP)));
}
// Cards per cell from 1, 2, 3, 4, 5, 10, 20, 25, 50, 100: the smallest that fits.
function cardsPerCell(total, fit) {
  var ladder = [1, 2, 3, 4, 5, 10, 20, 25, 50, 100];
  for (var i = 0; i < ladder.length; i++) if (Math.ceil(total / ladder[i]) <= fit) return ladder[i];
  return Math.ceil(total / fit);
}
// Share n cells among the states by largest remainder; a state with any
// cards keeps at least one cell, so nothing in flight disappears.
function allocate(counts, total, n) {
  var out = {}, used = 0, rem = [];
  STATES.forEach(function (st) {
    var exact = total ? counts[st] * n / total : 0, f = Math.floor(exact);
    if (counts[st] > 0 && f === 0) f = 1;
    out[st] = f; used += f; rem.push([exact - Math.floor(exact), st]);
  });
  rem.sort(function (a, b) { return b[0] - a[0]; });
  for (var i = 0; used < n && i < rem.length; i++) { if (counts[rem[i][1]] > 0) { out[rem[i][1]]++; used++; } }
  while (used > n) { // the minimum-one rule overshot: take from the largest
    var big = STATES.reduce(function (a, b) { return out[a] >= out[b] ? a : b; });
    out[big]--; used--;
  }
  return out;
}

// VU meter track: exactly `slots` cells (the machine's width), on a grid of
// `scale` columns (the widest member's width) so the cells line up down the
// column; the lit cells first, the rest dark; nothing past the width. The lit cells run in the
// priority ladder, highest on the left (docs/SPEC-SPRINT-DASHBOARD.md, "Fix"; the owner,
// 2026-10-07: "to the left of read cards, and to the right of critical cards"): blocker,
// critical, fix (purple), reads (one orange cell a read), then the working blue; a row's counts
// are where's <level>_working and the view's fix. One cell is one card or read on the row, so the
// lit cells always number the row's working figure (half-cell reads drew 24 cells for a friend's
// "31 / 32", the owner 2026-10-09: "Something is wrong with the rendering for a friend").
var TRACK_LEVELS = [["blocker_working", "p-blocker", "blocker"], ["critical_working", "p-critical", "critical"], ["fix", "p-fix", "fix"]];
function trackSegs(m) {
  var working = int(m.working), segs = [], words = [], cards = 0;
  TRACK_LEVELS.forEach(function (l) {
    var n = int(l[0] === "fix" && m.fix_working != null ? m.fix_working : m[l[0]]); cards += n;
    for (var j = 0; j < n; j++) segs.push(l[1]);
    if (n) words.push(n + " " + l[2]);
  });
  var reads = int(m.reads_working); cards += reads;
  for (var j = 0; j < reads; j++) segs.push("p-reader");
  if (reads) words.push(reads + " read" + (reads === 1 ? "" : "s"));
  for (var k = cards; k < working; k++) segs.push("working");
  return { segs: segs, words: words };
}
function setTrack(box, value, slots, scale, m) {
  var t = m ? trackSegs(m) : { segs: [], words: [] }, classes = [];
  if (!m) for (var j = 0; j < value; j++) t.segs.push("working");
  for (var i = 0; i < slots; i++) classes.push(t.segs[i] || "");
  setCells(box, classes, scale, trackCell(scale));
  setTitle(box, value + " working of " + slots + (t.words.length ? ": " + t.words.join(", ") : ""));
}
// "a / b" as a block of fixed width: a right-aligned in `digits` character widths, b
// left-aligned in as many, so the slash of every row in a column sits on one vertical line
// and the block can be right-aligned in its column like any number.
function frac(a, b, digits, bDigits) {
  return "<span class=\"fa\" style=\"width:" + digits + "ch\">" + a + "</span><span class=\"fs\"> / </span><span class=\"fb\" style=\"width:" + (bDigits || digits) + "ch\">" + b + "</span>";
}
function digitsOf(n) { return String(Math.max(0, n)).length; }
function makePill() { var p = el("span", "pill neutral"); p.appendChild(el("span", "dot")); p._t = quiet(el("span")); p.appendChild(p._t); return p; }
function setPill(p, text, tone, title) { setText(p._t, text); setClass(p, "pill " + tone); setTitle(p, title || text); }
function setOk(o, p, done) { setText(o, p === null ? "-" : p.toFixed(1) + "%"); setClass(o, "num" + (done ? "" : " zero")); }
// One vocabulary across the tables (SPEC.md, the owner 8:04 AM: "change 'asleep' to 'down' so we have
// consistency across all tables"): a store that still says "asleep" is shown as "down", in red.
var STATUS_TONE = { up: "good", held: "warning", down: "critical" };
// the status word alone (up, held, down): the server may carry a reason after it ("held (why)"), which is the coordinator's view, never the pill's (the owner 2026-10-04 10:40 PM)
function shownStatus(st) { var w = String(st || "").trim().split(/[\s(]/)[0]; return w === "asleep" ? "down" : w; }
// A bud shows as its person: "rowan-next" is "rowan (next)" when "rowan" is a friend too
// (the owner 2026-10-04 ~4:24 PM: "this enforces it is one person, but multiple buds").
function budLabel(name, table) {
  var i = name.indexOf("-");
  if (i > 0 && table && table[name.slice(0, i)]) return name.slice(0, i) + " (" + name.slice(i + 1) + ")";
  return name;
}
function tableScale(table) { return Math.max(1, Object.keys(table || {}).reduce(function (a, n) { return Math.max(a, int(table[n].width)); }, 0)); }
// the bar scale is shared with the friends table only while that table is shown; hidden, the fleet's own widest lane sets it (the owner 2026-10-12 ~01:42Z)
function friendsShown() { var p = document.querySelector(".panel.friends"); return !!(p && getComputedStyle(p).display !== "none"); }
function sharedScale(d) { return friendsShown() ? Math.max(tableScale(d.tables.fleet), tableScale(d.tables.friends)) : tableScale(d.tables.fleet); }

// ---------- sections ----------
function streamOrder(d) {
  var work = d.tables.work || {}, keys = [], seen = {};
  (d.streams || []).forEach(function (s) { if (work[s.Stream] && !seen[s.Stream]) { keys.push(s.Stream); seen[s.Stream] = 1; } });
  Object.keys(work).sort().forEach(function (k) { if (!seen[k]) keys.push(k); });
  return keys;
}
function streamStatus(state, c, total) {
  if (total > 0 && c.landed === total) return ["landed", "done"];
  if (state === "stopped") return ["stopped", "critical"];
  // held only for a hold set on the stream (the server's State "held", hold <stream>): cards that all
  // wait on their needs are waiting, never held (the owner 2026-10-13: "don't show held on a work stream
  // just because all its cards are in waiting state")
  if (state === "held") return ["held", "warning"];
  if (total > 0 && c.waiting === total) return ["waiting", "neutral"];
  if (c.ready + c.working + c.review + (c.fix || 0) + c.merging > 0) return ["working", "active"];
  if (state === "landed") return ["landed", "done"];
  // cards still waiting and nothing in flight (some landed already): waiting, like a stream
  // whose cards all wait
  if (c.waiting > 0) return ["waiting", "neutral"];
  return [state || "idle", "neutral"];
}

function archivedSet(d) { var a = {}; ((d.archived || {}).streams || []).forEach(function (s) { a[s] = 1; }); return a; }
function renderStreams(d) {
  var box = $("streams"), work = d.tables.work || {}, merge = d.tables.merge || {}, arch = archivedSet(d);
  var states = {}; (d.streams || []).forEach(function (s) { states[s.Stream] = s; });
  // children: 1 stream, 2 status, 3 waiting, 4 ready, 5 working, 6 review, 7 fix, 8 merging, 9 landed, 10 cost
  if (!box._head) {
    var cols = [["stream"], ["status"]];
    FLOW.forEach(function (st) { cols.push([st, st === "landed" ? "frac" : "num"]); });
    cols.push(["cost", "num"]); box._head = headRow(cols);
    box._total = el("div", "row total");
    box._total._c = [el("div", "", "Total"), el("div")];
    FLOW.forEach(function (st) { box._total._c.push(numCell(st === "landed" ? "frac" : "")); });
    box._total._c.push(quiet(numCell()));
    box._total._c.forEach(function (c) { box._total.appendChild(c); });
  }
  // Rows are grouped by status (landed, working, stopped, held), by name within a group,
  // with a thin rule between groups; each row also carries its status pill. Rows are keyed by stream, so a reorder
  // only moves nodes and flashes nothing.
  var RANK = { landed: 0, working: 1, stopped: 2, held: 3 };
  var statusOf = {};
  streamOrder(d).forEach(function (k) {
    var c = {}, total = 0; FLOW.forEach(function (st) { c[st] = int(work[k][st]); total += c[st]; });
    statusOf[k] = streamStatus((states[k] || {}).State, c, total)[0];
  });
  var rank = function (k) { return RANK[statusOf[k]] == null ? 3.5 : RANK[statusOf[k]]; };
  var keys = streamOrder(d).filter(function (k) { return !arch[k]; }).sort(function (a, b) { return rank(a) - rank(b) || (a < b ? -1 : a > b ? 1 : 0); });
  var sum = { cost: 0, totalCost: 0, workCost: 0, readCost: 0, unreconciled: 0, unpriced: 0 }, held = 0, landedStreams = 0, prevRank = null;
  // the epoch's spend, every stream's, the archived ones' too: the cost tile's scope once the
  // sprint is done (where --json's done), when the table's streams are all archived
  var epoch = { totalCost: 0, workCost: 0, readCost: 0, unpriced: 0 };
  streamOrder(d).forEach(function (k) {
    var sc = (d.stream_costs || {})[k] || {};
    var tc = cents(sc.total_cost); if (tc) epoch.totalCost += tc;
    var wc = cents(sc.work_cost); if (wc) epoch.workCost += wc;
    var rc = cents(sc.read_cost); if (rc) epoch.readCost += rc;
    epoch.unpriced += int(sc.unpriced_runs);
  });
  sum.epoch = epoch;
  var digits = digitsOf(streamOrder(d).reduce(function (a, k) { return a + FLOW.reduce(function (b, st) { return b + int(work[k][st]); }, 0); }, 0));
  FLOW.forEach(function (st) { sum[st] = 0; });
  syncRows(box, box._head, keys, function () {
    var r = { node: el("div", "row"), n: {} };
    r.name = el("div", "name"); r.nameT = el("span"); r.tag = el("span", "tag");
    r.name.appendChild(r.nameT); r.name.appendChild(r.tag);
    r.pill = makePill();
    r.node.appendChild(r.name); r.node.appendChild(r.pill);
    FLOW.forEach(function (st) { r.n[st] = numCell(st === "landed" ? "frac" : ""); r.node.appendChild(r.n[st]); });
    r.cost = quiet(numCell()); r.node.appendChild(r.cost);
    return r;
  }, function (r, k) {
    var w = work[k], m = merge[k] || {}, s = states[k] || {}, c = {}, total = 0;
    FLOW.forEach(function (st) { c[st] = int(w[st]); total += c[st]; });
    var ct = cents(w.cost);
    // an archived stream shown is drawn, and counted only on its archived line
    if (!arch[k]) {
      FLOW.forEach(function (st) { sum[st] += c[st]; });
      if (ct) sum.cost += ct;
      var sc = (d.stream_costs || {})[k] || {};
      var tc = cents(sc.total_cost); if (tc) sum.totalCost += tc;
      // the reads beside the work: the same total split by kind (sprint.TierCosts)
      var wc = cents(sc.work_cost); if (wc) sum.workCost += wc;
      var rc = cents(sc.read_cost); if (rc) sum.readCost += rc;
      sum.unpriced += int(sc.unpriced_runs);
    }
    var status = statusOf[k];
    if (status === "held") held++;
    if (status === "landed") landedStreams++;
    if (r.node.dataset.status !== status) r.node.dataset.status = status;
    r.node.classList.toggle("group-start", prevRank !== null && rank(k) !== prevRank);
    prevRank = rank(k);
    setText(r.nameT, k);
    var tone = { landed: "done", working: "active", held: "warning", stopped: "critical" }[status] || "neutral";
    setPill(r.pill, status, tone, status + (s.State ? " · stream " + s.State + (s.Since ? " since " + clockShort(new Date(s.Since)) : "") : ""));
    // an archived stream shown is marked: the total row leaves it out
    var tags = arch[k] ? ["archived"] : []; if (int(m.stuck) > 0) tags.push(m.stuck + " stuck"); if (m.ci === "red") tags.push("ci red");
    setText(r.tag, tags.join(" · "));
    FLOW.forEach(function (st) { if (st !== "landed") setNum(r.n[st], c[st]); });
    setHTML(r.n.landed, frac(c.landed, total, digits));
    setText(r.cost, ct === null ? "-" : money(ct)); setClass(r.cost, "num" + (ct === null ? " zero" : ""));
  }, box._total);
  // the sprint's unreconciled spend rides on every stream's record, an archived one's too:
  // read once, never summed
  Object.keys(d.stream_costs || {}).forEach(function (k) {
    var uc = cents(d.stream_costs[k].unreconciled); if (uc) sum.unreconciled = Math.max(sum.unreconciled, uc);
  });
  var tc = box._total._c, all = 0;
  FLOW.forEach(function (st, i) { all += sum[st]; if (st !== "landed") setNum(tc[2 + i], sum[st]); });
  setHTML(tc[LANDED_AT], frac(sum.landed, all, digits));
  setText(tc[COST_AT], money(sum.cost));
  // stream counts, never cards: "landed" here read as cards beside the table's landed column (Glenn 2026-10-14), so it names its unit
  setText($("streams-sub"), keys.length + " streams · " + landedStreams + " streams landed · " + held + " held");
  // the "landed" header is centred over its n / total cell: same width as the cell, text centred
  var lw = tc[LANDED_AT].offsetWidth ? tc[LANDED_AT].offsetWidth + "px" : "";
  if (lw && box._head.children[LANDED_AT].style.width !== lw) box._head.children[LANDED_AT].style.width = lw;
  return { sum: sum, all: all };
}

var overallLast = null;
function renderOverall(sum, all) {
  overallLast = { sum: sum, all: all };
  var box = $("overall"), per = cardsPerCell(all, cellsThatFit(box)), n = Math.ceil(all / per);
  var share = allocate(sum, all, n), classes = [];
  STATES.forEach(function (st) { for (var i = 0; i < share[st]; i++) classes.push(st); });
  setCells(box, classes, n);
  var lg = $("legend");
  if (!lg._items) {
    lg._items = {};
    STATES.forEach(function (st) {
      var it = el("span"); it.appendChild(el("i", "sw " + st)); var t = quiet(el("span")); it.appendChild(t);
      lg._items[st] = t; lg.appendChild(it);
    });
  }
  STATES.forEach(function (st) { setText(lg._items[st], st + " " + sum[st]); });
}
window.addEventListener("resize", function () { if (overallLast) renderOverall(overallLast.sum, overallLast.all); });

// One renderer for Fleet and Friends (SPEC.md, the owner 8:07 PM: "the friends table should LOOK
// exactly like the fleet table"): same columns, cells, pills (the fleet's up / held / down),
// sort, total row and load column (a friend's load is "-" until it is measured: the owner 8:08 PM,
// "for now, just put load as \"-\""). `scale` is the cell grid both
// tables share, so their columns have the same widths.
// `clamp` (Friends only, SPEC.md "Friends working, clamped to width, soft", the owner 2026-10-03 12:14 PM):
// a friend's working figure never exceeds its width; the raw count is the cell's title only.
function holdDurationWords(ms) {
  if (!(ms > 0)) return "0s";
  var s = Math.floor(ms / 1000), d = Math.floor(s / 86400); s -= d * 86400;
  var h = Math.floor(s / 3600); s -= h * 3600;
  var m = Math.floor(s / 60); s -= m * 60;
  if (d > 0) return h > 0 ? d + "d" + h + "h" : d + "d";
  if (h > 0) return m > 0 ? h + "h" + m + "m" : h + "h";
  if (m > 0) return s > 0 ? m + "m" + s + "s" : m + "m";
  return s + "s";
}
function holdReleaseWords(m, at) {
  var u = String(m.until || "").trim();
  if (u) {
    var t = Date.parse(u), now = Date.parse(at || "");
    if (!isNaN(t) && !isNaN(now)) return t > now ? "in " + holdDurationWords(t - now) : "due now";
    return "until " + u;
  }
  var r = String(m.hold_release || "").trim();
  if (r === "indefinite") return "indefinite";
  var inMatch = /\(in ([^)]+)\)/.exec(r);
  if (inMatch) return "in " + inMatch[1];
  if (/\bdue now\b/.test(r)) return "due now";
  return "indefinite";
}
function fleetLike(box, table, nameLabel, scale, clamp, at) {
  function shownWorking(m) { var w = int(m.working); return clamp ? Math.min(w, int(m.width)) : w; }
  var names = Object.keys(table || {});
  // rows by status (up, held, down), then by name
  var rank = { up: 0, held: 1, down: 2 };
  function rk(n) { var r = rank[shownStatus(table[n].status)]; return r == null ? 3 : r; }
  names.sort(function (a, b) { return rk(a) - rk(b) || a.localeCompare(b); });
  if (!box._head) {
    var cols = [[nameLabel], ["status"], ["ready", "num"], ["working"], ["", "frac"], ["done", "num"], ["ok%", "num"], ["load", "num"]];
    box._head = headRow(cols);
    var wh = box._head.children[4]; wh.textContent = ""; wh.appendChild(el("span", "alt", "working"));
    box._total = el("div", "row total");
    box._total._c = [el("div", "", "Total"), el("div"), quiet(numCell()), el("div"), quiet(numCell("frac")), quiet(numCell()), quiet(numCell())];
    box._total._c.push(el("div"));
    box._total._c.forEach(function (c) { box._total.appendChild(c); });
  }
  var t = { ready: 0, working: 0, width: 0, done: 0, ok: 0, up: 0, held: 0, down: 0, upWidth: 0, upWorking: 0, fix: 0 };
  scale = scale || tableScale(table);
  // the track column is exactly the widest track, so the figure sits right after it
  var tw = TRACK_SPAN.toFixed(3) + "rem";
  if (box.style.getPropertyValue("--track-w") !== tw) box.style.setProperty("--track-w", tw);
  box.style.removeProperty("--frac-w"); // a measured fraction column (9:31 PM) broke the layout; the column is fixed in CSS (the owner 9:34 PM: "undo that last one")
  // one digit width for every "n / width" figure in the table, the Total's sums included, so the slashes line up
  // the rows' fraction is as wide as the widest row's figure, not the total's: the total sits
  // below with no bar beside it, so reserving its digits per row left a gap to the right of the
  // bars (the owner 2026-10-06 9:25 PM); the total row uses its own digits
  // the Total row's slash sits on the same line as the rows' (the owner 9:28 PM: "the totals are
  // slightly misaligned"): every row's numerator, the total's included, is as wide as the widest
  // numerator; only the total's denominator may be wider
  var digits = names.reduce(function (a, n) { return Math.max(a, digitsOf(shownWorking(table[n])), digitsOf(int(table[n].width))); }, 1);
  digits = Math.max(digits, digitsOf(names.reduce(function (a, n) { return a + shownWorking(table[n]); }, 0)));
  var totalDigits = digitsOf(names.reduce(function (a, n) { return a + int(table[n].width); }, 0));
  syncRows(box, box._head, names, function () {
    var r = { node: el("div", "row") };
    r.name = el("div", "name"); r.pill = makePill(); r.track = el("div", "cells"); r.wf = numCell("frac");
    // fleet and friends: no numeric column flashes, only the cells
    r.wf = quiet(r.wf); r.ready = quiet(numCell()); r.done = quiet(numCell()); r.ok = quiet(numCell());
    [r.name, r.pill, r.ready, r.track, r.wf, r.done, r.ok].forEach(function (c) { r.node.appendChild(c); });
    r.load = quiet(numCell()); r.node.appendChild(r.load);
    return r;
  }, function (r, k) {
    var m = table[k], raw = int(m.working), working = shownWorking(m), width = int(m.width), done = int(m.done);
    var okv = m.okpct != null ? m.okpct : m["ok%"];
    t.ready += int(m.ready); t.working += working; t.width += width; t.done += done; t.ok += int(m.ok);
    // the total ok% weighs each row by its own ok% denominator (ok + failed), never by done, a different count (the owner 2026-10-14: "they lie")
    var okn = int(m.ok), okf = m.failed != null ? int(m.failed) : (pct(okv) > 0 ? Math.round(okn * 100 / pct(okv)) - okn : 0);
    t.okn = (t.okn || 0) + okn; t.okd = (t.okd || 0) + okn + okf;
    var st = shownStatus(m.status);
    if (st in t) t[st]++;
    if (shownStatus(m.status) === "up") { t.upWidth += width; t.upWorking += working; }
    setText(r.name, budLabel(k, table));
    var pillText = st || "-", pillTitle = pillText;
    if (!clamp && st === "held") {
      var rel = holdReleaseWords(m, at);
      pillText = st + " · " + rel;
      pillTitle = m.until ? st + " · releases at " + m.until + " (" + rel + ")" : (m.hold_release ? st + " · " + m.hold_release : pillText);
    }
    setPill(r.pill, pillText, STATUS_TONE[st] || "neutral", pillTitle);
    setTrack(r.track, working, width, scale, m);
    t.fix += int(m.fix_working != null ? m.fix_working : m.fix);
    setHTML(r.wf, frac(working, width, digits) + (m.window ? "<span class=\"win\"> · " + escHTML(m.window) + "</span>" : "")); // no reads count beside the fraction: the orange cells say it, and the label widened the column and broke the alignment (the owner 2026-10-06 9:08 PM)
    setNum(r.ready, int(m.ready)); setNum(r.done, done);
    setOk(r.ok, pct(okv), done);
    if (r.load) { var lp = pct(m.load); setText(r.load, lp === null ? "-" : lp.toFixed(1) + "%"); setClass(r.load, "num" + (lp === null ? " zero" : "")); }
  }, box._total);
  var c = box._total._c;
  setNum(c[2], t.ready);
  setText(c[3], ""); setClass(c[3], "num zero"); // no fix count here (the owner 2026-10-10: "Remove \"1 fix\"")
  // working as "x / y": the sum of working over the sum of width (SPEC.md, the owner 8:10 PM)
  setHTML(c[4], frac(t.working, t.width, digits, Math.max(digits, totalDigits)));
  setNum(c[5], t.done);
  setOk(c[6], t.okd ? t.okn / t.okd * 100 : null, t.okd);
  return { t: t, n: names.length };
}

function renderFleet(d) {
  var r = fleetLike($("fleet"), d.tables.fleet || {}, "machine", sharedScale(d), false, d.at);
  setText($("fleet-head"), r.t.up + " up · " + r.t.held + " held · " + r.t.down + " down" + sideWord(d.fleet_work, null));
  setSideOff($("fleet").closest("section"), d.fleet_work);
  return r.t;
}

function renderFriends(d) {
  var box = $("friends"), table = d.tables.friends;
  if (!table || typeof table !== "object" || !Object.keys(table).length) {
    if (!box._empty) {
      box._map = null; box._head = null; box.textContent = "";
      var e = el("div", "empty");
      e.innerHTML = "No friends table in the sprint data. This panel fills itself when <code>where --json</code> carries <code>tables.friends</code>.";
      box.appendChild(e); box._empty = true;
    }
    setText($("friends-sub"), "");
    return;
  }
  if (box._empty) { box.textContent = ""; box._empty = false; }
  var r = fleetLike(box, table, "friend", sharedScale(d), true);
  setText($("friends-sub"), r.t.up + " up · " + r.t.held + " held · " + r.t.down + " down" + sideWord(d.friends_work, d.friends_tiers));
  setSideOff(box.closest("section"), d.friends_work);
}
// A side (fleet, friends) can be switched off or limited to some tiers (set --fleet on|off,
// --fleet-tiers; the same for friends; the owner 2026-10-06 8:02 PM: "When [disabled], the table greys
// out a bit visually", "both chevron'd and open"): the head says "· off" or "· tiers flash, pro",
// and an off side's whole panel is dimmed, open or collapsed.
function sideWord(work, tiers) {
  var w = "";
  if (String(work || "on") === "off") w += " · off";
  return w;
}
function setSideOff(sec, work) { if (sec) sec.classList.toggle("off", String(work || "on") === "off"); }

// the machine pill: red when the machine line says every provider is out of credit (SPEC.md)
function setMachine(line) {
  var text = String(line || "-").replace(/^machine:\s*/, "");
  // the human page shows RUNNING, STOPPED or STALE; tick lateness is the coordinator's view only (the owner 2026-10-04 2:55 PM)
  var late = /^running\b.*tick late (\d+)s/i.exec(text);
  if (late) text = Number(late[1]) >= 60 ? "STALE" : "running";
  else if (/^running\b/i.test(text)) text = "running";
  var stopped = /STOPPED/.test(text);
  // the human page says STOPPED and nothing more; the reason is the coordinator's view (the owner 2026-10-04 10:15 PM: "STOPPED is plenty")
  if (stopped) text = "STOPPED";
  // the machine chip is gone (the owner 2026-10-12 ~00:58Z): STOPPED shows in the ETA tile instead
  // the bar pulses only while the machine runs (the owner 2026-10-04 9:14 AM)
  var running = /^(running|STALE)\b/.test(text);
  var box = $("overall"); if (box) box.classList.toggle("stopped", !running);
}

// Providers (SPEC.md, the owner 8:03 and 8:18 AM): shown only when the store carries tables.providers
var PROVIDER_STATE = { up: ["up", "good"], resting: ["resting", "warning"], "out of credit": ["out of credit", "critical"],
  out: ["out of credit", "critical"], out_of_credit: ["out of credit", "critical"], unknown: ["unknown", "dim"] };
function providerState(st) { return PROVIDER_STATE[String(st || "unknown").toLowerCase()] || [String(st), "neutral"]; }
function renderProviders(d) {
  var panel = $("providers-panel"), box = $("providers"), table = d.tables && d.tables.providers;
  if (!table || typeof table !== "object" || !Object.keys(table).length) { panel.hidden = true; return; }
  panel.hidden = false;
  if (!box._head) box._head = headRow([["provider"], ["status"], ["balance", "num"], ["spend/hour", "num"], ["note"]]);
  var rank = { up: 0, resting: 1, "out of credit": 2, unknown: 3 };
  var names = Object.keys(table).sort(function (a, b) {
    var ra = rank[providerState(table[a].state)[0]], rb = rank[providerState(table[b].state)[0]];
    return (ra == null ? 4 : ra) - (rb == null ? 4 : rb) || (a < b ? -1 : a > b ? 1 : 0);
  });
  var counts = {};
  syncRows(box, box._head, names, function () {
    var r = { node: el("div", "row") };
    r.name = el("div", "name"); r.pill = makePill(); r.bal = quiet(numCell()); r.spend = quiet(numCell()); r.note = quiet(el("div", "note"));
    [r.name, r.pill, r.bal, r.spend, r.note].forEach(function (c) { r.node.appendChild(c); });
    return r;
  }, function (r, k) {
    var p = table[k], st = providerState(p.state), bal = cents(p.balance), sp = cents(p.spend_hour);
    counts[st[0]] = (counts[st[0]] || 0) + 1;
    setText(r.name, k);
    setPill(r.pill, st[0], st[1], st[0] + (p.balance_at ? " · balance at " + p.balance_at : ""));
    setText(r.bal, bal === null ? "-" : money(bal)); setClass(r.bal, "num" + (bal === null ? " zero" : ""));
    setText(r.spend, sp === null ? "-" : money(sp)); setClass(r.spend, "num" + (sp === null ? " zero" : ""));
    setText(r.note, p.note || ""); setTitle(r.note, p.note || "");
  });
  setText($("providers-sub"), ["up", "resting", "out of credit", "unknown"].filter(function (x) { return counts[x]; })
    .map(function (x) { return counts[x] + " " + x; }).join(" · "));
}

function renderReaders(d) {
  var box = $("readers"), table = d.tables.readers || {}, names = Object.keys(table).sort();
  var F = ["asked", "reading", "ok", "broken"];
  if (!box._head) {
    box._head = headRow([["Reader"], ["asked", "num"], ["reading", "num"], ["ok", "num"], ["broken", "num"]]);
    box._total = el("div", "row total"); box._total._c = [el("div", "", "All")];
    F.forEach(function () { box._total._c.push(numCell()); });
    box._total._c.forEach(function (c) { box._total.appendChild(c); });
  }
  var t = { asked: 0, reading: 0, ok: 0, broken: 0 };
  syncRows(box, box._head, names, function () {
    var r = { node: el("div", "row"), c: {} }; r.name = el("div", "name"); r.node.appendChild(r.name);
    F.forEach(function (f) { r.c[f] = numCell(); r.node.appendChild(r.c[f]); });
    return r;
  }, function (r, k) {
    setText(r.name, k.replace(/^reader-/, ""));
    F.forEach(function (f) { var v = int(table[k][f]); t[f] += v; setNum(r.c[f], v); });
  }, box._total);
  F.forEach(function (f, i) { setNum(box._total._c[i + 1], t[f]); });
  setText($("readers-sub"), names.length + " readers");

  // merge, summed over streams the way the terminal table does
  var merge = d.tables.merge || {}, q = 0, mg = 0, stuck = 0, stopped = 0, ci = "-";
  Object.keys(merge).forEach(function (k) {
    var m = merge[k]; q += int(m.queued); mg += int(m.merged); stuck += int(m.stuck);
    if (m.state === "stopped") stopped++;
    if (m.ci === "red") ci = "red"; else if (m.ci === "green" && ci !== "red") ci = "green";
  });
  var strip = $("merge");
  if (!strip._v) {
    strip._v = {};
    [["queued", "Queued"], ["merged", "Merged"], ["stuck", "Stuck"], ["ci", "CI"], ["stopped", "Stopped"]].forEach(function (p) {
      var c = el("div"); c.appendChild(el("div", "label", p[1])); var v = el("div", "v"); c.appendChild(v); strip._v[p[0]] = v; strip.appendChild(c);
    });
  }
  setText(strip._v.queued, q); setText(strip._v.merged, mg); setText(strip._v.stuck, stuck);
  setText(strip._v.ci, ci); setText(strip._v.stopped, stopped);
  strip._v.stuck.style.color = stuck > 0 ? "var(--warning)" : "";
  strip._v.stopped.style.color = stopped > 0 ? "var(--critical)" : "";
  strip._v.ci.style.color = ci === "red" ? "var(--critical)" : ci === "green" ? "var(--good)" : "var(--text-3)";
}

function renderHero(d, s, ft) {
  var landed = int(d.landed), all = int(d.all);
  setText($("landed"), landed.toLocaleString("en-US")); setText($("all"), all.toLocaleString("en-US")); setText($("all2"), all.toLocaleString("en-US"));
  setText($("pct"), all ? (landed / all * 100).toFixed(1) + "%" : "not started"); // an empty sprint says so (the owner 2026-10-12 ~01:09Z)
  var pctSuffix = $("pct-suffix"); if (pctSuffix) pctSuffix.style.display = all ? "" : "none"; var ofAll2 = $("of-all2"); if (ofAll2) ofAll2.style.display = all ? "" : "none"; // an empty sprint says only "not started" (the owner ~01:22Z)
  var m = String(d.summary || "").match(/ETA\s+(\S+)/), at = new Date(d.at);
  var machineStopped = /STOPPED/.test(String(d.machine || "")) && !(all && landed >= all);
  if (machineStopped) { setHTML($("eta"), "<span class=\"eta-stopped\">STOP</span>"); /* STOP at every width (the owner ~01:32Z: "cleaner") */ etaAtLast = null; setText($("eta-at"), "\u00a0"); } /* the owner 2026-10-12 ~00:58Z: "When stopped put STOPPED there in red" */
  else if (m) {
    setHTML($("eta"), etaText(m[1]));
    var ms = etaMs(m[1]);
    // the viewer's zone after the time, the same source as the Updated clock (SPEC.md, the owner 10:00 PM)
    var etaAt = new Date(at.getTime() + ms);
    if (ms != null && !isNaN(at)) { etaAtLast = [etaAt, ms]; fitEtaAt(); } else { etaAtLast = null; setText($("eta-at"), "\u00a0"); }
  } else if (all && landed >= all) { setHTML($("eta"), "<span class=\"eta-done\">DONE</span>");  /* the owner 2026-10-12 ~00:32Z: "DONE with done in bright green" */ setText($("eta-at"), tookText(d));  /* the owner 2026-10-12 ~00:35Z: "Below DONE show the time it took in days, hours, minutes" */ }
  else { setText($("eta"), "-"); setText($("eta-at"), "not in the summary"); }
  // the cost tile and its tooltip cover one scope (docs/SPEC-SPRINT.md, the summary line): the
  // streams on the table, or, the sprint done, the epoch's every stream, as the hero's count
  // is. The cost is every recorded take and read of their cards in any column; the cost per
  // card is that over the cards that landed
  var c = d.done ? s.sum.epoch : s.sum, recorded = c.totalCost;
  setText($("cost"), money(recorded));
  setText($("cost-whole"), dollars(recorded));  // the phone layout shows whole dollars, rounded up (the owner 2026-10-12 ~00:30Z: "don't show the cents in the cost in iphone layout", "just round up to nearest dollar")
  var per = landed ? money(Math.ceil(recorded / landed)) + " per card" : "";
  setText($("cost-per"), per || " ");
  setText($("inflight"), s.sum.working + (s.sum.fix || 0) + s.sum.review + s.sum.merging);
  inflightLast = s.sum; renderInflight(s.sum);
  // throughput: cards landed per hour over the last hour, from the server's samples
  setText($("tput"), throughput == null ? "\u2014" : String(Math.round(throughput)));
  setTitle($("tput"), throughput == null ? "needs ten minutes of samples" : "over the last " + Math.round(throughputMinutes) + " min");
  
  setMachine(d.machine);
  setText($("coord"), d.coordinator || "-");
  // the running sprint server's version (server.py reads it from the process; absent when unknown) and the epoch (the owner ~01:20Z)
  var vc = $("ver-chip"); if (vc) { vc.hidden = !d.server_version; if (d.server_version) setText($("ver"), d.server_version); }
  setText($("epoch"), d.epoch != null ? d.epoch : "-");
  fitMetaChips(); // the coordinator's name in its chip beside the logo (the owner 2026-10-12 ~01:00Z)
}

// ---------- poll loop ----------
var shownAt = -Infinity, inFlight = 0, build = null, throughput = null, throughputMinutes = 0;
// Readers and merge are hidden by default; ?all=1 shows them.
var SHOW_ALL = /(?:^|[?&])all=1(?:&|$)/.test(location.search);
if (SHOW_ALL) $("readers-panel").hidden = false;
// The clock shows the time of the data on screen. A failed read or a
// restarting server changes nothing on the page: the last data, clock and
// dot stay exactly as they were (failures are logged by the server only).
var RELEASE = (/(?:^|[?&])release=([^&]*)/.exec(location.search) || [])[1];
RELEASE = RELEASE ? decodeURIComponent(RELEASE) : "";
var RELEASE_Q = RELEASE ? "?release=" + encodeURIComponent(RELEASE) : "";
function renderRelease(j) {
  var box = $("release"); if (!box) return;
  var names = (j.releases || []).concat(j.releases && j.releases.length ? ["all"] : []);
  var key = names.join("|") + ">" + (j.release || "");
  if (box._key === key) return;
  box._key = key;
  box.textContent = "";
  box.hidden = !names.length;
  names.forEach(function (n) {
    var a = el("a", n === j.release ? "on" : "", n);
    a.setAttribute("href", "?release=" + encodeURIComponent(n) + (SHOW_ALL ? "&all=1" : ""));
    box.appendChild(a);
  });
}
function setLive(since) {
  setClass($("live"), "live ok");
  // the viewer's zone after the time, from the browser (SPEC.md, the owner 9:59 PM): EDT now, EST after the change
  // "10:00:02 PM EDT": the digits right-aligned in a fixed 8ch box (no jump from 9 to 10 o'clock),
  // then one ordinary (proportional) space before PM and one before the zone. The browser's own time string
  // may put a narrow no-break space before PM, which the monospace face draws wide; so the
  // parts are joined here with plain spaces.
  var p = timeParts(since);
  setLiveHTML($("live-text"), "<span class=\"mono clk\"><span class=\"hms\">" + p.hms + "</span>" + SP + p.ampm +
    SP + "<span class=\"tz\">" + zoneAbbr(since) + "</span></span>");
}
function setLiveHTML(e, s) { if (e.innerHTML !== s) e.innerHTML = s; }
var DEBUG_FLASH = /(?:^|[?&])debug=flash(?:&|$)/.test(location.search), prevSig = null;
// The full form of a table, unless any of its rows or cells would overflow: then the compact form.
// Measured with the class removed and set again in the same task, so nothing flickers.
function fitTables() {
  ["streams", "fleet", "friends", "providers"].forEach(function (id) {
    var panel = document.querySelector(".panel." + id), head = panel && panel.querySelector(".row.head");
    if (!head) return;
    panel.classList.remove("compact");
    // every row, not only the header: a row's pill or name can be wider than its header
    var over = [].some.call(panel.querySelectorAll(".row"), function (r) {
      if (r.scrollWidth > r.clientWidth + 1) return true;
      // a number's flash tint bleeds 6 px into the gutter by design (.fv), so numbers get 7 px of slack
      return [].some.call(r.children, function (c) {
        var slack = (c.classList.contains("num") || c.classList.contains("frac")) ? 7 : 1;
        // a name that does not fit is ellipsized (full text on hover), never a reason for the compact layout (the owner 11:50 AM)
        if (c.classList.contains("name")) { if (c.scrollWidth > c.clientWidth + 1) c.title = c.textContent; return false; }
        return c.offsetParent !== null && !c.classList.contains("cells") && c.scrollWidth > c.clientWidth + slack;
      });
    });
    if (over) panel.classList.add("compact");
  });
}
window.addEventListener("resize", fitTables);
// Spend row (the owner 2026-10-04 10:15 AM): the pie of cards by tier and the ten most expensive streams.
var TIERS = ["flash", "pro", "heavy", "frontier"], topN = 10, lastSpendData = null;
window.addEventListener("resize", function () { if (lastSpendData) renderTopStreams(lastSpendData); });
function tierColor(t) { return "var(--tier-" + (TIERS.indexOf(t) >= 0 ? t : "other") + ")"; }
function tierCounts(obj) { // {flash: "12", pro: 3} -> [[tier, n], ...] sorted by the ladder, others last
  var out = [];
  if (!obj) return out;
  Object.keys(obj).forEach(function (k) { var n = parseInt(obj[k], 10); if (n > 0) out.push([k, n]); });
  out.sort(function (a, b) { var ia = TIERS.indexOf(a[0]), ib = TIERS.indexOf(b[0]); if (ia < 0) ia = 99; if (ib < 0) ib = 99; return ia - ib || b[1] - a[1]; });
  return out;
}
// The Cost breakdown (the owner 2026-10-04 2:43 to 3:05 PM): the pie is the spend by tier and the
// tier line in the panel's header is its legend, open or folded. The spend per tier is summed
// over every stream (tables.work[s].cost_by_tier); a tier at $0 is left out of the pie, the
// legend and the table alike. One format for the whole panel, decided once: cents (rounded up)
// when every tier shown is under $10, else whole dollars rounded up ("so we don't get weird
// stuff where the tiers' rounded $ values don't match the pie"). The pie is drawn from the
// values the legend shows, in the legend's order: by spend, most first.
function tierSpend(d) {
  var work = (d.tables && d.tables.work) || {}, byTier = {};
  Object.keys(work).forEach(function (k) {
    var b = work[k].cost_by_tier; if (!b || typeof b !== "object") return;
    // the four tiers alone: a record with no tier ("untiered") is no tier and is left out (the owner 2026-10-04 3:10 PM)
    TIERS.forEach(function (t) { var c = cents(b[t]); if (c) byTier[t] = (byTier[t] || 0) + c; });
  });
  var order = Object.keys(byTier).sort(function (a, b) { return byTier[b] - byTier[a] || a.localeCompare(b); });
  var inCents = order.length > 0 && order.every(function (t) { return byTier[t] < 1000; });
  var fmt = inCents ? money : dollars;
  // a value as shown: the cents, or the whole dollars rounded up, in cents
  var shown = function (c) { return inCents ? c : Math.ceil(c / 100 - 1e-9) * 100; };
  return { byTier: byTier, order: order, fmt: fmt, shown: shown, unit: inCents ? 1 : 100 };
}
function renderPie(d) {
  var svg = $("pie"); if (!svg) return; // no legend below the chart: its legend is the panel's header line
  var sp = tierSpend(d), vals = sp.order.map(function (t) { return [t, sp.shown(sp.byTier[t])]; });
  var total = vals.reduce(function (a, v) { return a + v[1]; }, 0);
  var key = JSON.stringify(vals) + "|" + sp.fmt(total);
  if (svg._key === key) return; // the same pie is not drawn again
  svg._key = key;
  while (svg.firstChild) svg.removeChild(svg.firstChild);
  svg.setAttribute("aria-label", "spend by tier");
  var ns = "http://www.w3.org/2000/svg";
  if (!total) { // nothing spent, or a build that serves no cost_by_tier
    var c = document.createElementNS(ns, "circle"); c.setAttribute("cx", 50); c.setAttribute("cy", 50); c.setAttribute("r", 48);
    c.setAttribute("fill", "var(--tier-other)"); c.setAttribute("opacity", ".35"); svg.appendChild(c);
    return;
  }
  var a0 = -Math.PI / 2;
  vals.forEach(function (v) {
    var frac = v[1] / total, a1 = a0 + frac * 2 * Math.PI, p = document.createElementNS(ns, "path");
    if (frac >= 0.9999) { p.setAttribute("d", "M50,2 A48,48 0 1 1 49.99,2 Z"); }
    else {
      var x0 = 50 + 48 * Math.cos(a0), y0 = 50 + 48 * Math.sin(a0), x1 = 50 + 48 * Math.cos(a1), y1 = 50 + 48 * Math.sin(a1);
      p.setAttribute("d", "M50,50 L" + x0.toFixed(2) + "," + y0.toFixed(2) + " A48,48 0 " + (frac > 0.5 ? 1 : 0) + " 1 " + x1.toFixed(2) + "," + y1.toFixed(2) + " Z");
    }
    p.setAttribute("fill", tierColor(v[0]));
    var tt = document.createElementNS(ns, "title"); tt.textContent = v[0] + " " + sp.fmt(v[1]) + " (" + Math.round(100 * frac) + "%)"; p.appendChild(tt);
    svg.appendChild(p);
    a0 = a1;
  });
}
// the In flight tile's subline (the owner 2026-10-04 3:20 and 3:25 PM): one line, two parts,
// "<working> working · <review+fix+merging> verify" (the owner 2026-10-10: review, fix and merge are one global state, verify), each number white and its words grey;
// the separate review and merging counts are in the tooltip
var inflightLast = null;
function renderInflight(sum) {
  var box = $("inflight-sub"); if (!box) return;
  var fx = sum.fix || 0;
  setTitle(box, sum.working + " working, " + sum.review + " review, " + fx + " fix, " + sum.merging + " merging");
  // the words shorten in turn when the line does not fit the tile (a phone)
  var forms = [["working", "verify"], ["work", "verify"], ["wk", "vfy"]];
  var draw = function (w) {
    var parts = [[sum.working, w[0]], [sum.review + (sum.fix || 0) + sum.merging, w[1]]].filter(function (p) { return p[0] > 0; });
    box.textContent = "";
    if (!parts.length) box.textContent = "\u00a0"; // blank when nothing is in flight; the line keeps its height (the owner 2026-10-12 ~01:10Z)
    parts.forEach(function (p, i) {
      if (i) box.appendChild(el("span", "pw", " \u00b7 "));
      box.appendChild(el("span", "pn", String(p[0]))); box.appendChild(el("span", "pw", " " + p[1]));
    });
  };
  for (var i = 0; i < forms.length; i++) { draw(forms[i]); if (fits(box)) break; }
}
window.addEventListener("resize", function () { if (inflightLast) renderInflight(inflightLast); fitEtaAt(); });
function renderTopStreams(d) {
  var box = $("top-streams"); if (!box) return;
  lastSpendData = d;
  var work = (d.tables && d.tables.work) || {}, rows = [];
  var sp = tierSpend(d), byTier = sp.byTier, order = sp.order, fmt = sp.fmt;
  Object.keys(work).forEach(function (k) {
    var w = work[k], ct = cents(w.cost); if (!ct) return;
    var n = {}; FLOW.forEach(function (c) { n[c] = parseInt(w[c], 10) || 0; });
    rows.push({ name: k, cost: ct, n: n, per: n.landed ? Math.ceil(ct / n.landed) : null, tiers: tierCounts(w.tiers), byTier: w.cost_by_tier || null });
  });
  rows.sort(function (a, b) { return b.cost - a.cost; });
  var cols = String(order.length + 1); // the tiers with spend, then total
  if (box.style.getPropertyValue("--tier-cols") !== cols) box.style.setProperty("--tier-cols", cols);
  var pie = $("pie"), rem = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
  var n = 10;
  if (pie && pie.clientWidth) n = Math.max(3, Math.min(rows.length, Math.round((pie.clientWidth + 4 * rem - 4 * rem - 1 * rem) / (5 * rem))));
  topN = n;
  // the rows are keyed by stream and patched in place like every other table's; the header and
  // each row grow or lose cells at the end when the tiers with spend change
  if (!box._head) box._head = el("div", "row head");
  var head = box._head, cellCount = order.length + 2;
  setCount(head, cellCount, function () { return quiet(el("div")); });
  putKid(head, 0, "", "stream");
  order.forEach(function (t, i) { putKid(head, 1 + i, "num", t); }); // plain headers, as every table's: the color keys are the legend's alone
  putKid(head, cellCount - 1, "num", window.matchMedia("(max-width: 1100px)").matches ? "cost" : "total"); // the phone shows only stream | its cost (the owner 2026-10-12 ~00:55Z: "it's the cost per stream")
  var shown = rows.slice(0, n);
  var byName = {}; shown.forEach(function (r) { byName[r.name] = r; });
  syncRows(box, head, shown.map(function (r) { return r.name; }), function () {
    return { node: el("div", "row") };
  }, function (r, k) {
    var row = byName[k];
    setCount(r.node, cellCount, function () { return quiet(el("div")); });
    putKid(r.node, 0, "name", k);
    order.forEach(function (t, i) { // the stream's spend on the tier, in the panel's format
      var c = row.byTier && cents(row.byTier[t]);
      putKid(r.node, 1 + i, "num" + (c ? "" : " faint"), c ? fmt(c) : "-");
    });
    putKid(r.node, cellCount - 1, "num", fmt(row.cost));
  });
  // with no stream spending, the table keeps its row lines and stays blank: no message (the owner 2026-10-12 ~01:07Z)
  var blankN = rows.length ? 0 : (pie && pie.clientWidth ? Math.max(3, Math.round((pie.clientWidth - rem) / (5 * rem))) : 10);
  if (!box._blank) box._blank = [];
  while (box._blank.length < blankN) {
    var b = el("div", "row blank");
    for (var bi = 0; bi < cellCount; bi++) b.appendChild(el("div", "", "\u00a0"));
    box._blank.push(b);
  }
  box._blank.forEach(function (b, i) {
    while (b.children.length < cellCount) b.appendChild(el("div", "", "\u00a0"));
    while (b.children.length > cellCount) b.lastChild.remove();
    if (i < blankN) { if (b.parentNode !== box) box.appendChild(b); } else if (b.parentNode === box) b.remove();
  });
  // the pie's legend in the header: each tier as the state legend draws an item, its square in
  // the tier's color, the name grey and the amount white, in the pie's order, no separators
  var ts = $("tier-sub");
  if (ts) {
    if (!order.length) setText(quiet(ts), "-");
    else {
      if (ts._val != null) { ts.textContent = ""; ts._val = null; }
      setCount(ts, order.length, function () {
        var p = el("span"); p.appendChild(el("i", "sw")); p.appendChild(quiet(el("span", "tn"))); p.appendChild(quiet(el("span", "ta"))); return p;
      });
      order.forEach(function (t, i) {
        var p = ts.children[i], bg = tierColor(t);
        if (p.children[0].style.background !== bg) p.children[0].style.background = bg;
        setText(p.children[1], t); setText(p.children[2], fmt(sp.shown(byTier[t])));
      });
    }
  }
}

function setCount(box, n, make) {
  while (box.children.length < n) box.appendChild(make());
  while (box.children.length > n) box.lastChild.remove();
}

function escHTML(s) { return String(s).replace(/[&<>"']/g, function (c) { return "&#" + c.charCodeAt(0) + ";"; }); }
function digitsOf(n) { return String(Math.max(0, n)).length; }
function makePill() { var p = el("span", "pill neutral"); p.appendChild(el("span", "dot")); p._t = quiet(el("span")); p.appendChild(p._t); return p; }
function setPill(p, text, tone, title) { setText(p._t, text); setClass(p, "pill " + tone); setTitle(p, title || text); }
function setOk(o, p, done) { setText(o, p === null ? "-" : p.toFixed(1) + "%"); setClass(o, "num" + (done ? "" : " zero")); }
var STATUS_TONE = { up: "good", held: "warning", down: "critical" };

// ---------- sections ----------
function streamOrder(d) {
  var work = d.tables.work || {}, keys = [], seen = {};
  (d.streams || []).forEach(function (s) { if (work[s.Stream] && !seen[s.Stream]) { keys.push(s.Stream); seen[s.Stream] = 1; } });
  Object.keys(work).sort().forEach(function (k) { if (!seen[k]) keys.push(k); });
  return keys;
}

function putKid(box, i, cls, text) {
  var c = box.children[i];
  if (!c._quiet) quiet(c);
  setText(c, text); setClass(c, cls);
  return c;
}

function render(d) {
  flashCount = 0;
  var s = renderStreams(d);
  renderOverall(s.sum, s.all);
  renderPie(d);
  renderTopStreams(d);
  var ft = renderFleet(d);
  renderFriends(d);
  renderProviders(d);
  if (SHOW_ALL) renderReaders(d);
  renderHero(d, s, ft);
  fitTables();
  if (DEBUG_FLASH) { // ?debug=flash: one line per refresh, same=1 when the rendered data did not change
    var sig = JSON.stringify([d.landed, d.all, d.summary, d.coordinator, d.epoch, d.machine, d.tables.work, d.tables.fleet, d.tables.friends || null,
      (d.streams || []).map(function (x) { return [x.Stream, x.State]; }), d.tables.merge]);
    var line = "flashes=" + flashCount + " same=" + (sig === prevSig ? 1 : 0);
    prevSig = sig; console.log(line);
    var log = $("flashlog") || document.body.appendChild(el("pre", "", "")); log.id = "flashlog"; log.textContent += line + "\n";
  }
}
// One snapshot, from the poll or the stream: shown only when its `at` is newer than the one on
// screen, so a slow response that arrives after a faster later one never moves the page backwards
// (SPEC.md, the owner 11:30 AM). Accepts the /api/sprint envelope ({data, build, throughput}) or a bare
// `where --json` snapshot.
function accept(j) {
  if (!j) return;
  var d = j.tables ? j : j.data;
  if (j.build) { if (build == null) build = j.build; else if (build !== j.build) { try { sessionStorage.setItem("dash-scroll", String(window.scrollY)); } catch (e) {} location.reload(); return; } }
  if (!d || !d.tables) return;
  var at = Date.parse(d.at);
  if (isNaN(at) || at <= shownAt) return;
  shownAt = at;
  if ("throughput" in j) { throughput = j.throughput == null ? null : j.throughput; throughputMinutes = j.throughputMinutes || 0; }
  renderRelease(j);
  try { render(d); } catch (e) { console.error(e); }
  setLive(new Date(at));
}
// The poll: a fixed 1 s timer that never waits on the round trip. Up to two fetches in flight;
// a third tick is skipped (console.debug). A fetch that hangs is aborted after 10 s so it cannot
// hold a slot. A failure changes nothing on the page.
var MAX_IN_FLIGHT = 2, FETCH_TIMEOUT_MS = 10000, pollTimer = null;
function poll() {
  if (inFlight >= MAX_IN_FLIGHT) { console.debug("poll: tick skipped, " + inFlight + " fetches in flight"); return; }
  inFlight++;
  var ctl = typeof AbortController === "function" ? new AbortController() : null;
  var kill = ctl && setTimeout(function () { ctl.abort(); }, FETCH_TIMEOUT_MS);
  fetch("/api/sprint" + RELEASE_Q, { cache: "no-store", signal: ctl ? ctl.signal : undefined }).then(function (r) {
    if (!r.ok) throw new Error("HTTP " + r.status);
    return r.json();
  }).then(accept).catch(function () {
    // hold: nothing changes on the page
  }).then(function () { inFlight--; if (kill) clearTimeout(kill); });
}
function startPoll() { if (pollTimer == null) { poll(); pollTimer = setInterval(poll, POLL_MS); } }

// The top bar's centre line is static CSS (index.html): nothing here moves the title, the
// pills, the clock or the button after load.

// theme: always dark (the owner 2026-10-04 2:21 PM: no light theme, no toggle)
startPoll();

// the version and epoch chips stay only while the header holds them and the clock on one line;
// the clock has priority, so the last chip goes first (the owner 2026-10-12 ~01:20Z)
function fitMetaChips() {
  // the version always stays; epoch, then the coordinator, go while the header cannot hold them and the clock on one line (the owner ~01:25Z)
  var order = [$("wordmark"), $("epoch-chip"), $("coord-chip")].filter(Boolean)  /* the wordmark goes first (the owner 2026-10-12 ~03:08Z: "only when it fits, hide otherwise") */, live = $("live"), ver = $("ver-chip"), logo = $("logo");
  if (!live || !logo) return;
  order.forEach(function (c) { c.classList.remove("nofit"); });
  var rem = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
  function overflows() {
    var tr = live.getBoundingClientRect(), lr = logo.getBoundingClientRect(), mid = (lr.top + lr.bottom) / 2, right = lr.right;
    var wrapped = tr.top > mid || tr.bottom < mid;
    [ver].concat(order).forEach(function (c) { if (c && c.offsetParent !== null) { var r = c.getBoundingClientRect(); right = Math.max(right, r.right); if (r.top > mid || r.bottom < mid) wrapped = true; } });
    return wrapped || right + 2 * rem > tr.left;
  }
  for (var k = 0; k < order.length && overflows(); k++) order[k].classList.add("nofit");
}
window.addEventListener("resize", fitMetaChips);

// keep the reader's place across the build-change reload: the page is short until the first data
// renders, so the browser's own restore lands at the top; restore after the panels fill instead
(function () {
  var y = null;
  try { y = sessionStorage.getItem("dash-scroll"); sessionStorage.removeItem("dash-scroll"); } catch (e) {}
  if (y == null) return;
  var go = function () { window.scrollTo(0, +y); };
  [500, 1500, 3000].forEach(function (ms) { setTimeout(go, ms); });
})();
