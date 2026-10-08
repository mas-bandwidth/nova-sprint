# nova-sprint overall review, 2026-10-05

Rater: gpt-5.6-terra
Build: 348a4c56dbbd
Verdict: GOOD WITH FIXES for an AI to use
Score: 6/10

## Reasons
This is a serious coordination tool with unusually explicit state, handoff and refusal semantics. `docs/SPEC-SPRINT.md:1657-1696` makes the lifecycle and its non-automatic exits precise, and the command I used emitted a valid JSON object from `where --json`. I would use it after a local-first repair, but not ask a cold AI to begin with it today: the advertised first lap reaches a push-target prerequisite before it can add its first card.

The first point of confusion is the split between the promised no-infrastructure local first lap at `docs/GETTING-STARTED.md:53-54` and the live-system prerequisites at `docs/GETTING-STARTED.md:55-63`; the document links out rather than saying what the very first local command is. The first claim I doubted was the reliability claim at `README.md:38-45`. The scratch use partly supports it: state was retained and the refusal did not add a card, but the primary onboarding path was blocked. A 10 would let a fresh coordinator complete the documented local lap without installing a delivery target, present a short task-oriented top-level help, and make the default JSON state proportional to the state requested.

## Findings
| # | where | finding | fix | size |
|---|---|---|---|---|
| 1 | `NOVA_SPRINT_REDIS=mem:.../scratch.twin NOVA_SPRINT_ACTOR=terra-review nova-sprint add --stream review --count 1 --one` | After the documented scratch `init`, the first attempt to add a card is refused: `PUSH DOWN: terra-review has no push target recorded`. The required `seat install` target is incompatible with the documented no-infrastructure first lap, so the first usable workflow stops before a card exists. | For a `mem:` first lap, retain notices locally and skip the push-target prerequisite; keep the existing requirement for a real server. | S (under 30 lines) |
| 2 | `nova-sprint help` | The default help is 691 lines and places the one-sentence first-run setup above a flat inventory of more than a hundred verbs. An AI can find a verb with search, but cannot quickly determine the smallest safe sequence or which verbs are coordinator-only. | Make default help a short first-lap and role-oriented index, with the exhaustive inventory behind `help all`. | S (under 30 lines) |
| 3 | `NOVA_SPRINT_REDIS=mem:.../scratch.twin NOVA_SPRINT_ACTOR=terra-review nova-sprint where --json` | The response is valid JSON (`jq -r type` returned `object`), but an empty scratch sprint returns two 144-bucket zero histories in `landedSeries`. The default payload is much larger than the current state and neither `help where` nor getting-started tells an AI to expect or avoid it. | Omit an all-zero series by default, or add an explicit series flag and document its JSON shape. | M |

## Good, keep
`nova-sprint init --readers reader-a,reader-b --members m1 --coordinator terra-review` created an isolated twin and clearly reported each state transition.
The failed `add` named the exact missing condition and an explicit remedy, and the subsequent `where --json` still reported zero cards; that no-write refusal behaviour is worth preserving.
The lifecycle specification and coordinator runbook distinguish machine moves from coordinator judgment rather than hiding a state transition behind a chat convention.
