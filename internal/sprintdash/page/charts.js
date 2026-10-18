// The charts (the owner, 2026-10-18: "I would like it if you could create a stats page showing me all
// the things you can do with https://pele.run for the sprint dashboard. You could put these things
// below the existing stuff we have at the bottom."). The server writes every chart as Mermaid source
// (/api/history); this file hands each to Pele (pele.js, loaded only when the charts are about to be
// seen) and inserts the SVG. Nothing here touches the page above the charts.
(function () {
  "use strict";
  var REFRESH_MS = 60000; // the server recomputes at most once a minute
  var pele = null, peleLoading = null;
  function loadPele() {
    if (pele) return Promise.resolve(pele);
    if (!peleLoading) peleLoading = import("/pele.js").then(function (m) { pele = m; return m; });
    return peleLoading;
  }
  function el(tag, cls, text) { var e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; }

  // the charts' look, in the page's dark tokens (index.html :root)
  var css = [
    ".charts { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 40rem), 1fr)); gap: 2rem; padding: 1rem 3rem 3rem; }",
    ".chart { background: var(--bg); border: 1px solid var(--border); border-radius: 1rem; padding: 1.5rem 1.5rem 1rem; min-width: 0; }",
    ".chart.wide { grid-column: 1 / -1; }",
    ".chart.wide .svg svg { max-width: 110rem; margin: 0 auto; }",
    ".chart h3 { margin: 0; font-size: 1.625rem; line-height: 2.25rem; font-weight: 600; }",
    ".chart .says { margin: .25rem 0 .75rem; font-size: 1.25rem; line-height: 1.75rem; color: var(--text-2); }",
    ".chart .legend { display: flex; flex-wrap: wrap; gap: .25rem 1.25rem; font-size: 1.25rem; line-height: 1.75rem; color: var(--text-2); margin-bottom: .5rem; }",
    ".chart .legend i { display: inline-block; width: .875rem; height: .875rem; border-radius: .2rem; margin-right: .5rem; vertical-align: -.05rem; }",
    ".chart .empty { font-size: 1.375rem; color: var(--text-3); padding: 1rem 0 1.5rem; }",
    ".chart .svg { overflow-x: auto; }",
    ".chart .svg svg { display: block; width: 100%; max-width: 100%; height: auto; }",
    ".chart.routes .svg svg, .chart.models .svg svg, .chart.tiers .svg svg { max-width: 36rem; margin: 0 auto; }",
    ".chart.deps .svg svg { width: auto; max-width: none; }",
    ".chart, .chart .svg { --pele-bg: var(--bg); --pele-fg: var(--text); --pele-muted: var(--text-2); --pele-line: var(--text-3); --pele-surface: var(--surface); --pele-surface-alt: #181b20; --pele-border: var(--border-strong); --pele-accent: var(--s-working); --pele-critical: var(--critical); --pele-error: var(--critical); }",
    "#streams .row:not(.head):not(.total) .name > span:first-child { cursor: pointer; }",
    "#streams .row:not(.head):not(.total) .name > span:first-child:hover { text-decoration: underline; text-underline-offset: .25em; text-decoration-color: var(--text-3); }",
    ".history-sub b { color: var(--text); font-weight: 550; }",
    "@media (max-width: 600px) { .charts { padding: 1rem 16px 16px; gap: 12px; } .chart { padding: 12px; } }"
  ].join("\n");
  var style = el("style"); style.textContent = css; document.head.appendChild(style);

  // hourLabels relabels an hour axis in the viewer's own time: the server writes UTC hours
  function hourLabels(src, start, hours) {
    var ls = [];
    for (var i = 0; i < hours; i++) { var h = new Date((start + i * 3600) * 1000).getHours(); ls.push('"' + (h < 10 ? "0" : "") + h + '"'); }
    return src.replace(/^(\s*x-axis )\[[^\]]*\]/m, "$1[" + ls.join(", ") + "]");
  }

  // drawCharts keeps one card per chart in box, keyed by id, drawing a chart again only when its
  // source changed
  function drawCharts(box, charts, start, hours) {
    var seen = {};
    charts.forEach(function (c, i) {
      seen[c.id] = 1;
      var card = box.querySelector('[data-chart="' + c.id + '"]');
      if (!card) {
        card = el("div", "chart " + c.id + (c.wide ? " wide" : "")); card.dataset.chart = c.id;
        card._h = card.appendChild(el("h3")); card._says = card.appendChild(el("p", "says"));
        card._legend = card.appendChild(el("div", "legend")); card._body = card.appendChild(el("div", "svg"));
      }
      if (box.children[i] !== card) box.insertBefore(card, box.children[i] || null);
      card._h.textContent = c.title; card._says.textContent = c.says;
      var src = c.mermaid || "";
      if (src && c.hourAxis) src = hourLabels(src, start, hours);
      var key = src + "|" + (c.empty || "") + "|" + JSON.stringify(c.legend || []);
      if (card._key === key) return;
      card._key = key;
      card._legend.textContent = "";
      var pie = /^pie\b/.test(src), gantt = /^gantt\b/.test(src);
      (c.legend || []).forEach(function (l, j) {
        // a pie draws its own legend; every chart takes its series colours from the legend
        if (!pie) { var s = el("span"); var sw = el("i"); sw.style.background = l.color; s.appendChild(sw); s.appendChild(document.createTextNode(l.label)); card._legend.appendChild(s); }
        card._body.style.setProperty("--pele-series-" + (j + 1), l.color);
      });
      // a gantt draws a task in the accent and a done task in the alternate surface: work, then read
      if (gantt && c.legend && c.legend.length > 1) { card._body.style.setProperty("--pele-accent", c.legend[0].color); card._body.style.setProperty("--pele-surface-alt", c.legend[1].color); }
      if (!src) {
        if (card._mount) { card._mount.destroy(); card._mount = null; }
        card._body.textContent = ""; card._body.appendChild(el("div", "empty", c.empty || "nothing to draw"));
        return;
      }
      var opts = { zoom: c.id === "deps" ? "auto" : false, fontSize: 15, onError: function (e) { console.warn("chart " + c.id + ": " + (e && e.message || e)); } };
      if (card._mount) card._mount.update(src, opts);
      else { card._body.textContent = ""; card._mount = pele.mount(card._body, src, opts); }
    });
    Array.prototype.slice.call(box.children).forEach(function (n) { if (!seen[n.dataset.chart]) box.removeChild(n); });
  }

  function getJSON(url) {
    return fetch(url, { cache: "no-store" }).then(function (r) { if (!r.ok) throw new Error("HTTP " + r.status); return r.json(); });
  }
  function every(fn) {
    // run fn now and then each REFRESH_MS while the tab is shown
    fn();
    setInterval(function () { if (!document.hidden) fn(); }, REFRESH_MS);
  }
  function plural(n, one, many) { return n + " " + (n === 1 ? one : many); }

  // the dashboard: the last 24 hours, drawn when the panel comes near the view
  function history(panel) {
    var box = document.getElementById("history"), sub = document.getElementById("history-sub"), started = false;
    function load() {
      return Promise.all([loadPele(), getJSON("/api/history")]).then(function (r) {
        var h = r[1];
        if (h.unavailable) { box.textContent = ""; box.appendChild(el("div", "chart wide")).appendChild(el("div", "empty", h.unavailable)); return; }
        var t = h.totals || {}, pct = t.finished ? Math.round(100 * t.ok / t.finished) : 0;
        sub.textContent = plural(t.landed || 0, "landed", "landed") + " · " + plural(t.finished || 0, "attempt finished", "attempts finished") + " (" + pct + "% ok) · " + plural(t.reads || 0, "read", "reads") + " · " + plural(t.workers || 0, "worker", "workers");
        drawCharts(box, h.charts || [], h.start, h.hours);
      }).catch(function (e) { console.warn("history: " + e.message); });
    }
    var go = function () { if (!started) { started = true; every(load); } };
    // the page above is short until its first data arrives: watch only once it has drawn, so the
    // charts load when they come near the view, never with the page
    setTimeout(function () {
      if (!("IntersectionObserver" in window)) { go(); return; }
      var io = new IntersectionObserver(function (es) { if (es.some(function (e) { return e.isIntersecting; })) { io.disconnect(); go(); } }, { rootMargin: "600px 0px" });
      io.observe(panel);
    }, 3000);
  }

  // the stream names in the Work table open the stream's page (the owner, 2026-10-18: "Maybe we can
  // click on a work stream by name, and this takes you to a page showing all details for that work
  // stream"): one listener on the table; the table itself is drawn as before
  function streamLinks(table) {
    table.addEventListener("click", function (e) {
      var span = e.target.closest && e.target.closest(".row .name > span:first-child");
      if (!span) return;
      var row = span.closest(".row");
      if (!row || row.classList.contains("head") || row.classList.contains("total")) return;
      var name = span.textContent.trim();
      if (!name) return;
      var url = "/stream/" + encodeURIComponent(name);
      if (e.metaKey || e.ctrlKey) window.open(url, "_blank"); else location.href = url;
    });
  }

  // the pieces a stream's page (stream.js) draws with
  window.novaCharts = { el: el, loadPele: loadPele, drawCharts: drawCharts, getJSON: getJSON, every: every, plural: plural };

  var panel = document.getElementById("history-panel");
  if (panel) history(panel);
  var table = document.getElementById("streams");
  if (table) streamLinks(table);
})();
