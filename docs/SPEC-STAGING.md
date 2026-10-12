# SPEC-STAGING

This spec describes how the member stages a card's job from a brief, including what
happens when the brief names a sibling repository.

## The staged environment

The member stages a job into `jobs/<job>/`: it writes `JOB.md`, clones the card's
repository at its base into the job's `repo/` checkout, and prepares whatever the
card's harness needs. The child works in `$JOB/repo` and ends as `JOB.md` says.

## The siblings directory

A brief may reach outside its own tree to read a sibling repository (docs/SPEC-CARD-CONTRACT.md,
the SIBLINGS header): the schema conformance cards, for example, compare a leg against a
reference corpus in another repository. At staging the member clones each SIBLINGS
repository at its ref into

    $JOB/siblings/<name>

where `<name>` is the sibling repository's own name (the part of its `owner/name@ref`
after the slash). The harness is pointed there by `JOB.md`, which names each sibling as

    SIBLING_<NAME>=$JOB/siblings/<name>

so the harness reads `$SIBLING_SERIALIZE` (for a sibling named `serialize`) instead of
guessing a path next to the checkout.

## The mirror

The member clones a sibling from the bench mirror

    ~/nova-bench/mirror/<name>.git

where one exists. Where no mirror exists, the member clones from the forge from outside
the wall (the member's staging runs outside the wall; the card's lane never fetches).

## The refusal

A sibling that cannot be staged (the mirror is absent and the forge is unreachable) refuses
the stage with the sibling's name, and the card returns to ready. That is a bench fault,
not the worker's: no work card is dealt and nothing of the card is attempted.
