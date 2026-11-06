#!/usr/bin/env python3
"""Print a card machine counterexample one compact line per state.

    python3 -I tla/card-trace.py logs/<name>.log

For checking the card witnesses and reach traces by hand: each state shows
every producer (state, attempt, head, author, review, bounces, score,
priority, needs), every cut consumer (state, kind, row, attempt, head,
result, review), each row that is not up/width 1/unheld, the stream's hold,
the environment budget spent and the step monitor. TLC names every step
<Next> (the actions sit inside Next), so the move is read off the diff.
"""
import re
import sys

FIELD = re.compile(r'(\w+) \|-> ("[^"]*"|\d+|\{[^}]*\}|TRUE|FALSE)')


def fields(body):
    return {k: v.strip('"') for k, v in FIELD.findall(body)}


def section(text, var):
    m = re.search(r'/\\ ' + var + r' = (.*?)(?=\n/\\ |\Z)', text, re.S)
    return m.group(1) if m else ""


def prod(name, f):
    s = "%s:%s a%s h%s" % (name, f["st"], f["att"], f["head"])
    if f["author"] != "none":
        s += " by " + f["author"]
    s += " rv%s b%s" % (f["review"], f["bounces"])
    if f["score"] != "0":
        s += " sc" + f["score"]
    if f["prio"] != "normal":
        s += " " + f["prio"]
    if f["needs"] != "{}":
        s += " needs" + f["needs"].replace('"', "").replace(" ", "")
    if f.get("after", "{}") != "{}":
        s += " after" + f["after"].replace('"', "").replace(" ", "")
    return s


def cons(cid, f):
    s = "%s:%s %s@%s a%s" % (cid, f["st"], f["kind"], f["row"], f["att"])
    if f["head"] != "0":
        s += " h" + f["head"]
    if f["res"] != "-":
        s += " " + f["res"]
    if f["kind"] == "work" and f.get("prior", "0") != "0":
        s += " pr" + f["prior"]
    if f["kind"] == "read":
        s += " rv" + f["review"]
    return s


def main():
    with open(sys.argv[1]) as f:
        log = f.read()
    for line in log.splitlines():
        if "is violated" in line:
            print(line)
    parts = re.split(r'\nState (\d+): [^\n]*\n', log)
    for i in range(1, len(parts) - 1, 2):
        n, text = parts[i], parts[i + 1]
        out = []
        for name, body in re.findall(r'(\w+) \|->\s*\[([^\]]*)\]', section(text, "ps")):
            out.append(prod(name, fields(body)))
        for p, k, body in re.findall(r'<<"(\w+)", (\d+)>> :>\s*\[([^\]]*)\]', section(text, "cs")):
            f = fields(body)
            if f["st"] != "none":
                out.append(cons(p + "." + k, f))
        for r, body in re.findall(r'(r\d) \|-> \[([^\]]*)\]', section(text, "rs")):
            f = fields(body)
            if f["st"] != "up" or f["width"] != "1" or f["held"] != "FALSE" or f["flaps"] != "0":
                out.append("%s:%s w%s%s f%s" % (r, f["st"], f["width"],
                                                " held" if f["held"] == "TRUE" else "", f["flaps"]))
        if section(text, "held").strip() == "TRUE":
            out.append("STREAM-HELD")
        out.append("env" + section(text, "env").strip())
        out.append("last" + " ".join(section(text, "last").split()))
        print("%2s | %s" % (n, " | ".join(out)))


main()
