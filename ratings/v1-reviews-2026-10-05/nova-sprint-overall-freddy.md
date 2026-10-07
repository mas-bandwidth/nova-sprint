# nova-sprint overall review, 2026-10-05

Rater: inception/mercury-2.5
Build: 348a4c5
Verdict: GOOD WITH FIXES for an AI to use
Score: 7/10

## Reasons
The tool has a clear mental model (cards, streams, members, fleet) and the CLI documentation is extensive with good examples. The `where` and `view cards` commands work well and show useful status information. However, there are several issues:

First confusion: The `init` command requires a push target to be configured before any other coordinator verbs will work (`start`, `add`, etc.). This is not obvious from the help or README - the push target setup is a prerequisite that should be mentioned in the getting started flow.

First doubted claim: The README says "Open source, free forever, and you can use it right now" with a link to GETTING-STARTED.md. In practice, the tool refuses to run most useful commands without the seat login and push target configuration. This creates friction for new users trying to follow the documentation.

The tool correctly refuses to operate without proper authorization, but the error messages and documentation could be clearer about the setup requirements. The help text for `init` mentions push but doesn't explain that it blocks subsequent operations.

## Findings
| # | where | finding | fix | size |
|---|---|---|---|---|
| 1 | `nova-sprint init` and subsequent verbs | The tool refuses all coordinator verbs (`add`, `start`, etc.) if no push target is configured. This is a blocking issue for first-time users following the docs. | Add a `--no-push-required` flag for twin/learning mode, or document the `seat install` requirement prominently in the `init` help and README. | M |
| 2 | `docs/CLI.md` "### First run" section | The example commands (init, add, start, tick, etc.) will all fail with "PUSH DOWN" error unless the push target is pre-configured. | Update the examples to show the `seat install` command first, or use a flag that bypasses the push check for documentation purposes. | S |
| 3 | `nova-sprint help` output | The help text mentions "first run: no Redis needed" but doesn't mention the push target requirement which actually blocks most operations. | Add a note about the push target prerequisite in the main help text and the CLI.md first run section. | S |

## Good, keep
- The `where` command provides clear status tables showing work state across cards, friends, and fleet.
- The twin store (`mem:<file>`) works well for testing without Redis.
- The card lint and brief validation are thorough and prevent malformed cards from being added.
