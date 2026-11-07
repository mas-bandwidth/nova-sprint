// Checks the board's ready alarm (directive 65.5) against app.js itself: a row's
// ready cards are an alarm only after they have been ready for READY_ALARM_MS
// without a break. Run: node dash/testdata/ready-alarm-check.cjs
const fs = require("fs"), vm = require("vm"), assert = require("assert");
const src = fs.readFileSync(__dirname + "/../page/app.js", "utf8");
const take = (re) => { const m = re.exec(src); assert(m, "not found in app.js: " + re); return m[0]; };
const ctx = {};
vm.createContext(ctx);
vm.runInContext(take(/var READY_ALARM_MS = \d+;/) + "\n" + take(/function readyAge[\s\S]*?\n}\n/), ctx);
const late = (since, name, ready, now) => ctx.readyAge(since, name, ready, now) >= ctx.READY_ALARM_MS;
const since = {};
assert(!late(since, "a", 0, 0), "nothing ready is no alarm");
assert(!late(since, "a", 1, 1000), "just ready is no alarm");
assert(!late(since, "a", 1, 2500), "ready for 1.5 s is not yet past a tick");
assert(late(since, "a", 2, 3000), "ready for 2 s is");
assert(!late(since, "a", 0, 3500) && !("a" in since), "taking it clears the clock");
assert(!late(since, "a", 1, 4000), "and it starts again");
assert(late(since, "a", 1, 6000));
console.log("ready alarm: ok");
