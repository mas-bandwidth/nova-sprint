#!/usr/bin/env python3
"""Write the trace-validation fixtures for StepsTrace.tla from TLC logs.

    python3 -I tla/make-steps-traces.py <logdir>

<logdir> holds the TLC logs of the Steps reach checks (r-<name>.log) and
witnesses (w-<name>.log), as ci-tlc.sh leaves them. Each fixture is a
directory traces/steps-<name>/ with StepsTraceLog.tla (TraceLog, the record
shape the Go walk emits: i, action, the action's arguments, and the logged
variables w, inbox, inflight and js; snap, phase, split and the Tick's
answer are left out, which StepsTrace allows) and StepsTrace.cfg (the
scenario's constants, TraceSpec, TraceMatched, and an
"expect:" comment line that ci-tlc.sh reads).

An ok fixture is a reach check's counterexample: a behaviour of the model,
so the trace must be matched whole (TLC exit 0). A forbidden fixture is a
state-invariant witness's counterexample: a behaviour of the faulted model
whose last step the model forbids, so TraceMatched must be violated (TLC
exit 13) with the last matched record one before the end. These stand for
the Go walk's bugs: the same fault in steps/ makes the same trace.
"""
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
SCEN = {
    "pre":   {"K": 2, "MaxAtt": 2, "MaxBounce": 0, "MaxEnv": 2, "MaxArrive": 2, "Budget": 2, "Pack": 2},
    "deal":  {"K": 4, "MaxAtt": 2, "MaxBounce": 1, "MaxEnv": 1, "MaxArrive": 1, "Budget": 2, "Pack": 2},
    "land":  {"K": 2, "MaxAtt": 1, "MaxBounce": 0, "MaxEnv": 2, "MaxArrive": 3, "Budget": 2, "Pack": 2},
    "burst": {"K": 2, "MaxAtt": 2, "MaxBounce": 0, "MaxEnv": 0, "MaxArrive": 4, "Budget": 1, "Pack": 1},
}

# fixture name -> (log, scenario, "ok" or "forbidden")
FIXTURES = {
    "ok-preempted":     ("r-StepsNeverPreempted.log", "pre", "ok"),
    "ok-stop-acked":    ("r-StepsNeverStopAcked.log", "pre", "ok"),
    "ok-refused":       ("r-StepsNeverRefused.log", "pre", "ok"),
    "ok-escalated":     ("r-StepsNeverEscalated.log", "deal", "ok"),
    "ok-need-released": ("r-StepsNeverNeedReleased.log", "deal", "ok"),
    "ok-red":           ("r-StepsNeverRed.log", "land", "ok"),
    "ok-land-judged":   ("r-StepsNeverLandJudged.log", "land", "ok"),
    "ok-carried":       ("r-StepsNeverCarried.log", "burst", "ok"),
    "bad-dealt-twice":  ("w-StepsDealtTwice.log", "pre", "forbidden"),
    "bad-width-over":   ("w-StepsWidthOver.log", "burst", "forbidden"),
    "bad-stop-early":   ("w-StepsStopEarly.log", "pre", "forbidden"),
    "bad-no-overlap":   ("w-StepsNoOverlapCheck.log", "deal", "forbidden"),
    "bad-tier":         ("w-StepsTierUnchecked.log", "deal", "forbidden"),
    "bad-carry-dropped": ("w-StepsCarryDropped.log", "burst", "forbidden"),
}

LOGGED = ["w", "inbox", "inflight", "js"]


def states(text):
    parts = re.split(r'^State \d+: .*$', text, flags=re.M)[1:]
    out = []
    for body in parts:
        body = re.split(r'^\s*$', body.strip("\n"), maxsplit=1, flags=re.M)[0]
        vs = {}
        for m in re.finditer(r'^/\\ (\w+) = (.*?)(?=^/\\ |\Z)', body, flags=re.M | re.S):
            vs[m.group(1)] = re.sub(r'\s+', ' ', m.group(2)).strip()
        out.append(vs)
    return out


def cons(wtext):
    """slot -> (st, res) from a printed w."""
    out = {}
    for m in re.finditer(r'<<"(\w+)", (\d+)>> :> \[(.*?)\]', wtext):
        f = dict(re.findall(r'(\w+) \|-> ("[^"]*"|\d+)', m.group(3)))
        out[(m.group(1), int(m.group(2)))] = (f["st"].strip('"'), f["res"].strip('"'))
    return out


