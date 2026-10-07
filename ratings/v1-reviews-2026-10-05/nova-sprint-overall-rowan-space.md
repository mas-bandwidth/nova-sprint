# nova-sprint overall review, 2026-10-05

Rater: inception/mercury-2.5
Build: 0f89d7113f19b77d7a131220481269dddfe0e9cd
Verdict: GOOD WITH FIXES for an AI to use
Score: 8/10

## Reasons
Verdict GOOD WITH FIXES because the tool is well-documented and conceptually sound, but several gaps prevent it from being excellent for an AI to use cold. First confusion: the `nova-sprint help` output is not accessible without building the tool, so I couldn't run the exact commands. I relied on the CLI.md documentation which is thorough but requires understanding the twin store pattern. First doubted claim: the live dashboard at http://69.67.149.151/ is referenced but its availability and current state couldn't be verified from offline documentation alone.

A score of 10 would need: (1) the binary help output to match the CLI.md reference exactly, with all verbs and flags documented in the same style as other nova-tools; (2) a more prominent "First run" section that works with minimal setup; (3) the ratings directory to include the expected review files.

## Findings
| # | where | finding | fix | size |
|---|---|---|---|---|
| 1 | `nova-sprint help` (CLI.md) | The banner's example block requires environment variables to be set before the commands work; this is not stated as a prerequisite | Add a note before the example block saying "Set `NOVA_SPRINT_REDIS` and `NOVA_SPRINT_ACTOR` before running" | S |
| 2 | `nova-sprint card` verb (CLI.md) | The card generation tool is referenced but not fully documented alongside other verbs | Add a section for `nova-sprint card` with the same First run → verbs → exit codes structure | S |
| 3 | ratings/v1-reviews-2026-10-05/ | Empty directory - review file for this tool is missing | Include at least one review file when publishing the release | S |

## Good, keep
- The twin store pattern (mem:<file>) allows learning and testing without Redis, which is excellent for AI agents to try the tool
- The CLI.md "First run" block is executable and shows the complete card lifecycle
- The spec documents are thorough and the TLA+ models provide clear state machine specifications
