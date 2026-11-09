# nova-sprint default policy. Every top-level name is a question or a _helper.

_RUNG = {t: i for i, t in enumerate(TIERS)}
_SECURITY_STREAMS = []  # the coordinator lists streams whose reads go to frontier
_ESCALATE_AFTER = 2  # bounces before one rank up: guess (layer 3's engine default)
_FRONTIER_AFTER = 4  # bounces before frontier: guess (PRIORITY T19; layer 3's engine default)

def _up(t, n):
    return TIERS[min(_RUNG[t] + n, len(TIERS) - 1)]

def _security(card):
    return card.stream in _SECURITY_STREAMS  # the prose is no input (round 5)

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
    if card.bounces >= _FRONTIER_AFTER:
        return "frontier"
    if card.bounces >= _ESCALATE_AFTER:
        return _up(card.tier, 1)
    return card.tier  # at its tier from its first deal (PRIORITY T17)

def rows(card, kind, candidates):
    # Every serving row at or above the card's tier: the wanted rank and up,
    # nearest first, then the ranks between the tier and the wanted one,
    # highest first. Every candidate is listed, those with room first (the
    # rest queue). Never below the tier: candidates all serve it.
    # prefer is that rule in Go, so a tick is linear in its producers.
    return prefer(candidates, _want(card, kind))

def reads(card):
    return 1  # v1 standing rule, 2026-10-20

def verdict(card, reads):
    results = [r.result for r in reads]
    if "fix" in results:
        return "fix"
    if "ok" in results:
        return "yes"
    return "retry"  # every done read faulted: ask one more read

def width(row):
    # dogfood 2026-11-07 (Glenn: a ready card never sits): no deal-ahead for
    # anyone; a bench runs at most 4 harnesses at once (nova-reserve shared
    # slots), so its work width is 4 whatever its cores say (defect 65).
    if row.kind == "worker":
        return {"work": min(row.width, 4), "ahead": 0}
    return {"work": row.width, "ahead": 0}

def limits():
    return {
        "bounces": 12,             # dogfood 2026-11-07 (defect 75): unblock keeps the count, so the limit must leave room for the escalated attempts
        "faults": 3,               # guess
        "same_finding": 2,         # v1 DISPATCHER section 7
        "reports_per_tick": 1000,  # derived from the layer 1 load test: a guess
        "route_rest_window": 10,   # v1 (PRIORITY T20)
        "route_rest_after": 3,     # v1
        "escalate_after": _ESCALATE_AFTER,
        "frontier_after": _FRONTIER_AFTER,
        "flap_limit": 3,           # unmeasured: a guess (layer 3's engine default)
        "fleet_width_cap": 8,      # guess (roster's FleetWidthCap before round 6)
        "idle_floor": 0.5,         # guess (LESSONS 3.18: under half the open width)
        "land_target": 0.3,        # guess (layer 3's engine default, over 20 landings)
        "chain_limit": 3,          # Glenn 2026-11-08: wide and flat; add refuses a longer NEEDS chain
    }

def landing(stream):
    # rung 2 at more than four heads waiting; ci_wait a guess
    return {"backed_up": 4, "flowing": 1, "ci_wait": 600}

def timeouts():
    return {
        "row_silent_gone": 600,       # guess; never one missed challenge (LESSONS 4.7)
        "row_silent_withdraw": 1800,  # guess; above a 600 s gate (LESSONS 2.9)
        "landing_stall": 1200,        # 20m so active merges and reviews do not re-trigger stall.<stream> every minute
        "base_regate": [120, 300],    # v1 (LANDING step 8)
        "judgement": 3600,            # guess
        "route_rest_for": 1800,       # v1 (PRIORITY T20)
        "tick": 10,                   # guess: run's budget for one tick
        "ready_nag": 30,              # nag an unacknowledged deal every 30s, not every 1s
        "ready_take": 180,            # Glenn 2026-11-07: a card ready this long is dealt elsewhere
        "judgement_unseen": 300,      # Glenn 2026-11-08: a judgement open this long was pushed to nobody
        "consumer": {                 # a consumer's deadline by tier
            "flash": 3600,
            "pro": 7200,
            "heavy": 7200,
            "frontier": 7200,
        },
    }

def stitch(stream):
    return stream.cards > 1

def prices():
    # micro-dollars per million tokens: {"in", "out"}, cache_read and
    # cache_write default to in. Unlisted API models are unpriced, never $0.
    return {}

def catalog():
    # The one model a bench runs per tier, as its harness names it
    # (provider/model); the deal names it and the take receipt carries it. A
    # tier with no bench model deals to no bench: the operator names them.
    return {"bench": {}}
