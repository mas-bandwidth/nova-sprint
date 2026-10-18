// A stream's own page (the owner, 2026-10-18: "Maybe we can click on a work stream by name, and this
// takes you to a page showing all details for that work stream, including all its cards, sankey for
// all cards if applicable, dependencies between cards etc etc etc."): /api/stream/<name>, drawn with
// the charts' pieces (charts.js) and Pele.
(function () {
  "use strict";
  var C = window.novaCharts, el = C.el, plural = C.plural;
  function ago(sec, now) {
    if (!sec) return "-";
    var m = Math.max(0, Math.round((now - sec) / 60));
    if (m < 60) return m + "m";
    if (m < 2880) return Math.floor(m / 60) + "h" + (m % 60) + "m";
    return Math.floor(m / 1440) + "d" + Math.floor((m % 1440) / 60) + "h";
  }
  function clock(sec) { if (!sec) return "-"; var d = new Date(sec * 1000); return d.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }); }
  function streamPage() {
    var name = decodeURIComponent(location.pathname.replace(/^\/stream\//, ""));
    document.title = name + " · nova-sprint";
    var $ = function (id) { return document.getElementById(id); };
    $("stream-name").textContent = name;
    function load() {
      return Promise.all([C.loadPele(), C.getJSON("/api/stream/" + encodeURIComponent(name))]).then(function (r) {
        var v = r[1];
        if (v.unavailable) { $("stream-sub").textContent = v.unavailable; return; }
        if (!v.found) { $("stream-sub").textContent = "no stream of this name is on the sprint"; }
        else $("stream-sub").textContent = [v.display, v.state ? "stream " + v.state + (v.since ? " since " + clock(Date.parse(v.since) / 1000) : "") : ""].filter(Boolean).join(" · ");
        var counts = $("stream-counts"); counts.textContent = "";
        (v.counts || []).forEach(function (f) { var t = el("div", "tile"); t.appendChild(el("div", "v", f.value)); var l = el("div", "k", f.label); if (f.label !== "cards") l.style.borderColor = "var(--s-" + f.label + ")"; t.appendChild(l); counts.appendChild(t); });
        var facts = $("stream-facts"); facts.textContent = "";
        (v.facts || []).forEach(function (f) { var d = el("div"); d.appendChild(el("dt", "", f.label)); d.appendChild(el("dd", "", f.value)); facts.appendChild(d); });
        C.drawCharts($("stream-charts"), v.charts || [], v.start, v.hours);
        var body = $("stream-cards"); body.textContent = "";
        (v.cards || []).forEach(function (c) {
          var tr = el("tr");
          [c.id, c.column, c.tier || "-", c.attempts, c.reworks, c.reads, c.who || "-", c.last || "-", (c.needs || []).join(", ") || "-", ago(c.admitted, v.at), c.landed ? clock(c.landed) : "-"].forEach(function (x, i) {
            var td = el("td", i >= 3 && i <= 5 ? "num" : (i === 9 ? "num" : ""), String(x));
            if (i === 1) { td.className = "col"; var dot = el("i"); dot.style.background = "var(--s-" + c.column + ", var(--text-3))"; td.insertBefore(dot, td.firstChild); }
            tr.appendChild(td);
          });
          body.appendChild(tr);
        });
        $("stream-cards-sub").textContent = plural((v.cards || []).length, "card", "cards");
      }).catch(function (e) { $("stream-sub").textContent = "the stream view did not load: " + e.message; });
    }
    C.every(load);
  }

  streamPage();
})();
