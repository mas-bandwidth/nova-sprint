# nova-sprint overall review, 2026-10-05

Rater: inception/mercury-2.5
Build: 40ea414
Verdict: GOOD WITH FIXES for an AI to use
Score: 7.5/10

## Reasons
Good documentation coverage with clear examples in README.md and CLI.md. The inbox/outbox standard is well-defined for AI coordinators. However, the GETTING-STARTED.md (line 14-28) requires installing nova-tools v1.1.0 first with multiple go install commands, which is a significant barrier for an AI trying to use this tool cold. Additionally, nova-sprint help output is extremely verbose (600+ lines) making it difficult for an AI to find specific command syntax without parsing the entire output.

## Findings
| # | where | finding | fix | size |
|---|---|---|---|---|
| 1 | `GETTING-STARTED.md:14-28` | Requires manual installation of 4 nova-tools binaries before nova-sprint works; no bundled installer | Add a single `nova-setup` script or docker-compose that installs all dependencies at once | M |
| 2 | `CLI.md` | Help output is 600+ lines; AI cannot easily find specific command syntax | Add `nova-sprint help --verb <name>` to show only one verb's usage; add short help flags | S |
| 3 | `SPEC-SPRINT.md:299-307` | Work lint is documented but not visible via `nova-sprint rules` in help output | Document lint tokens in help output or add `nova-sprint lint --help` | S |

## Good, keep
The inbox/outbox standard for friend/coordinator communication (FRIENDS.md:11-26) is well-designed for AI workflows. The twin file mode (mem:sprint.twin) allows learning without Redis setup. The comprehensive CLI reference with exit codes is thorough.