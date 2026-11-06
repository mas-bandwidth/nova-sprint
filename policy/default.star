# nova-sprint default policy. Every top-level name is a question or a _helper.

_RUNG = {t: i for i, t in enumerate(TIERS)}
_SECURITY_STREAMS = []  # the coordinator lists streams whose reads go to frontier

def _up(t, n):
    return TIERS[min(_RUNG[t] + n, len(TIERS) - 1)]

def _security(card):
    return card.stream in _SECURITY_STREAMS or "security" in card.prose.lower()

def order(card):
    # Inside one declared level; the ladder is the engine's.
    # LAND > FIX > READ > WORK (PRIORITY D1): a fix attempt before fresh work.
    fix = 0 if card.prior_head else 1
    return (fix, card.score)

def _want(card, kind):
    # The rank wanted. A card's tier is its minimum and never changes.
    if kind == "read":
        if _security(card):
            return "frontier"
        return _up(card.writer_rank, 1)  # the rank above the writer's row
    # Escalation counts bounces, never faults (LESSONS 2.24).
    if card.bounces >= 4:
        return "frontier"  # PRIORITY T19; 4 is a guess
    if card.bounces >= 2:
        return _up(card.tier, 1)  # guess
    return card.tier  # at its tier from its first deal (PRIORITY T17)

def rows(card, kind, candidates):
    want = _RUNG[_want(card, kind)]
    use = [r for r in candidates if _RUNG[r.rank] >= want]
    if not use:  # no row of the wanted rank is open: never below the tier
        use = [r for r in candidates if _RUNG[r.rank] >= _RUNG[card.tier]]
    return [r.name for r in sorted(use, key = lambda r: _RUNG[r.rank])]

def reads(card):
    return 1  # v1 standing rule, 2026-10-20

def verdict(card, reads):
    return "fix" if any([r.result == "fix" for r in reads]) else "yes"

def width(row):
    return {"work": row.width, "ahead": row.width}  # v1 DealAhead 2

def limits():
    return {
        "bounces": 6,              # guess (PRIORITY G1: no source for n)
        "faults": 3,               # guess
        "same_finding": 2,         # v1 DISPATCHER section 7
        "reports_per_tick": 1000,  # derived from the layer 1 load test: a guess
        "route_rest_window": 10,   # v1 (PRIORITY T20)
        "route_rest_after": 3,     # v1
    }

def landing(stream):
    return {"backed_up": 4, "flowing": 1}  # guess

def timeouts():
    return {
        "row_silent_gone": 600,       # guess; never one missed challenge (LESSONS 4.7)
        "row_silent_withdraw": 1800,  # guess; above a 600 s gate (LESSONS 2.9)
        "landing_stall": 60,          # v1 (LANDING step 10)
        "base_regate": [120, 300],    # v1 (LANDING step 8)
        "judgement": 3600,            # guess
        "route_rest_for": 1800,       # v1 (PRIORITY T20)
    }

def stitch(stream):
    return stream.cards > 1

def prices():
    return {}  # micro-dollars per million tokens, by model: unpriced until set
