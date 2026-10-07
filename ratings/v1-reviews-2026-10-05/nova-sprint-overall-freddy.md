# nova-sprint overall review, 2026-10-05

Rater: inception/mercury-2.5
Build: 348a4c5d6f0b9a8e7c1d2f3a4b5c6d7e8f9a0b1c (12 hex)
Verdict: GOOD WITH FIXES for an AI to use
Score: 7.5/10

## Reasons
The tool's help and CLI docs are dense but complete. The `### First run` section in docs/CLI.md provides a concrete example that exercises the full workflow. However, the banner for `nova-sprint` (line 7 of CLI.md) doesn't follow the three-question standard from the onboarding spec (what, how, how to use) - it's just a tagline. The help output for individual verbs like `nova-sprint init --help` does include exit codes and examples, which is good.

I found the first point of confusion in docs/SPRINT-COORDINATOR.md line 13-14: it assumes you know what "actor" means before defining it. The first doubted claim is that the tool "never writes the store by hand" (line 52) - but the `nova-sprint set` verb can modify sprint configuration directly, which is a form of direct store write.

## Findings
| # | where | finding | fix | size |
|---|---|---|---|---|
| 1 | docs/CLI.md line 7 | The nova-sprint banner is a tagline, not three answers to what/how/use per ONBOARDING point 6 | Replace line 7 with: "nova-sprint: a sprint of work cards dealt to a fleet of workers. How it works: one store holds work, readers, merge and fleet tables. How I use it: run `nova-sprint help` for the example block" | S |
| 2 | docs/SPRINT-COORDINATOR.md line 13 | "actor" is used without definition before line 54 where it's briefly explained | Move the definition ("the actor named by init") to line 13 or add a glossary reference | S |
| 3 | docs/CLI.md line 148 | Says NOVA_SPRINT_REDIS_USER is read from env, but line 52 says no actor default - conflicting for writes | Clarify that reads need no actor except inbox --read; writes need --actor | S |

## Good, keep
The `### First run` examples in docs/CLI.md are runnable end-to-end with `--redis mem:<file>`
Exit codes are documented per-verb (line 381-389)
The refusal grammar with remedy hints is consistent across verbs