def items(inbox):
    """(stamp, id, out) per inbox item, whatever order TLC prints fields in."""
    out = []
    for m in re.finditer(r'\[([^\[\]]*)\]', inbox):
        body = m.group(1)
        out.append((re.search(r'stamp \|-> (\d+)', body).group(1),
                    re.search(r'id \|-> (<<\d+, "\w+", <<"\w+", \d+>>, \d+>>)', body).group(1),
                    re.search(r'out \|-> "(\w+)"', body).group(1)))
    return out


def action(a, b):
    if a["phase"] != b["phase"]:
        return ("Snap", {}) if b["phase"] == '"snapped"' else ("Tick", {})
    if a["env"] != b["env"]:
        return ("EnvMove", {})
    if a["arrived"] != b["arrived"]:
        n = int(b["arrived"]) - int(a["arrived"])
        new = items(b["inbox"])[-n:]
        return ("EdgeResult", {"id": new[0][1], "out": '"%s"' % new[0][2], "n": str(n)})
    ca, cb = cons(a["w"]), cons(b["w"])
    for k in cb:
        if ca[k][0] == "working" and cb[k][0] == "done":
            return ("Report", {"card": '<<"%s", %s>>' % k, "res": '"%s"' % cb[k][1]})
    gone = landed_judged(a["js"]) - landed_judged(b["js"])
    if len(gone) == 1:
        p = gone.pop()
        st, red = producer(b["w"], p)
        choice = "land" if st == "landed" else "fix" if red and st != "merging" else "retry"
        return ("Answer", {"p": '"%s"' % p, "choice": '"%s"' % choice})
    raise SystemExit("cannot name the step between two states")


def landed_judged(js):
    """The producers with a land judgement in a printed js."""
    return set(re.findall(r'kind \|-> "land", about \|-> "(\w+)"', js)) | \
        set(re.findall(r'about \|-> "(\w+)", kind \|-> "land"', js))


def producer(wtext, p):
    """(st, red) of producer p in a printed w."""
    m = re.search(r'\b%s \|-> \[(.*?)\]' % re.escape(p), wtext)
    f = dict(re.findall(r'(\w+) \|-> ("[^"]*"|\w+)', m.group(1)))
    return f["st"].strip('"'), f["red"] == "TRUE"


def record(i, act, args, st):
    fields = [("i", str(i)), ("action", '"%s"' % act)] + list(args.items())
    fields += [(v, st[v]) for v in LOGGED]
    return "[" + ", ".join("%s |-> %s" % f for f in fields) + "]"


def main():
    if len(sys.argv) != 2:
        raise SystemExit(__doc__)
    logdir = sys.argv[1]
    for name, (log, scen, kind) in sorted(FIXTURES.items()):
        sts = states(open(os.path.join(logdir, log)).read())
        recs = [record(1, "Init", {}, sts[0])]
        for i in range(1, len(sts)):
            act, args = action(sts[i - 1], sts[i])
            recs.append(record(i + 1, act, args, sts[i]))
        d = os.path.join(HERE, "traces", "steps-" + name)
        os.makedirs(d, exist_ok=True)
        with open(os.path.join(d, "StepsTraceLog.tla"), "w") as f:
            f.write("---- MODULE StepsTraceLog ----\n")
            f.write("\\* Generated by make-steps-traces.py from %s; do not edit.\n" % log)
            f.write("EXTENDS TLC\n\nTraceLog == <<\n  ")
            f.write(",\n  ".join(recs))
            f.write("\n>>\n====\n")
        expect = "matched" if kind == "ok" else "forbidden at %d" % len(recs)
        with open(os.path.join(d, "StepsTrace.cfg"), "w") as f:
            f.write("\\* Generated by make-steps-traces.py from %s; do not edit.\n" % log)
            f.write("\\* expect: %s\n" % expect)
            f.write('CONSTANT Scenario = "%s"\n' % scen)
            for k, v in SCEN[scen].items():
                f.write("CONSTANT %s = %d\n" % (k, v))
            f.write("SPECIFICATION TraceSpec\nPROPERTY TraceMatched\n")
            f.write("CHECK_DEADLOCK FALSE\n")
        print("%s: %d records, %s" % (name, len(recs), expect))


main()
