# nova-sprint default policy. Every top-level name is a question or a _helper.

_RUNG = {t: i for i, t in enumerate(TIERS)}
_SECURITY_STREAMS = []  # the coordinator lists streams whose reads go to frontier
_ESCALATE_AFTER = 2  # bounces before one rank up: guess (layer 3's engine default)
_FRONTIER_AFTER = 4  # bounces before frontier: guess (layer 3's engine default)

def _up(t, n):
    return TIERS[min(_RUNG[t] + n, len(TIERS) - 1)]

def _security(card):
    return card.stream in _SECURITY_STREAMS  # the prose is no input

def order(card):
    # Inside one declared level; the ladder is the engine's.
    # LAND > FIX > READ > WORK: a fix attempt before fresh work.
    fix = 0 if card.prior_head else 1
    return (fix, card.score)

def _want(card, kind):
    # The rank wanted. A card's tier is its minimum and never changes.
    if kind == "read":
        if _security(card):
            return "frontier"
        return _up(card.writer_rank, 1)  # the rank above the writer's row
    # Escalation counts bounces, never faults.
    if card.bounces >= _FRONTIER_AFTER:
        return "frontier"
    if card.bounces >= _ESCALATE_AFTER:
        return _up(card.tier, 1)
    return card.tier  # at its tier from its first deal

def rows(card, kind, candidates):
    # Every serving row at or above the card's tier: the wanted rank and up,
    # nearest first, then the ranks between the tier and the wanted one,
    # highest first. Every candidate is listed, those with room first (the
    # rest queue). Never below the tier: candidates all serve it.
    # prefer is that rule in Go, so a tick is linear in its producers.
    return prefer(candidates, _want(card, kind))

def reads(card):
    return 1  # one read per review

def verdict(card, reads):
    results = [r.result for r in reads]
    if "fix" in results:
        return "fix"
    if "ok" in results:
        return "yes"
    return "retry"  # every done read faulted: ask one more read

def width(row):
    return {"work": row.width, "ahead": 0}  # no deal-ahead: a worker takes only what it can run now

def limits():
    return {
        "bounces": 6,              # guess
        "faults": 3,               # guess
        "same_finding": 2,         # guess
        "reports_per_tick": 1000,  # derived from the layer 1 load test: a guess
        "route_rest_window": 10,   # guess
        "route_rest_after": 3,     # guess
        "escalate_after": _ESCALATE_AFTER,
        "frontier_after": _FRONTIER_AFTER,
        "flap_limit": 3,           # unmeasured: a guess (layer 3's engine default)
        "fleet_width_cap": 8,      # guess
        "idle_floor": 0.5,         # guess: under half the open width
        "land_target": 0.9,        # guess (layer 3's engine default, over 20 landings)
    }

def landing(stream):
    # rung 2 at more than one head waiting; ci_wait a guess
    return {"backed_up": 2, "flowing": 1, "ci_wait": 600}

def timeouts():
    return {
        "row_silent_gone": 600,       # guess; never one missed challenge
        "row_silent_withdraw": 1800,  # guess; above a 600 s gate
        "landing_stall": 60,          # guess
        "base_regate": [120, 300],    # guess
        "judgement": 3600,            # guess
        "route_rest_for": 1800,       # guess
        "tick": 10,                   # guess: run's budget for one tick
        "ready_nag": 1,               # nag a row holding ready cards every second
        "ready_take": 180,            # a card ready this long is dealt elsewhere
        "consumer": {                 # a consumer's deadline by tier: nova-worker's old literals, guesses
            "flash": 900,
            "pro": 2700,
            "heavy": 5400,
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
    # The models rows may name, by provider, each with the tier it serves, and
    # the harnesses. The tiers:
    # 1. flash - deepseek flash 4.1 / luna / haiku; 2. pro - mercury 3 / terra
    # / sonnet; 3. heavy - opus 5.5 / sol; 4. frontier - fable 5.1 / astra /
    # argon. The openai and google providers are guesses, astra's provider is
    # unknown (a guess). api is the API-rate adapter.
    return {
        "models": {
            "anthropic": [
                {"name": "haiku", "tier": "flash"},
                {"name": "sonnet", "tier": "pro"},
                {"name": "opus", "tier": "heavy"},
                {"name": "fable", "tier": "frontier"},
            ],
            "openai": [  # guess
                {"name": "luna", "tier": "flash"},
                {"name": "sol", "tier": "heavy"},
            ],
            "google": [  # guess
                {"name": "terra", "tier": "pro"},
                {"name": "argon", "tier": "frontier"},
            ],
            "deepseek": [
                {"name": "deepseek-flash", "tier": "flash"},
                {"name": "deepseek-flash", "tier": "heavy"},  # the bench mapping below: a guess
                {"name": "deepseek-v4-pro", "tier": "frontier"},  # the bench mapping below: a guess
            ],
            "inception": [
                {"name": "mercury-3", "tier": "pro"},
                {"name": "mercury-2.5", "tier": "flash"},  # guess
            ],
            "paperinstruments": [
                {"name": "ultramarine-1-flash", "tier": "flash"},  # guess
            ],
            "unknown": [  # the provider is unknown: a guess
                {"name": "astra", "tier": "frontier"},
            ],
        },
        "harnesses": ["claude", "codex", "opencode", "api"],
        # The one model a bench runs per tier, as its harness names it
        # (provider/model); the deal names it and the take receipt carries it.
        # Guesses until each bench is measured.
        "bench": {
            "flash": "deepseek/deepseek-flash",
            "pro": "inception/mercury-3",
            "heavy": "deepseek/deepseek-flash",
            "frontier": "deepseek/deepseek-v4-pro",
        },
    }
