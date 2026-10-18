# Command reference: nova-sprint, and the tools folding into it

Moved from nova-tools docs/CLI.md on 2026-10-04.

## nova-sprint

nova-sprint: a sprint of work cards, dealt to a fleet of workers and read before they land

One store (Redis or a twin file) holds the work, readers, merge and fleet
tables and the `sprint` view. A card is one unit of work in a stream. Each tick
deals ready cards to members (machines with a width), sends finished work to
readers and queues passed work for merging by stream. Decisions it cannot
make go to the coordinator's inbox. The contract is
[SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md).

### First run

The [first-lap note](FIRST-LAP.md) walks the production command through a local
file-backed twin, including a reader heartbeat before the tick asks for a
review. A deterministic test-binary transcript is in
[TESTS.md](TESTS.md#nova-sprint). `--redis mem:<file>` selects a local twin
even when a server is configured; see the
[environment reference](ENVIRONMENT.md) for connection precedence.

### Landed series

`where --json` carries `landedSeries`: cards landed per 10 minutes over the last 24 hours, 144 buckets, split `friends` and `fleet`. A landing is a work-table move to `<stream>:landed` from any state but waiting. A sentinel's release is not work. Each card counts once, the first landing. The worker is the last `<who>:ok` of the card's work attempt (`<card>.wN`): `friend.<name>` is a friend and anything else is a fleet machine. The lander is not the worker. `where -h` states it. The text frame does not carry the series. `dashboard` reads one `where --json` and the series is on that object, so the page does not loop the log. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md), the `where` frame.

### Provider coverage

`where` prints `providers:` after its machine line: each applied provider's funding, and funded providers' unique per-tier in/enabled counts, followed by warnings for omitted funded routes or configured arrays, including empty ones, without an enabled funded native-tier route. `where --json` carries those facts in `providers` beside existing balance/rest fields, and the same gaps in `provider_gaps`. A route without an applied provider row has no fabricated funding value. An applied provider hash with missing or invalid funding remains readable, counts as not funded, and carries one named diagnostic with a provider-apply remedy; text says unknown and JSON omits its funding value, preserving the difference from an explicitly unfunded row. The full `check cards` reports kind `routes`, exits 1 for every such gap or no provider rows, and prints the route/tier name and `nova-config tier set <tier> --routes <array>,<route>`, `nova-config tier show <tier>`, or `nova-config apply --kind provider` remedy; start requires a clean full check and rechecks the same provider findings live, so funding or arrays changed after that check still refuse a start from READY or STOPPED. Empty arrays mean every enabled native-tier route; an entry from another tier does not cover a route because the deal ignores it. The coordinator can run `nova-config provider` through the sprint.

### Server version

The server also pushes completed CI receipts from `ev:github` to the coordinator
and the identified card owner as `CI <conclusion> <repo>@<sha12> pr=<number> run=<id> card=<card or ->`. Delivery is event driven, with a ten-minute recovery
sweep; retries retain the same message and once-key. Unmatched receipts go to the
seat alone. See [CI receipt notifications](SPEC-SPRINT.md#ci-receipt-notifications)
for routing, restart and hook-display limits.

`where --json` carries the running server's own version as `server_version` (the string `nova-sprint version` prints) and the server's heartbeat as `server` (`version`, `commit`, `started`, `at`): the version it runs, the source commit it was built from, when it started and when it last wrote. The run loop writes the heartbeat to the store once a second under a three-second TTL, one `SET` with an expiry a second. Both fields are absent when no server runs or its last heartbeat is older than that TTL, so a program (the dashboard's version chip) hides its version cell rather than show a stale one. The heartbeat is the deployment's record, not an epoch's: a clear never leaves a stale version behind. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#the-server).

### Verbs

```
nova-sprint init [--readers <a,b,...>] [--members <m1[:<width>],m2,...>] [--coordinator <name>] [--rules <file>]
nova-sprint add --stream <s> (<id>... | --count <n> | --sentinel <id> | --brief-dir <dir> | --brief-file <f1> --brief-file <f2>...: a card per file, its id the file's name without .md) [--partial] [--needs <a,b>] [--before <id> | --after <id> | --score <n>] [--brief <text> | --brief-file <path>: once, the brief of the cards named] [--rules <file>] [--replaces <old-id>[,<old-id>]] [--not-a-duplicate-of <card>=<reason>]...
# add and preflight name a probable duplicate (same REPO:, an overlapping PATHS glob neither lists as SHARED:, and a word overlap at or above 0.20, measured 0.273 on the timestamp fixtures and 0.095 on the two v17 landgo.go fixtures); add compares a wave in order, so a brief is also measured against every earlier card of the wave not already held (the first of two duplicates is admitted, and a later card that names the earlier one in DEPENDS-ON: or LAND-AFTER: is a follow-up), holds that brief back and admits the rest of the wave, one DUPLICATE line each; the add exits with the write's code (0 when it wrote the rest), and exits 1 only when every real card of the wave was held, unless --not-a-duplicate-of <card>=<reason> admits it and records the reason on its admission note (a reason is required; preflight prints the DUPLICATE finding); `brief check` runs the same check over the briefs it holds, against the open cards, one `duplicate` finding per held brief with add's words, and takes no `--not-a-duplicate-of` (its remedy names the flag add takes)
# add refuses an unresolved need (v1.2.17): absent ("needs <id>, which is no primary on the table or in this add"), dropped ("needs <id>, which was <outcome>"), unknown stream ("needs <id> (unknown stream): <id>'s stream <s> has no control card in this epoch") and held ("needs <id> (held): <id> is held by judgment <j> (<type>, open <age>) until it is decided; decide it: nova-sprint rework <id> --fix <text> or nova-sprint drop <id> --reason <text> ..."); an add of more than one card (--brief-dir, many briefs, --count) with one is refused whole, naming every card and need, nothing written; brief check names the same through add's plan
nova-sprint quack --streams <a,b,...> --count <n> --repo <clone url> [--tiers <t,...>] [--base <branch>]
nova-sprint release (<sentinel>... | <selector> [--dry-run]) --reason <text> [--answers <note>]
nova-sprint release check [--json] [--streams <glob>] [--window <duration>] [--merge-p90 <duration>] [--check <name>]...
nova-sprint resolve [<id>...] [--stream <s>] [--limit <n>]
nova-sprint start
# start refuses, exit 1, nothing written, while a setting the design depends on is non-working and no set of this epoch chose it: one line each, START REFUSED setting=<name> value=<v> why=<...> run: <verb> (read_cards off, fleet and friends off, fleet_tiers/friends_tiers serving no needed tier, git_identity empty or unparseable, max_attempts outside 1..100, a server run with --land=false); a value a set of this epoch chose starts with one START WARN setting=<name> value=<v> set by <who> at <when> why="<...>" run: <verb> line each, and the readers up (none for a needed tier, or fewer than reads_needed) are live state: a START WARN line, never a refusal
# start (from READY or STOPPED) reads the card check and never gates on it (v1.4.1, start-warns-never-gates; a check reports, it never gates infrastructure): when there is no clean full check of the cards now it prints one WARN line on stderr and starts anyway, exit 0: "WARN run nova-sprint check cards first: no check record for epoch <n>; the machine starts anyway" (none, as after a clear), "WARN run nova-sprint check cards: the last check found <kind:n,...>" (a failed record), "WARN run nova-sprint check cards: the last check covered stream(s) <s> only" (a --stream check), or "WARN run nova-sprint check cards: the cards changed since the check (<card> added | removed | changed: <stream (a move) | needs | priority | tier | brief>)"; a clean matching check prints no WARN; a start of a RUNNING machine changes nothing, and a mem twin (--redis mem:<file>) prints no report
# before the record's gate, start computes every unresolved need live (absent, dropped, unknown stream, held at its bound with no decision, cycle) and refuses, exit 1, nothing written, one line a card and need: "run nova-sprint check cards: <card> needs <need> (<reason>)"; a hold raised after a clean check is caught
nova-sprint stop --reason <text> [--until <time or duration>]
# stop puts the machine into STOPPED until an explicit start (v1.2.17); --reason is required; --until is optional display metadata for readers (a duration from now like 90m, a clock time like 2:04 PM or 14:04, or an RFC 3339 time), showing "STOPPED, expected back by <until> (<reason>); start ends it"; without --until it shows "STOPPED until the next start (<reason>)"
nova-sprint run [--answer-rules=false] [--idle-alarm=false] [--land | --land=false]   # since v1.2.17 the server (run --listen) lands by default and a plain run lands only on --land; --land=false is the server's opt-out, which start refuses; a land loop is refused (LAND REFUSED) while another lander's fresh record says it lands
nova-sprint tick [--answer-rules] [--idle-alarm]
nova-sprint tick --retire-only [--dry-run]
# the tick's retire checks alone, as the machine, STOPPED or RUNNING (v1.5.4): each judgment whose cause is gone (its cards off the table or moved past it, an orphan, a need that no longer blocks a card) is closed with the machine's answer, one line each, RETIRED judgment=<id> alias=<j> type="<type>" subject="<s>" answer="<words>" (WOULD RETIRE with --dry-run, which writes nothing), then TICK RETIRE OK judgments=<n> subjects=<n>; it moves no card and leaves a repair's skip judgment to repair --skipped
nova-sprint promote [--once] [--poll <duration>] [--every <duration>] [--landings <n>] [--branch <name>] [--repo-dir <clone>] [--base <branch>] [--check <command>] [--dry-run]
nova-sprint selftest [--dir <d>] [--keep]
nova-sprint goal set <name> [--file <path>] [--to file:<path>]
nova-sprint goal show [<name>]
nova-sprint goal drop <name>
nova-sprint take --as <member> [<card>@<gen>...] [--epoch <n>] [--limit <n>]
nova-sprint finish --as <member> <card>@<gen>... --epoch <n> (--head <commit> | --failed) [--report <text>] [--exit <n>] [--harness <text>] [--usage <text>]
# finish ends where the card's needs field says (review, or waiting for a need not landed); a finish the lifecycle refuses is counted on the work card, and the sixth refusal of an attempt raises one judgment, its id printed as JUDGMENT raised <id> <type>
nova-sprint progress --as <worker> <card>[@<gen>]... --epoch <n>
nova-sprint ask [<id>... | --group <id> [--expect <n>]] [--stream <s>] [--limit <n>] [--another] [--answers <note>]
# ask --answers closes cannot ask when it deals a read; the tick closes it as soon as an up reader may read, even at room
# ask <card> [--answers <j>] on a card whose "a read's branch is not on origin" judgment is open asks origin: a branch of the work holding the head asks the read again and closes it; none is refused in one line naming the judgment, rework with a fix and drop (ask --another gives the same reason; tla/ReadBranchMissing.tla)
nova-sprint queue --as <reader|member> [--wait <duration> --after <cursor>] | --stream <s>
nova-sprint read --as <reader> (--begin | --ok | --broken) [<card>...] --epoch <n> [--limit <n>] [--finding <text>] [--usage <text>] | --as <reader> --return <card> --reason <text> --epoch <n> [--exit <n>] [--harness <text>] [--usage <text>] | --as <coordinator> (--ok | --broken) <card>... --finding <text>
# read --as <coordinator> is the coordinator's own read of a primary in review, recorded on it
# as one of the reads the card needs; refused for the card's own worker. A card is never read
# below its tier: a card at a tier no reader serves waits for a reader of its tier or above.
nova-sprint accept (<id>... | --stream <s> | --read-ok | --group <id> [--expect <n>]) [--answers <note>]   # the tick accepts every primary whose reads are all ok and tells the seat (ready to merge); accept is for a held primary or a stuck case, and accept --read-ok with nothing eligible says "nothing waits: the tick accepts"
nova-sprint rework (<id>... | --group <id> [--expect <n>] | <selector> [--dry-run]) [--fix <text>] [--carry] [--answers <note>]
# rework --carry keeps each card's brief, records its latest pushed head as the brief's CARRY: line (what --widen writes), resets its bound and opens the next attempt staged from that head: the one verb that answers a bound judgment and a brief-defect judgment whose cause is coordination once its needs have landed; a card with no pushed head refuses the whole step, by name, and nothing is written
nova-sprint redo <card>... [--stream <s>] [--answers <note>]   # merge conflicts only (a card merging whose merge stuck, or a stream stopped on a conflict); for any other card, rework
nova-sprint return (<id>... | --group <id> [--expect <n>] | <selector> [--dry-run]) [--reason <text>] [--answers <note>]
nova-sprint drop (<id>... | --stream <s> --col <state> | --repo <owner/name>... --expect <n> | --group <id> [--expect <n>] | <selector> [--dry-run]) --reason <text> [--answers <note>] [--cascade | --relink <new-id> | --waive]   # a drop of a card any unlanded card needs is refused, naming the dependants, unless --cascade drops them too, --relink re-points their needs at <new-id> (refusing an unresolved target), or --waive removes the need from each (recorded with the reason): no drop strands a need
nova-sprint defer (<id>... | --stream <s> | --repo <owner/name>) --repo-dir <dir> --reason <text> [--expect <n>]   # defers waiting cards to the roadmap; refused if any unlanded card needs the card being deferred, naming the dependents (no defer strands a dependent)
nova-sprint needs set <id> (--needs <a,b> | --land-after <a,b> | --clear) [--answers <note>]   # the coordinator's alone: re-point a card's needs or soft dependencies (--land-after; '-' or '' clears them) in place; work runs in parallel, and the lander lands the two in order; a card in merging or review whose need is gone is answerable here
nova-sprint rank (<id>... | <selector> [--dry-run]) (--score <n> | --first) [--answers <note>]
nova-sprint priority <id>... | (<id>... | --stream <s> | <selector> [--dry-run]) (--blocker | --critical | --fix | --high | --normal | --low) --reason <text>
nova-sprint relink <old-id>[,<old-id>...] <new-id> [--reason <text>]
nova-sprint sentinel set <id> --needs <a,b>
nova-sprint brief check <file|dir>... [--stream <s>] [--rules <file>] [--allow-shared-paths] [--json]
nova-sprint brief <id> (--brief <text> | --brief-file <path>) [--rules <file>] [--answers <note>] [--tier <t> --reason <text>] [--needs-from-brief] [--keep-head] [--replace-in-flight] | --dir <dir> [--rules <file>] [--keep-head] [--replace-in-flight] | --group <id> [--expect <n>] (--brief-file <path> | --dir <dir>) [--answers <note>] | <id> --widen [--repo-dir <clone>] [--replace-in-flight] | <id> --tier <flash|pro|heavy|frontier> --reason <text> | <selector> (--set-base <branch> | --drop-who | --tier <t>)... [--replace-in-flight] [--dry-run]
# While working, only additions or changes to whole CARRY:, RESUME: or ROUTE NOTE: lines or scope-only widening edits (entries added to PATHS, NEW or SHARED, or START entries marked (read) added) preserve the in-flight binding automatically without --replace-in-flight. On a review card a scope-only edit writes the brief in place and leaves the card in review with its head and reads. Other brief edits require --replace-in-flight, naming the working attempt on refusal. Metadata changes through flags are not operational notes.
# A replaced brief returns an old-brief finish to ready with no head; a held review head is cleared. --keep-head keeps the in-flight or held head eligible for review and says so in the result. On a waiting sentinel, brief replaces its brief in place and checks its machine-read header lines (KIND, STREAM, TIER, PRIORITY, BENCH, NEEDS/DEPENDS-ON, CONSUMERS).
nova-sprint tier <card> --set <flash|pro|heavy|frontier> --reason <text>
nova-sprint recut <id> (--tier <flash|pro|heavy|frontier> | --brief-file <path> [--rules <file>]) [--new <id>] | <selector> (--tier <t> | --set-base <branch> | --drop-who)... [--dry-run]
nova-sprint twin <card> [--paths <extra,...>] [--needs <card,...>] [--before <card>] [--tier <t>] [--instruction <text>] [--carry]
nova-sprint continue <card> [--note <text>] [--tier <flash|pro|heavy|frontier>] [--carry <branch|sha>] [--answers <judgment>] [--dry-run]
nova-sprint move <id>... --stream <s> [--before <id> | --after <id> | --score <n>]
nova-sprint merge --stream <s> [--batch <n>] [--conflict <id> [--conflict-kind file|ledger] [--conflict-path <p>...] | --cross <id>=<other> | --red [--suspect <id>...] | --rejected | --base-red <error>] [--note <text>]
# a merge with no fact flag and no --landed is the twin's; on a real store use land, or record a pushed landing with --landed <id>@<head> --repo <dir> --base-ref <ref>
nova-sprint land [--stream <s>...] [--repo-dir <clone>] [--base <branch>] [--check <command>] [--dry-run]   # a gate runs on a member up or held by the coordinator without --return (a hold is a deal control; fleet down, hold --return, writes held_return and runs no gate); --dry-run names that bench (bench=<member>) or refuses the batch as the loop would, running nothing
nova-sprint rebase --from <branch> --to <branch> [--repo-dir <clone>] [--dry-run]
nova-sprint stream set <stream>... [--read-tier <flash|pro|heavy|default>] [--land-protected <owner/name,...|any|default>] [--release <name>] [--prose <glob,...|default>] [--seat-accept <on|off>] [--default-tier <flash|pro|heavy|default>] [--attempts <n|default>] [--base <branch>] [--reason <text>] [--answers <notes>]
nova-sprint settings [--json]   # every setting with its value and default; `set` with no flags prints the same
nova-sprint set [--read-tier <flash|pro|default>] [--read-cards <on|off|default>] [--dealt-max <duration|default>] [--go-lanes <n|default>] [--friend-idle <duration|default>] [--seat-silent-to <bus name|default>] [--friend-finish <duration|default>] [--fleet <on|off>] [--friends <on|off>] [--fleet-tiers <flash,pro,heavy,frontier|all>] [--friends-tiers <flash,pro,heavy,frontier|all>] [--reads <0|1|2|3|default>]
nova-sprint resume --stream <s> [--did <text>] [--answers <note>]

A card waits for its own tier: no cross-tier dealing. The `set --unserved-tier-falls-to` fallback is retired.
nova-sprint backup (--out <dir> [--part-bytes <n>] [--secrets-store <dir> --secrets-as <seat> --secrets-key <path> --sops <path>] | --file <path> [--dry-run])
nova-sprint demo load <backup.xz part>... [--sha256 <hex>] [--dir <dir>] [--xz <path>] [--redis-server <path>]
nova-sprint demo stop [--dir <dir>]
nova-sprint fleet beat <member> [--stop-returns <n>] [--no-room <why>] [--draining] [--live <set>] [--facts <list>] --load <percent> [--dry-run]
nova-sprint fleet up <member> [--width <n>]
nova-sprint fleet down <member>
nova-sprint lane take go --machine <m|auto> --as <worker> [--wait <duration>] [--dry-run]
nova-sprint lane give go --machine <m|auto> --as <worker> [--dry-run]
nova-sprint hold <name>... --reason <text> [--return] [--force-last]

A member that is up keeps taken cards and finishes them, including with `--return`; that
flag hands back only its dealt, untaken cards. A member that is down also returns its dead
working cards. `fleet down <member>` follows the same rule. A hold that would leave no
bench the land loop can use (the members up or held by the coordinator without `--return`,
minus this machine and any host the check refuses) is refused, exit 2, naming the member:
every landing would be refused until one is unheld. `--force-last` holds it anyway and the
log line ends `--force-last: no gate bench left`.

`--machine auto` asks the server to pick an allowed bench by committed load / slots;
a successful take prints its lease deadline and the give command. Take again within
20m to renew it. A full fleet prints its retry time and full benches, exits 1, and
asks the caller to retry then. `--wait` and `--dry-run` are refused with `auto`.

nova-sprint fleet sync [--check] [--pg <dsn>]
nova-sprint fleet level
nova-sprint friend sync [--pg <dsn>] [--root <dir>] [--every <duration>]
nova-sprint friend sync install --every <duration> [--redis <addr>] [--pg <dsn>] [--root <dir>] [--dir <dir>] [--log <file>] [--dry-run]
nova-sprint friend sync uninstall [--dir <dir>] [--dry-run]
nova-sprint collect [<friend>...] [--dead-lanes] [--pg <dsn>] [--root <dir>] [--dry-run]
nova-sprint install server --listen <address:port> [--redis <addr>] [--land=false] [--decide <dir>] [--dir <dir>] [--log <file>] [--dry-run]
nova-sprint install member --as <member> --server <address:port> [--harness <path>] [--root <dir>] [--pass <NAME,...>] [--swarm <path>] [--dir <dir>] [--log <file>] [--dry-run]
nova-sprint install friend-sync (friend sync install's flags)
nova-sprint install table --out <file> [--every <duration>] [--redis <addr>] [--dir <dir>] [--log <file>] [--dry-run]
nova-sprint uninstall server|member|friend-sync|table [--dir <dir>] [--dry-run]
nova-sprint units --check [--dir <dir>]
nova-sprint gc [--machine <m>] [--dry-run] [--max-age <d>] [--ai-root <dir>]
nova-sprint friend beat <friend> [--working <n>] [--queue <n>] [--width <n>] [--running <id>,...] [--load <percent>] [--stop-returns <n>]
nova-sprint friend down <friend> [--reason <text>] [--until <RFC3339>]
nova-sprint friend up <friend> [--width <n>]
nova-sprint friend cards <friend> [--json]
nova-sprint friend take <friend> (<id>... [--working] | --all-unstarted) [--dry-run] [--reason <text>]
nova-sprint friend give <friend> <id>... [--reason <text>]   # hands a ready card to her at its next attempt, from no friend too: its current work card Done (a brief-replaced bounce) or absent; refused when that work card is live (Ready or Working on a row), a hard pin names another friend (nova-sprint unpin), or her tiers, restriction or the card's needs refuse it; a friend not up is no refusal, the card waits on her row
 nova-sprint friend level
  nova-sprint friend stage <card>@<gen> --as friend.<name> [--bench <host>] [--dir <jobs dir>]   # clones the friend's dealt card into jobs/<job>/ at its NEXT head or BASE, creates the delivered branch, sets her own git identity in the clone's local config (the friend's nova-config row git_identity= when it names one, else the sprint row's git_identity; identity=friend or identity=sprint; when nova-config cannot be read the stage is refused), writes JOB.md (naming the finish command, and BENCH: <host>, or BENCH: local without --bench) naming the finish command; with --bench every git step and the JOB.md write run on that fleet member over ssh (jobs default ~/<friend>-jobs; the host must be a fleet member), and a friend can run it through the sprint server as `friend stage <card>@<gen> --as friend.<name> --bench <host> [--dir <d>]`, where the server's --dir must name the friend's job root on the bench (`~/<friend>-jobs` or `$HOME/<friend>-jobs`) or a directory beneath it, else the server refuses; refuses a card not on her row or a friend with no git_identity
 nova-sprint reader add <reader>... [--tiers <flash[,pro,heavy,frontier]|all|default>]   # a bare fleet member name (m1) names its member reader reader-m1; READER-ADD prints member-reader=<yes|no> per row and writes the row up
nova-sprint reader set <reader>... --tiers <flash[,pro,heavy,frontier]|all|default>   # a bare member name is prefixed too; an empty tiers cell (default) reads every tier a route serves
nova-sprint reader away <reader>...
nova-sprint reader up <reader>...   # a bare member name is prefixed too
nova-sprint reader remove <reader>...
nova-sprint stream remove <stream>...
nova-sprint stream archive <stream>...
nova-sprint stream unarchive <stream>...
nova-sprint ci <id>... (--red | --green) --epoch <n> [--head <h>] [--run <id>] [--source <s>] [--note <text>]
nova-sprint wait <note> (--for <duration> | --until <RFC3339>)
nova-sprint ack <note>... --reason <text>
nova-sprint answer [--dry-run] [--bar <p>] [--every <duration>] [--timeout <duration>] [--backend jev|fixed] [--answers <file>] [--record <file>]
# answer --every <d> is the coordinator seat's loop (60s): between passes it blocks on the inbox stream from the last note it has seen (XREAD BLOCK on the inbox), so a judgment committed there ends the wait and the next pass runs at once; a quiet inbox runs a pass at --every, the backstop that catches a lost push. The cursor is the inbox's tail as the wait woke, read before the pass, so a note committed during a pass ends the next wait at once and none is skipped. A backend that cannot block, a twin, and a loop through the sprint's server (NOVA_SPRINT_SERVER) wait --every on the clock between passes, as before
nova-sprint inbox [--open <group>] [--read] [--wait [--timeout <duration>] [--push <dir>]] [--deadline <duration>] [--stale <duration>] [--json]
nova-sprint card <id> [--brief | --fields] [--json] [--at-epoch <n>]
nova-sprint card (--all | --stream <s>) --json [--at-epoch <n>]
nova-sprint card base <id> <branch> [--repo-dir <clone>]
nova-sprint streams [--repo <owner/name>] [--release <name>] [--cards]
# every stream with its repositories, bases, release, open and landed counts, and with --cards each card in state order; repos and bases come from the union of its open cards' briefs' REPO and BASE (the source bases uses), the control card's fields only where no brief names one
nova-sprint log [--card <id>] [--stream <s>] [--member <m>] [--since <10m|RFC3339>] [--at-epoch <n>]
nova-sprint check cards [--stream <s>...] [--dry-run] [--from <export>] [--json]
# the core pre-start card check (v1.2.17; coordinator): over the epoch's work table and streams (or --from an export file or directory of card --json, card --all --json with briefs/ and trees/, or backup --file / RESTORE dump), one line a finding, CHECK <kind> <card|stream> <finding> remedy=<what to do>, then CHECK CARDS OK cards=<n> (exit 0) or CHECK CARDS FAILED kinds=<kind:n,...> (exit 1); 2 is usage or a store that did not answer. The kinds: needs (every unresolved need, one line each, CHECK needs <card> <need> <reason>: <words> remedy=<verbs>; the reasons absent, dropped, unknown stream, held (an open bound, brief-wrong or harness-fault judgment names it, with its id, type and age and the verbs that decide it) and cycle), files (a file two streams write, PATHS expanded at each card's base tree; a ledger or a test is none), same-file (two cards of a stream naming one PATHS file with no need between them), bases (a stream on the trunk or on another stream's branch, or a need across two streams' branches but a producer's sentinel), names (a stream's display name unset, its id, or a hyphenated id), stale-fix (priority fix with no attempt this epoch), stale-head (a pushed head from an attempt at or below the brief's `brief_attempt`; `brief --keep-head` exempts it), lint (the card lint brief check runs; a primary with no brief), brief-size (a brief over the store's bound, or a card record over the drain's 16384 bytes). Every live run without --dry-run writes the epoch's one check record (clean or the findings, every card's id, stream, tier, priority, needs and brief sha256, and their digest), which start reads; --dry-run and --from write no check record, and a --stream check's record never opens the start. The --from input path skips local store reads and writes, but normal client forwarding and hook checks still run first.
nova-sprint check [--max <n>] [--json]
# the store's invariants (section 9). `--json` prints the check report, including `row_forms`: the store row reader's per-record, per-form read counts (`binary`, `base64`, `json`, `legacy`, `unknown_fields`, and `refused_<reason>`). A record is counted when it is read through that reader; until one is, `row_forms` is an empty object. The plain `CHECK` line does not print the counts.
nova-sprint repair [--skipped <op>.skip] [--dry-run] [--json]
# --skipped <op>.skip [--accept-props] (v1.5.4; coordinator) reconciles a tick drain repair's skip judgment (another verb's is refused): each listed card the store holds as the skip found it, one revision past what its entry expected, whose last move line before the repair ends at the expected place and whose queued lines end where the store holds it, gets its missing move line (expected -> found, the queued lines' words and fields, its generation as stored, why in its text): RESTORE card=<id> ...; a card the log has since moved on from where the skip found it, a line already restored or kept, is SETTLED; a table property is UNVERIFIED (its value as stored is printed) unless --accept-props; anything else is REFUSED by name with why and nothing is written for it; RULE13 lines name any rule 13 violation the log with the restored lines still has. The judgment is closed only when nothing is refused or unverified and RULE13 is empty. Summary: REPAIR SKIPPED OK|REFUSED judgment=<id> op=<op> restored=<n> settled=<n> unverified=<n> refused=<n> rule13_after=<n> closed=yes|no; --dry-run prints WOULD lines and writes nothing; --op <id> returns the recorded result on a second run; exit 1 when anything is refused, unverified or left under rule 13
nova-sprint where [--watch] [--every <duration>] [--all] [--json [--cards] [--rows] [--archived] [--stale <duration>] [--at-epoch <n>]: includes landedSeries] [--release [<name>]]
nova-sprint view coordinator [--all] [--since <cursor>] [--needs] [--json]
nova-sprint view cards [--col <c>] [--stream <s>] [--holder <member>] [--by tier|stream|col|holder] [--json]
nova-sprint view worker --as <member|friend> [--since <cursor>] [--json]
nova-sprint dashboard [--listen <address:port>[,...] | none] [--pull <address:port>[,...] | none] [--logo <file>] [--every <duration>]
nova-sprint seat
nova-sprint seat login --store <secrets dir> --as <seat> --key <keyfile> --secret <NAME> --user <redis user> --redis <addr> [--sops <path>]
nova-sprint seat login --check
nova-sprint seat logout
nova-sprint seat install [--server <host:port>] [--config-seat <name> --config-dsn <dsn> --config-password-env <NAME>] [--dry-run]
nova-sprint seat uninstall [--dir <dir>] [--dry-run]
nova-sprint seat check
nova-sprint routes
nova-sprint rules
nova-sprint funded <provider> --reason <text>
nova-sprint cost reconcile [--dry-run] [--json]
nova-sprint cost reprice [--route <r>]... [--since <RFC3339>] [--dry-run] [--json]
nova-sprint stats [--routes [--since <10m|RFC3339>]] [--children]
nova-sprint audit [--stream <s>] [--stuck <10m>] [--json]
nova-sprint reads --review [--stream <s>] [--json]
nova-sprint doctor [--all] [--area <group>] [--preflight <candidate>] [--json]
# `doctor --all` runs `nova-doctor run` alongside the sprint doctor, then prints both named reports and preserves each exit and stderr; `--json` wraps both child reports, statuses and stderr in one object. `--area` scopes the sprint checks. `--preflight` remains the candidate-binary gate and cannot be combined with `--all`.
nova-sprint stats tidy (--friends | --fleet | --routes | --streams | --all)... --reason <text> [--dry-run]
nova-sprint play [--simulation] [--seed <n>] [--every <duration>] [--broken <p>] [--fail <p>] [--stuck <p>] [--cross <p>] [--down <p>] [--up <p>] [--red <p>] [--flap <p>] [--batch <n>] [--hold] [--silent <member>@<from>+<for>]... [--ticks <n>]
nova-sprint clear --confirm sprint
nova-sprint teardown --confirm sprint
```

A stream stop and `resume` each push a separate blocker message to the coordinator
and a status message to each affected card owner. The resume notice includes the
new state, actor and `--did` explanation. Delivery wakes on the committed sprint
log, with the existing five-second quiet-log retry backstop; the stop judgment
still requires its ordinary answer.


Friend availability comes only from `nova-hook`: a missing, unproven or down
hook reads `down`; a live, proven hook reads `held` when sprint's hold is set,
and `up` otherwise. Down takes precedence in the display while the underlying
sprint hold persists. `friend down` sets that hold and `friend up` clears it;
neither supplies liveness evidence. `friend beat` reports work, queue, width
and load for scheduling and reconciliation; it does not establish availability.
When the hook record becomes explicitly unhooked, the tick returns the friend's
work and review assignments to their deals, even without a work-report beat.
See [SPEC-SPRINT.md, The tables](SPEC-SPRINT.md#1-the-tables).

**`clear`** advances the epoch, then sets every working default in the new epoch (`sprint.Defaults`: read_cards on, fleet and friends on, tiers all, the bounds at their defaults, the feed alarm on; the coordinator's threshold alarms and base carried, a threshold never set at its working value), writing only what the new epoch does not hold, so no setting is absent after a clear, no alarm is turned off, and no `prop` value of the old epoch is carried; each one the old epoch held away from its default prints as `CLEAR DEFAULT <name> was <v>, now <default>` (`--json`: `defaults`). Every up fleet member has a reader row `reader-<member>`, made by `init`, by `fleet up` and by a clear when missing, up while its member is up and not held, so a cleared sprint reads with no seat action. nova-config's sprint row is never written by a clear: an unapplied `git_identity` reads as the machine identity, and `doctor` warns. See [SPEC-SPRINT.md, working defaults](SPEC-SPRINT.md#working-defaults).

**`audit`** is the one answer to "is anything blocked, stuck or looped": one
read of the work, fleet and readers tables prints one line per unlanded primary
with its id, stream, state, tier, attempt, holder (the member or friend whose
working card holds it, the lane and since when) or `-`, the age in its current
state and attempt, the ends of its last four attempts and the needs not yet
landed with their states, and its flags: `NO-HOLDER`, `ATT>=4`, `FAILS=<n>`
(six or more failed, idle-reaped, bound or brief-defect ends), `STUCK` (unchanged
past `--stuck`, default 10m) and `NEEDS-GONE`. It ends `AUDIT OK cards=<n> flagged=<n>`; `--json` is one object a line, `--stream` narrows it.

**`reads --review`** is the one answer to "who holds each review read" (`reads` is a new
verb name beside the worker's `read`): one read of the work, fleet and readers tables prints one
line per primary in review with its attempt, then each of its read cards at that attempt — the
reader (a member's reader row, or a friend), its state (`asked`, `reading`, `returned`,
`withdrawn`, `ended`), for how long, and its last read outcome (its verdict, `no verdict (ran=false)` for a read that never ran, or the return or hold reason). A card with no read out
and no open judgment is flagged `NO-READ-OUT`. It ends `READS OK review=<n> no-read-out=<n>`;
`--json` is one object a line, `--stream` narrows it.

**`check`** verifies the sprint's invariants across all tables ([SPEC-SPRINT.md section 9](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#9-what-is-always-true), "What is always true"), printing each violation. Under Rule 8 ("No card lost or made twice"), it inspects and reports orphan judgments: every open judgment must name a card placed on the table or a live stream (`judgment <id> (<type>) names <target>, not on the table`).

**`where`'s readers cells** count the read cards each reader has in flight
(`tables.readers`, and the one summed row the text frame draws): with read cards on
(the default) `asked` is the read cards dealt (ready) to the reader on the fleet
table and `reading` those it has taken (working), beside any reads of the legacy
readers table; a member reader `reader-<m>` counts its member's read cards, and a
friend reader `reader-<friend>` the friend's. The dashboard reads the same `tables`
shape and needs no change.

**The tier on line 1** (one rule, read or refused): a card brief names its tier on line 1, in
either form, `<title> tier: <t>` or the RESULT line `RESULT: <id> sha=<sha12> tier: <t>`, and
that is the tier the card is stored with and dealt on: a `tier: heavy` card's first deal draws
a heavy route, never flash. `add` prints it on each card's line (`<id> -> ready ... tier=<t>`),
and `brief` on the card it re-briefs (`<id> tier=<t>`). The card checks read it by the same
rule (`cardhdr.ReadModel`): a card brief (one with a `PATHS:` line) whose line 1 names no
tier, in either form, is refused at `add` and by `brief check` (`LINT DRIFT ... check=tier-line line=1`), unless the stream it is added to names a default tier (`stream set <s> --default-tier <flash|pro|heavy>`), which the card is then stored with as its pinned tier. A
free task (no `PATHS:`) that names none starts on flash; frontier and a brief that pins its
model name their own.

**A finish-PR card stages from the PR's own head.** A card cut to finish an open pull
request carries the PR's branch and head in the machine-read `CARRY:` line of its brief
(the line `brief --widen` writes, `CARRY: <card> attempt <n> head=<sha>`, or a finish-PR
card's `CARRY: head=<sha> branch=<branch>`): `add` stores it so and `brief` keeps it, and
the deal's packet names the PR head as the base the slot is staged from, so the worker
builds on the PR's work instead of one commit off its base. The base (`BASE:`) is still
merged in by the worker, never replaced. `add` refuses a brief whose `PR:` line or THE
TASK ("Start from the PR's branch ...") names a PR and carries no `CARRY:` line
(`LINT DRIFT ... check=pr-carry`, the remedy naming the line to add); `brief check` holds a
brief to the same rule.

A `brief` edit replaces the card's whole text, but the text dumped from the
card is the admission-time one: three rules keep what a verb set, and a `brief`
NOTE names every property it kept against the text. **Needs**: the edit's needs
are the card's, not the text's — it starts from the card's needs, maps a name its
relink record re-pointed to its twin (`needs kept: <twin> (relinked from <old>
<time>)`), and adds only names the text holds that the card does not, never a
dropped card the card's needs no longer name (`needs kept: <id> not added`); a
need the text drops stays (`needs kept: <id> (the text's DEPENDS-ON drops it)`),
unless `--needs-from-brief` takes the text whole. **The tier**:
the tier is a card field, so a text whose line 1 names another tier than the
card's recorded one keeps the recorded one (`tier kept: <recorded> (the brief's
line 1 says <t>)`), unless `--tier <t>` names one; `tier <card> --set <t>
--reason <why>` sets it without a text edit. Both refuse a tier below heavy
for a card whose `NEW:` names a new TLA+ model, a `.tla` file (the lint's new-model-is-heavy-or-above rule); a card that only edits what `tla/` holds is any tier. **The bound**: the bound hashes the
task (the body and `PATHS:`, `NEW:`, `SHARED:`, `TEST:`, `DONE-WHEN:`), so an
edit that changes only `DEPENDS-ON:`, `ORDER:`, `TLC-HOSTS:`, `MERGE:`,
`TOOLCHAIN-HOSTS:` or the tier keeps the count (`bound: same task (<n> attempts
stand)`) and a new task resets it (`bound: new task (count reset)`). Every
DEPENDS-ON or Needs item is a card id, and the card lint refuses any other item
by name (finding depends-on).

`continue <card> --note <text>` continues a card whose attempts pushed partial work
and reached its brief's bound, without judging it wrong and without starting over: it
edits the card's brief in place, a `CARRY: <card> attempt <n> head=<sha>` line at the
newest full head any attempt pushed (or the `--carry <branch|sha>` head, a branch
resolved to its head through the lander's clone of the card's repository, `--repo-dir` for a card
naming no `REPO:` line) written in its header, the
continue note appended with the UTC time, its bound reset, every open bound and brief-defect
judgment answered, and with `--tier` its tier re-pinned in the same step. A card
in review goes back to ready, its open reads retired, its next attempt staged from
the carried head. It refuses, exit 1, nothing written, when the card is landed, when
no attempt pushed a head and no `--carry` is given, and when `--carry` is not a
40-hex commit sha. `--dry-run` prints the new brief's header and the judgments it
would answer.

`card <id> --json` reads that card: its lines of the log from the card log index the
tick keeps (and the tail it has not indexed yet), never the whole log, and its own
records, its needs, its place in line and its hold from one read of the tables
(docs/SPEC-SPRINT.md section 17, the card log index). `card --all --json` prints every card on the table in one call,
one JSON object a line: `id`, `stream`, `column`, `score`, `needs`, `brief_len` and
the other fields (the brief's text is `card <id> --brief`); `card --stream <s> --json` prints one stream's.

A card's live `column` comes from its table row in `card <id> --json`,
`card --all --json`, `where --json --rows` and `needs --json`. The rows listing
keeps `state` as a deprecated alias of `column` for one release. `needs` labels
the waiting card as `card <id> column <c>` and each unmet need as
`need <id> column <c>`; JSON names both columns separately on their objects.

`needs --max n` caps the displayed streams, cards, unmet needs, width rows and
cycle ids independently across one answer. Totals, depths, roots and each
shown width's count describe the complete graph. Each truncated list has a
`more` object with `shown`, `total`, `omitted`, `first_hidden` and an exact
`command` that reveals the full list; width and cycle lists use `width_more`
and `cycle_more`. Text prints the same facts in `MORE kind=...` lines after
that list. Hidden needs still carry their count and boundary id, including
when none are displayed. `needs --max 0` prints the full graph; drill-down
commands preserve `--stream`, `--roots` and `--json` as appropriate.

A `<selector>` is `--stream <s>`, `--who <friend.<name>|friend|none>`, `--state <ready|waiting|held|merging>` and `--ids-file <path>`, combinable: the verb changes every
card it selects in one store step, prints one line per card and the total, and with
`--dry-run` lists what would change and writes nothing (docs/SPEC-SPRINT.md, "One
selector, one step").

`friend sync` wakes a friend through the bus store at `NOVA_BUS_REDIS` after
delivering her card. Its bus login reads `NOVA_BUS_REDIS_USER` and the password
variable named by `NOVA_BUS_REDIS_PASSWORD_ENV`, separately from the sprint
store's login. With no bus user it uses the default user; a failed bus send
leaves the delivered card in her inbox and records that she was not woken.

Every store verb takes `--redis <addr>` (else `NOVA_SPRINT_REDIS`, then
`NOVA_REDIS_ADDR`, then the address `seat login` recorded), `--actor <name>` (else
`NOVA_SPRINT_ACTOR`; a coordinator or seat verb may use the coordinator's name only
when its sprint hook and bus push are both proven live), `--op <id>` (the same id
again returns the recorded result), `--json` and `--max <n>` (listed items; 0 is all). The coordinator's
verbs are the coordinator's alone (the first `init` names it: `--coordinator`,
else the actor); `take`, `finish`, `read`, `fleet beat` and `friend beat` are the
workers', whose actor is the member, reader or friend named; `merge` and `ci`
are reports; `tick`, `run`, `friend clean` and `gc` are the system's. Reads need no
actor except `inbox --read`, which moves the coordinator's cursor. A set is
ids, a stream, a column, `--max n` (`--limit` is an alias), or an inbox group:
`--group <id>`, the id `inbox` prints, with `--expect <n>` the size it printed,
which refuses a group that has changed. `nova-sprint help <verb>` (or
`<verb> -h`) prints one verb's usage, flags and exit codes; `nova-sprint help
<group>` (fleet, friend, reader, goal, stream) prints one group's.

### Worker queue waits

Read `nova-sprint queue --as <worker> --json`, save its `cursor`, then repeat
`nova-sprint queue --as <worker> --wait 10m --after <cursor> --json` with each
returned cursor. The request wakes as soon as the sprint log changes, or at the
deadline, and returns the current queue. A timeout does not consume or clear its
cards. Plain output adds `CURSOR <id>`; `0-0` is an empty stream. The cursor is
sampled before the queue read, so a concurrent deal remains visible to this read
or the next wait. Through the server, send each wait in its own request.

Keep the worker's existing heartbeat and timer sweep at their existing cadence
as a backstop; a waiting reader queue records its reader beat when the queue is
read, not throughout the wait. The nova-tools worker loops move to this interface
separately. The server also checks its durable log every 15 seconds during a
quiet wait, recovering a lost wake or an epoch change.

`inbox --wait` also blocks through the server, with the existing 15-second
backstop. Its transport is `log --wait-tick <duration> --after <notes-cursor> --json`: zero duration samples the initial cursor; subsequent calls return
`cursor` and `woke`. This cursor belongs to the notes stream and cannot be used
as a queue cursor. The transport takes no log filters or historical epoch and
is served only on the coordinator listener.

### A card's priority

Every card carries a level of the ladder blocker, critical, high, reader, normal, low; a
read card is reader or its primary's higher level, a primary normal unless its brief's `PRIORITY: <level>` line, its
stream's default or `priority` sets it. `priority s1-4 --high --reason '<why>'` sets one card,
`priority --stream s2 --low --reason '<why>'` sets, in one call, every card now in the stream
(its own level overwritten) and the stream's default for cards added later, and `priority s1-4` prints the level and where it comes from;
`priority` also takes the shared selector (`--who`, `--state`, `--ids-file`, or `--stream`
with any of them, and `--dry-run`), setting every selected card in one call and no stream
default: `priority --ids-file raise.txt --critical --reason 'next point release'` sets the
three cards the file names, one line per card and the total, or none (an id that is no card,
or names a read or work card, refuses the whole call with nothing written);
each change is on the card's timeline (`log --card`) with the actor and the reason. Every
deal places the cards above reader first, then the reads, then normal and low work in the
room the reads leave; `where` prints the levels beside the critical list and the backup
state (`backup: reads (review ... > working ...)`) while there is one
(docs/SPEC-SPRINT.md section 1, "Priority").

### The seat's store login

`nova-sprint seat login --store <secrets dir> --as <seat> --key <keyfile> --secret <NAME> --user <redis user> --redis <addr>` records the store login in `~/.config/nova-sprint/login.json` (or under `$XDG_CONFIG_HOME`), mode 0600: the address, the user and where the password is in nova-secrets, never the password, and only once the secret resolves. After it, `nova-sprint <verb>` typed bare reaches that store as that user, the password read in the verb's own process through nova-secrets' checks, with no `nova-secrets exec` wrapper; `--redis`, `NOVA_SPRINT_REDIS`/`NOVA_REDIS_ADDR` and `NOVA_SPRINT_REDIS_USER` still win. `seat login --check` prints `SEAT LOGIN file=… redis=… user=… … resolves=yes|no` (exit 1 on no), the password never shown; `seat logout` removes the record. A recorded secret that does not resolve is refused naming the file and the remedy, never dialed without a password. `nova-sprint run --keys <NAME,...>` records the decision key and each provider key in `keys.json` beside that login (names only, never a value) and the run process reads each one from the seat; a name that cannot be read refuses at start, naming the name. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#the-seats-store-login) and [SPEC-SECRETS.md](https://github.com/mas-bandwidth/nova-tools/blob/main/docs/SPEC-SECRETS.md) ("A tool's store login").

No store secret is ever in argv or a long-running process's environment. Beside the seat login, `NOVA_SPRINT_REDIS_PASSWORD_FILE=<absolute path>` names a file holding the password for `NOVA_SPRINT_REDIS_USER` (refused, naming the path and `chmod 600`, unless a regular file of this user's with no group or other bits), and on macOS `NOVA_SPRINT_REDIS_PASSWORD_KEYCHAIN=<service>` reads that service's generic password for the user through `/usr/bin/security`; each is read once per process. At startup every password variable (the one `NOVA_SPRINT_REDIS_PASSWORD_ENV` names, `NOVA_REDIS_BENCH_PASSWORD`, `NOVA_REDIS_COORDINATOR_PASSWORD`, any `NOVA_*_PASSWORD`) is read in and unset, so no child inherits it; a long-running verb (`run`, `dashboard`, `watch`, `wait`, `play`, any `--watch`, `--wait` or `--every`) that removed one re-execs itself once so the kernel's copy of its environment holds none, the values handed across on the pipe `NOVA_SPRINT_SECRET_FD` numbers. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#no-store-secret-in-argv-or-a-running-processs-environment).

For a live sprint, the coordinator hooks in with nova-hook ([SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#the-hook)): `nova-hook hook --as <seat> --role coordinator` under a Monitor, each `HOOK CHALLENGE` answered with `nova-hook prove`. Until the seat's nova-hook record is proven as the coordinator (its bus push, which nova-hook holds, and its sprint push, which the sprint's server holds while it runs and reaches the bus), every non-exempt verb the seat's holder runs (the caller is `--actor` or `NOVA_SPRINT_ACTOR`; a worker verb's `--as` never takes the holder past it) fails with one line that starts `You must hook in first: ` and goes on to the breadcrumb: what is not live and the exact nova-hook commands. The server sends every judgment and note for the coordinator to the seat's name on the bus. Exempt: `run`, `tick` (not `tick --answer-rules`), `server switch`, `seat install`, `selftest land`, help and version. A record that cannot be read refuses (fail closed).

`nova-sprint seat install --server <host:port> --config-seat <name> --config-dsn <dsn> --config-password-env <NAME>` records the sprint's server in `seat.json` beside that login and writes the nova-config seat profile, the row `<name>\t<dsn>\t<NAME>` of `~/.config/nova-config/seats.tsv`. After it, `nova-sprint seat check` measures the recorded server when `NOVA_SPRINT_SERVER` is not set and prints `MACHINERY config OK seat=<name> …` (or DOWN with the remedy), and `nova-config <verb> --seat <name>` (or `NOVA_SEAT`) reaches the config store with no `nova-secrets exec`: the password is read in process from the store login's nova-secrets seat under `<NAME>` when that variable is not set and `NOVA_PG_PASSWORD_ENV` is not given (`NOVA_PG_PASSWORD_ENV` given still wins). The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#handing-over-the-seat) and [SPEC-CONFIG.md](https://github.com/mas-bandwidth/nova-tools/blob/main/docs/SPEC-CONFIG.md#connecting).

A fleet member back from down adopts the latest before it is dealt when the
coordinator's machine sets `NOVA_SPRINT_ADOPT_FLAGS` to `nova-update release adopt`'s
flags less `--machines`, `--version` and `--dry-run` (blank-separated: `--ssh`, `--from`,
`--bin`, `--dest`, the stage's digest, `--no-certify` or the certification's three) and
`nova-sprint` was built with a release stamp: the tick holds a member whose beat
returns after it was down (the fleet table's status `adopting`, reason `adopting <release>: back from down`), adopts the release this `nova-sprint` runs onto that machine alone, reads its
installed version back, and brings it up at its width with one note `<m> is back: <old> -> <new>`; a failed adoption keeps it held with the failure as its reason and
one judgment (`fleet up <m>` brings it up as it is). Unset, a member back is up at
once (docs/SPEC-SPRINT.md section 5, "Back from down: adopt the latest").

### Every unit a sprint needs, installed by a verb

A running sprint needs eight units on its coordinator's machine: the store and the bus (`nova-redis install store|bus`), the server, the machine's member, the friend sync loop and the live table (`nova-sprint install server|member|friend-sync|table`; the seat's push loop is retired, v1.3.6), and the disk guard (`nova-swarm install disk-guard`) and the mirrors' refresh (`nova-swarm install mirror-refresh`, owed: the `nova-swarm mirror` verb is written, its unit is not). Each verb writes its unit (a launchd agent on macOS, a systemd user unit on Linux, kept alive and started again at login) into `--dir` (default `~/Library/LaunchAgents` or `~/.config/systemd/user`) and loads it; `--dry-run` prints it and writes nothing, and `uninstall <kind>` unloads and removes it. A unit runs the verb itself by the tool's absolute path, never under `nova-secrets exec`, a shell or a wrapper, and carries no secret: the server, the push loop and the table open the store with the seat login recorded by `seat login` (above), read in their own process, and install refuses a store this shell reaches as a user with no login recorded for it. Not yet in process: the server's decision loop reads its API key and the member the providers' keys `--pass` names from the service's environment, which the unit does not set. `install seat-push` and `install friend-sync` are `seat install` and `friend sync install`. `install table` runs `where --watch --every <d>` with its lines to `--out`. `nova-sprint units --check` reads the unit files and prints `UNIT <kind> installed|missing|different unit=<path>` for each of the nine, a different one with `why=` (the file runs a wrapper, another verb, or is no unit) and each not installed with `; run: <the verb that installs it>` (or `; owed: <the verb> (<what it waits on>)`), then `UNITS CHECK OK|DIFFERENT installed=<n> missing=<n> different=<n>`; exit 1 when one is not installed. `--json` prints the same as one object.

### The sprint backup

`nova-sprint backup --file <path>` writes the store to a new file (owner-only; an existing file is refused, never overwritten), reads it back against its SHA-256, restores it into a twin and compares it with the store (on a twin store its sprint state part for part; on a Redis the RDB's header and checksum alone), and scans it for secret-shaped text. A file that fails any step is removed. On success it prints `BACKUP OK file=<path> sha256=<hex> bytes=<n> keys=<n> cards=<n> restore=<semantic|integrity> compared=<state+document+counts|header+checksum> secrets=none`, and a backup checked at the integrity level ends it `; integrity only, not a semantic restore: the sprint's state was not loaded and compared`; a refusal names the failed step and, for a secret, the lines (never the value). It runs on the store's host for a Redis, and on any twin (`--redis mem:<file>`) with no server. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#sprint-backup-verb).

`nova-sprint backup --out <dir>` is the backup for the work record: the sprint's keys of its epoch, the keys every epoch shares and the records an older epoch left as a RESTORE text dump (`sprint-epoch<n>.restore.txt`, one `RESTORE <key> <ttl ms> <payload>` line a key), compressed with `xz -9` and split into parts under 100 MB (`--part-bytes`, default 95000000). It records the sums of the text and of the xz (and of each part) in `SHA256SUMS`, puts the parts together again, checks both sums, restores the dump into a throwaway store (a `redis-server` on a unix socket holding this build's function library, or a twin) and compares the keys and the cards of each column with the store's, and the sprint state part for part on both a twin and Redis; it then scans the dump and the restored values for every sealed nova-secrets value of the seat (`--secrets-store`, `--secrets-as`, `--secrets-key`, `--sops`, default the seat login's) in a child of `nova-secrets exec`, which prints counts only; and it writes `README.md`, naming the parts in order, the sums and the load command. `--out` must not exist or be empty; it is written only when every step passed. It prints one `BACKUP FILE <name> bytes=<n> sha256=<hex>` line per file and `BACKUP OK out=<dir> epoch=<n> keys=<n> cards=<n> (<col>=<n> ...) restored=<twin> keys=<n> cards=<n> (...) parts=<n> text_sha256=<hex> xz_sha256=<hex> secrets=<n> matched=0 restore=<semantic|integrity> compared=<state+counts|counts>`, and a count-only fallback, whose source or loader cannot expose logical state, ends it `; integrity only, not a semantic restore: the sprint's state was not loaded and compared`. A match fails it with exit 1 and `secrets=<n> matched=<k>`, never a value or where it was. It needs `xz` and `split` on PATH (`--xz`, `--split`), and `redis-server` (`--redis-server`) for a Redis store. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#sprint-backup-out).

### A backup as a demo

`nova-sprint demo load sprint-store-2026-10-04-2336.redis.txt.xz.part-*` loads a store backup (the RESTORE text dump, xz, split into parts) into a throwaway Redis on a free 127.0.0.1 port, with the function library of this nova-sprint binary (never the installed nova-redis's), and prints `where` against it and the line `DEMO UP --addr 127.0.0.1:<port>`: point any read verb at the demo with `--redis 127.0.0.1:<port>`. The parts are joined in name order and checked against the sum beside them when there is one: `<file>.sha256` (the hand backup's), else the line of `SHA256SUMS` naming the joined file (`backup --out`'s), or `--sha256 <hex>`. It takes no `--redis`: the only store it opens is the one it starts. The server's directory and the state file (`demo.json`: address, port, pid, directory) are under `--dir`, by default the user cache directory's `nova-sprint/demo`; a second load while one is up is refused. `nova-sprint demo stop` stops that Redis by the pid it recorded, only when the Redis at the recorded address is that pid, and removes the recorded directory and nothing else. The live store is never opened. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#demo-load-verb).

### The system's scratch

`nova-sprint gc [--machine <m>] [--dry-run] [--max-age <d>] [--ai-root <dir>]` reclaims, on the machine it runs on (or, with `--machine`, on that machine through the fleet runner, which runs the same verb there), exactly the scratch the system made and no longer needs: the job directories of finished or absent lanes, reader checkouts of recorded findings, lander worktrees, bench directories under `~/nova-bench` older than `--max-age` (default `2d`; days or a Go duration), and the go caches trimmed to their cap. It refuses a path under no known scratch root (the AI root: `--ai-root`, else `NOVA_AI_ROOT`, else `~/ai`, else the one the home's `<name>-working` links name, `<root>/<name>/working` or `<root>/buds/<name>/working`, as on a machine that exports none; the bench root; land's clone root; a plain `<home>/<name>-working` directory, as a bench keeps one), and keeps a clone with uncommitted work, a stash or unpushed commits. It prints one line per class, `GC jobs|reads|landers|bench|cache count=<n> bytes=<b> kept=<n> refused=<n> failed=<n>`, and `GC OK freed=<bytes> volume=<use%>`; `--dry-run` says `GC WOULD-REMOVE` and removes nothing. `nova-sprint run` runs it on every machine once an hour and as soon as a machine's volume is at 80%. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md) section 1, "gc".

### A card re-cut as its twin

A card re-cut under a new id is its old card's twin: `add --stream s1 lint-pkg-cairn-tb
--brief-file lint-pkg-cairn-tb.md --replaces lint-pkg-cairn-t` admits the twin, makes
every waiting card that needed the old id need the twin instead (`card <dependent>` shows
the new need), drops the old card `replaced by lint-pkg-cairn-tb`, and raises no "blocked
on something dropped" judgment, in one step. Where the drop and the add were made apart,
`relink lint-pkg-cairn-t lint-pkg-cairn-tb` re-points the edges and answers the blocked
judgments of that pair. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md) section 2, "A
card replaced by its twin".

### Where a sentinel is placed, and what it blocks

`add --sentinel <id>` with `--needs` on cards of its own stream places the
sentinel directly after the last of them in the line (its producer), so it
waits on that card and what is before it only; with no need on its own
stream it goes to the end of the line, as before. `--before`, `--after` and
`--score` still place it explicitly. `nova-sprint sentinels` prints, per
sentinel, `waits-behind=` (the unlanded cards ahead of it in its line) and
`blocks=` (the cards behind it and the cards of other streams that need
it). A cycle refusal marks each edge of its loop: `a -needs-> b` for a
named need, `a -line-> b` for an edge of its place in line.

### Deferring cards and relinking needs

`defer` takes waiting cards off the table and appends them to the roadmap. If an unlanded card still needs the card being deferred, `defer` is refused, naming the dependents: `<id> is needed by <deps>; defer them too, or drop <id> with --cascade, --relink <card> or --waive`. No defer strands a dependent.

When dropping a needed card with `--relink <new-id>`, `drop` verifies that `<new-id>` is placed and not unresolved (held, dropped, absent, or an unknown stream). A relink to a held or absent card is refused: relinking to a held card moves the strand, it does not resolve it.

### A sentinel whose cards were deferred

When the cards a sentinel waits on move to a later release, `sentinel set v1 --needs
s1-3` re-points it to the cards that remain, in one step: it keeps its id, its place in
its stream and its log, and the log gains one line with the needs before and after. A
need that is no card on the table is refused, naming every one, and nothing changes;
`--needs ""` is refused, since a sentinel with nothing to wait on is released
(`release v1 --reason '<why>'`), not emptied. The contract is
[SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md) section 16.

### A merging card whose base is gone

When a merging card's `BASE:` branch is deleted from origin, the lander refuses it once,
`LAND REFUSED ... reason=card <id> names BASE <b>, which is not on origin`, raises one
judgment for it, and lands the rest of its stream; it does not try the card again while
that judgment is open. `card base <id> <branch>` re-points it: the branch is asked of the
card's origin first and one not there is refused, nothing changed; else the brief's
`BASE:` line names the new branch, the judgment is answered, the card's work and reads are
kept, the log gains one line, `<id> BASE <old> -> <new>`, and the next land pass tries the
card once. `ack` of the judgment has the next pass try the card once on its old base. The
contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md) section 7, a dead base.

### The server's lander names a standing refusal

`run --listen --land` prints one line every two seconds. While a landing is refused, every
line is `LAND REFUSED queued=<n> since=<age> reason=<reason> run: <remedy>`, never `LAND
IDLE`, until a pass gets past it or the refusal changes. Only a refusal before any batch
stands (a pass of landed and refused batches is none; each refused batch has its own
judgment), keyed on its reason with numbers folded, so timings never make it new. The first
refused cycle raises
one judgment, `the lander refuses every landing`, naming the reason and the remedy
(decisions `check`, `ack`), and the tick's `landing stalled` judgment names it too. A pass
that gets past it closes it; another reason closes it and raises its own. A judgment the
store does not take is said once as `LAND JUDGMENT NOT WRITTEN: <why>` and tried again. A failure
before any batch, or a merge queue the loop cannot read, is worded as a failure: `LAND
FAILED queued=<n> since=<age> reason=<reason> run: <remedy>` and the judgment `the lander
fails every landing`; the queue's first good read clears it. The cause is keyed on the class land names
at the source, `code=<class>` (and `stream=<s>`), never on its words; `ack` of its judgment
keeps it acknowledged while the cause stands, and a pass that gets past the cause closes
both. A restarted server asks once more about a cause that was acknowledged before the
restart (it cannot tell that acknowledgement from one left by an earlier episode whose close
was lost with the old server), and an `ack` of that judgment sticks. `LAND IDLE` with cards queued says
why: `why=running` (a landing in flight), `why=no-coordinator`, or `why=nothing-landable`.
The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md) section 7, the server's lander.

### Role views: what a model reads instead of the dashboard

The owner, 2026-10-04: "i'd rather you hit this vs. hitting my dashboard which is for human
eyes". `nova-sprint view coordinator` is everything that needs the seat now, ranked by the
cards behind each item: open judgments, notes addressed to the coordinator, alarms (the
system stopped, the fleet idle, nothing ready, a review or merge backlog, a stream stopped),
sentinels reached, and friends and machines that need a look, each with `next`, the exact
command that acts on it. `nova-sprint view worker --as <member|friend>` is one worker's cards
in order (brief, BASE, PATHS, deadline, attempt), its next step and its results not landed.
Both are reads, `--json` (schema 1), compact for the tokens a model pays: only what needs
action (`--all` adds every machine's and friend's row), a summary line first, and a `cursor`
that `--since <cursor>` takes to leave out what the last read showed unchanged:

```sh
nova-sprint view coordinator                 # the summary and up to 19 items, then cursor=
nova-sprint view coordinator --json --since <the cursor the last read printed>
nova-sprint view worker --as m1 --json
curl -s --compressed http://<tailnet address>:<port>/api/view/coordinator
curl -s --compressed 'http://<tailnet address>:<port>/api/view/worker?as=<name>'
```

The sprint's server (`run --listen`) serves them read-only at `/api/view/coordinator` and
`/api/view/worker?as=<name>`. `nova-sprint view cards --col review --by tier --json` counts the primaries by column, tier, stream or holder (also `/api/view/cards?col=review&by=tier`). `nova-sprint friend cards <friend> --json` is every card held on
a friend's row (working, then ready) with its packet and its `BRIEF.md` as friend sync writes
it; her nova-friend daemon reads it every loop to write her inbox, and the server serves it to
her as a worker's verb and at `GET /api/friend/<friend>/cards`. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md), section 11,
"Role views".

### A worker's own view: the dashboard's pull routes

| command | what it does |
| --- | --- |
| `dashboard [--listen <address:port>[,...] \| none] [--pull <address:port>[,...] \| none] [--logo <file>] [--every <duration>]` | Serves the page and its cached copy of `where --json`; one poller runs the read, back to back with `--every` as its floor, and every page is answered from the cache |

`dashboard` serves the page on `--listen` (default `127.0.0.1:7390`) and, on listeners of
their own, the pull routes on `--pull` (default `127.0.0.1:7395`; `none` for either serves
nothing there): `curl -s http://<tailnet address>:7395/friend/<name>` is one friend's view
as plain text, a line an item (the sprint line, her friends-table row, a line per card
dealt to her: id, stream, state, how long, the time to its deadline, the branch; then the
open judgments on them); `/machine/<name>` is a fleet row's; `/team` is every friend and
the cards she holds; `/api/team`, `/api/friend/<name>`,
`/api/machine/<name>` and `/api/sprint` are JSON; `/events`, `/events/friend/<name>` and
`/events/machine/<name>` push each new copy as server-sent events. All of it is read-only,
no-store, carries the copy's time in `Sprint-At`, and comes from one copy of `where --json
--cards` read at most once a second however many pull. Responses on both `--listen` and
`--pull` listeners gzip-compress responses when requested with `Accept-Encoding: gzip` (`/`,
`/app.js`, `/api/sprint`, and pull routes). The server ticks the page at 1 s from its cache
every second independently of background reads; reads exceeding 1 s log a warning.
`where --json --dashboard` prints the cheap dashboard view with `stream_costs` folded into
`tables.work`, reader rows folded into `tables.fleet` (`reads_working` and `working`), and
sentinel-free summary, dropping archived streams without loading card rows; `--dashboard`
requires `--json`. An unknown name is a 404 of one
line; `where --json` also carries `landedSeries` (cards landed per 10-minute bucket over 24 hours, split between friends and fleet). The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md), the dashboard.

Each stream's clock in `where --json`'s `streams` carries its stored `State` and, beside it, `held_cards` when the stream has held cards: its waiting primaries no tick moves on its own (behind a sentinel not released, admitted held with `add --held`, or waiting on one of those through a need), counted by the tick into the where record. A card hold is not a stream hold: the state still reads what the store holds (`waiting`, `working`, `held`, `landed`), and `held_cards` never replaces it.

### A provider out of funds

The owner, 2026-10-03: "provider out of funds should never be a mystery failure." `run` reads
each provider's balance every 10 minutes through the seat's key in its environment
(`OPENROUTER_API_KEY` for openrouter; opencode publishes no balance and reads `unknown`) and
prints a `BALANCE` line. A provider out of credit by a take it refused is rested until a
payment is seen (a balance read higher than the read before it, or than the balance at the
refusal) or `funded`: a balance over zero that is not higher never ends it. One out of credit by
a balance at zero is rested until a balance over zero; one low on funds, a balance not over an
hour of its spend, until the balance is over it. One judgment of the provider says which (`a
payment is the owner's`). `where --json` carries the `providers` table (balance, spend an hour,
state) and `routes` each route's `balance=`. When every provider is out of credit, the tick
stops the system (`machine: STOPPED (every provider is out of credit)`) and `start` is refused
until one is paid; a provider low on funds never stops it. `funded <provider> --reason <text>`
says one was paid. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md), "A provider out of funds".

### Answered by rule

The run loop's tick answers the mechanical judgments itself, by rule, and records each as
`answered by rule <name>` on the log and on the card (`rule_answer`): work came back
failed is redealt on the next route of its tier, and the second failure on a tier goes a
tier up (flash, pro, heavy, then a friend's card); a card at its bound goes a tier up; a
work card past its deadline is waited 30 minutes once when it made progress in the last 10,
else returned and dealt again; a stream stopped on a conflict in a file no ledger owns has
the card returned, the stream resumed and the card redone on the current tip; the same
finding twice marks the card a brief defect and leaves it to you; and land gates a red base
again after 2 and 5 minutes before the third failure stops the stream with the error. A
reader's finding stays yours. `nova-sprint rules` prints what the rules would answer now and
Xoff, and nova-config's sprint row turns single ones off: `nova-config sprint set
--answer_rules_off late,conflict`, then `nova-config apply`. The contract is
[SPEC-SPRINT.md section 8](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#answered-by-rule).

### The machine's commit identity

Every commit the sprint machine makes (land, merge, rework-at-tip, the promotion's cut,
ledger and roadmap writes) carries an explicit author and committer, the sprint row's
`git_identity`: `nova-config sprint set --git_identity 'Name <email>'` (default
`nova-sprint <nova-sprint@example.invalid>`), then `nova-config apply`. The landing and
the promotion refuse to commit, naming the setting, when none is configured; `nova-sprint doctor` reports the identity in use and fails when its email equals the host's global
`user.email`. A worker's clone gets the identity in its local git config at clone time
(`pkg/swarm`, `StageJob`/`StageCloneIdentity` and their `As` variants): the live staging
entry points carry the machine's identity (the sprint row's `git_identity`, default
`nova-sprint <nova-sprint@example.invalid>`), never the pool's owner, so a rework-at-tip
commit in the clone carries it too. The contract is [SPEC-SPRINT.md section 7](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#7-merging).

### Promoting the sprint branch into dev

`nova-sprint promote --once --branch <sprint branch> --repo-dir <clone>` carries one
promotion from the cut to the recorded merge with no hand steps. It fetches origin and cuts
`promo/<date>-<n>` from `origin/<sprint branch>`, never the clone's local ref, and refuses a
cut that is not ahead of `origin/dev`. It merges `origin/dev` into the cut without a checkout.
If that merge conflicts, it raises one judgment naming the files, `JUDGMENT promote conflict ... files=<a,b>`, and stops; nothing is cut or pushed, and the tool resolves nothing. A clean
cut is gated (`--check`), pushed, and its pull request opened. The verb waits on the pull
request's checks, queues it once they pass, and watches the queue. When the queue merges it,
the verb records `promoted --sha <merge>` in the store. A failed check or merge-group run
raises one judgment naming the check, `JUDGMENT merge-group failed ... check=<name>`, with
the failing log's tail. A pull request closed without a merge clears the promotion in
flight with one judgment naming it, `JUDGMENT closed-pr ... pr=<n>`, and the next pass cuts
afresh. The verb claims the promotion cleared only after every in-flight key is gone: a
cleanup that fails is a refusal naming the keys that remain. Every step prints a line as it goes, and every wait names what it waits on
(`PROMOTE WAIT ... checks pending: <names>`). While the promotion is pending the wait blocks on the store's `ev:github` stream
for a row naming the repository and the pull request (the checks arrive as events, written by the ci receipts); such a row ends the
wait at once and the verb asks gh once, and `--poll` (default 1m) stays the backstop for a lost row.
Without `--once` the verb repeats every `--every`. `--dry-run` prints the cut it would make,
or the promotion in flight, and writes, enqueues and records nothing. The contract is
[SPEC-SPRINT.md section 11](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md), promote.

### The stream branch

Every stream lands on its own branch `sprint/<stream>`; a card cut on dev or main is re-cut
onto it at `add`, never landed straight on dev or main. The landing flow is one sentence:
"The landing flow: card heads land in a batch on their stream's branch sprint/<stream>,
oldest first, one writer per stream; stream branches merge one at a time into dev (main where
a repository has no dev); a card never lands on dev or main." A stream's branch and trunk are
fields of its control card (`stream_branch`, `trunk`): the first `add` of a stream writes both
in the same step as its first card, the branch `sprint/<stream>` and the trunk the protected
branch the card's `BASE:` names (dev, or main; dev when it names none); a card naming a
non-protected branch keeps that branch as the stream's. `add` re-cuts a card cut on dev or
main onto the stream's branch, saying so on one `NOTE` line per add naming the branch, the
trunk and quoting the landing flow. The lander creates an absent stream branch on origin at
the trunk's tip before it lands any card of the stream, one `NOTE` line `LAND BRANCH stream=<s> repo=<r> branch=sprint/<s> at=<sha>`; a card of a stream whose branch origin lacks is held, never
landed on the trunk. No setting turns this on or off: a cleared sprint (an epoch bump) builds
the same on its first add. The promotion stream (`stream set --promotion` / `--land-protected`)
is the exception: its cards land on the protected branch the mark names. The contract is
[SPEC-SPRINT.md section 7](SPEC-SPRINT.md#7-merging). On a RUNNING machine whose last tick failed, an `add` whose work-table changes wait for the next tick's drain adds one `NOTE` line (`queued: ...`) beside its OK, naming how many ticks failed and the error.

### The fleet is idle

When the fleet works under half its width for 5 minutes while cards wait, the run loop's
tick pushes you one note, `the fleet is idle`: `fleet 4/68: 311 behind 21 drop-blocked
judgments (oldest 1h50m); 89 behind md-secrets (a card reached its bound, 40m)`, every
waiting card traced to the root of its chain and the roots named by the cards behind
them; once an episode, and `the fleet is working again` when it recovers. `run
--idle-alarm=false` turns it off. The contract is [SPEC-SPRINT.md section 14](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#the-fleet-is-idle).

### Answering the routine judgments

`nova-sprint answer` answers the routine judgments (a reader found it
broken, work came back failed, blocked on something dropped, stalled, a conflict,
past its deadline, cannot ask, ready to accept for a held primary, a card at its bound) by
nova-decide's judgment decision, card by card: it applies the verb chosen when
its probability is at or above `decide_judgment_bar` (nova-config's sprint row,
empty by default: with no bar it applies nothing, records every decision and lists
what a bar would apply; `--bar` gives one for a run; 0.8 is a starting point measured on 100 of the coordinator's own judgments,
not an independent calibration), by the line the inbox prints for that card,
and lists the rest for you: every drop, everything under the bar, and a provider
refusal for want of payment, which it never asks about. It prints one table, a
row a card, and records every decision (`--record`, default
`~/nova-sprint/decide/judgment.jsonl`, its directory made 0700) with its outcome once the card lands, is dropped
or comes back. Each verb it applies carries the decision's op id (`--op
decide.<decision id>`), recorded as `applying` before the verb runs and `applied`
or `refused` after, so a pass stopped between the two is finished by the next
through the same op and nothing is applied twice. One ask may take `--timeout`
(60s by default); an ask past it, or one that fails, is that card's `failed` row,
nothing is applied for it, and the pass exits 1. `--dry-run` applies and records
nothing; `--every 60s` runs it as the seat's loop until the system is STOPPED. Jev's key comes from `JEV_API_KEY`:
`nova-secrets exec --only JEV_API_KEY -- nova-sprint answer`. The
contract is [SPEC-SPRINT.md section 8](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#answered-by-nova-decide)
and [nova-decide package documentation](https://pkg.go.dev/github.com/mas-bandwidth/nova-tools/pkg/decide).

`add` under `JEV_API_KEY` (`nova-secrets exec --only JEV_API_KEY -- nova-sprint add
...`) asks nova-decide's brief decision of every card it names with a brief after its
own checks and before it writes, one deadline for the batch: one `BRIEF card=<id>
op=<card>@brief-<hex> p_converges= minutes= failed= uncalibrated=true recorded=` line
per card, recorded in `~/nova-sprint/decide/brief.jsonl` (or `--decide-record <file>`),
the op stored on the card for land and drop to attach its end. The decision is
uncalibrated: nova-config's `sprint` row `decide_brief_bar` stays empty, which reports
only, until the brief record's own outcomes support a bar
([nova-decide package documentation](https://pkg.go.dev/github.com/mas-bandwidth/nova-tools/pkg/decide)).

`add` holds every card brief (one with a `PATHS:` line) to the brief checks at its base
before it writes: the repository `REPO:` names, in the lander's clone, at the tip of `BASE:`,
fetched once a call, read with git and no go command. The tokens are `paths-at-base`,
`donewhen-test-name`, `paths-cover-named`, `paths-cover-test`, `paths-cover-testdata`,
`paths-cover-ledgers`, `paths-cover-docs`, `base-is-live`, `tier-set`, `new-model-is-heavy-or-above` and
`who-serves-tier`; each finding is a `LINT DRIFT card=<id> check=<token> line=<n>: <excerpt>
remedy=<remedy>` line and each corrected header line a `LINT FIX card=<id> <line>` line (the
 `PATHS:`, `NEW:` or `SHARED:` line, or line 1, with every addition applied), then one refusal,
 exit 2, nothing written. Each refusal at the base is recorded in the server's refused-verb
 record under the verb, the card id and the rule tokens it drew in Why, so a later review
 counts refusals per rule from the record; an accepted add records nothing. A `TEST:` line is accepted in its Go form
(`[-tags <tags>] <package> <TestName>`) or its ctest form (`ctest <TestName>`, for a
C++/CMake repository), and a ctest test is searched in `CMakeLists.txt` and `*.cmake`
rather than in `*.go`; preflight's TEST check does the same, and the brief check accepts a
ctest `TEST:` with no package, its test file held by the `PATHS:` checks instead of a
directory ([SPEC-CARD-CONTRACT.md](SPEC-CARD-CONTRACT.md), the TEST line). A base that cannot be
read refuses with `MISSING: <what>`; a brief
naming no `REPO:` or no `BASE:` is held only to the checks that need no tree
([SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md) section 11, the brief checks).

The `brief check`'s `tla-records` rule reads the card's base tree too, and a repository
whose base holds no `tla/CASES.tsv` holds no models and is not held to it, the same file
the lander's run-record check reads. A brief on a placed card with a recorded tier
(`FieldTier`) that its line 1 contradicts gets a `tier` finding naming both tiers, and
the remedy that pins the line 1 tier (`nova-sprint brief c --tier <t> --reason <why>`);
a file naming the recorded tier, a card with no recorded tier, an id that is no card and
a sentinel report nothing.

A `brief check` file whose id is a card on the table is also read against the card's soft
dependencies: a `LAND-AFTER:` line that would change what the store holds is one `land-after`
finding naming both lists and the verb that writes each side, `nova-sprint brief <id> --brief-file <path>` the file's and `nova-sprint needs set <id> --land-after <ids>` the
store's. An equal list, an id that is no card, and a sentinel report nothing, and the check
still writes nothing.

`add --partial`, with `--brief-dir` or a repeated `--brief-file`, runs every check over every
brief and admits what passes instead of refusing the whole call. Every brief the checks find a
fault in is held back, and so is every card that needs a held card through its `Needs:` or
`DEPENDS-ON:` line or `--needs`, transitively. The add step's own plan is then run over the
rest, and a card its refusal list names is held back the same way, with the cards that need it,
the write planned again without them; the rounds are bounded by the number of cards, and
nothing is written for a held card. A brief that is a probable duplicate of an open card is
held back with the same finding add gives it, and so is every card that needs it;
`--not-a-duplicate-of <card>=<reason>` admits it and records the reason on its admission
note, as it does for add. It prints one `HELD <id>: <its first finding or refusal>`
or `HELD <id>: needs <other>, held back` line per held card, then the add's own closing line for
what it admitted, and exits 0 when every card was admitted, 1 when some were held and the rest
written, and 2 when none could be admitted, or for a usage refusal: `--partial` is refused with
ids, `--count`, `--sentinel` or `--land-protected`, since a marked first add is one step.

### install-canary-shadow-tick-r.w1: the shadow tick before a server swap

`nova-sprint tick --shadow` plans one tick on the store and applies nothing: the store is
opened read-only, every write a refusal, and each part's plan is printed (`SHADOW PLAN
<table>/<part> size= due=`, then `SHADOW TICK OK epoch= state= parts= size= took= wrote=nothing`;
`--json` prints the plan as one line). `nova-sprint server switch <binary>` runs `<binary> tick
--shadow --json` against the store first and refuses the swap, exit 1 with nothing changed and
the old server running, when the shadow exits non-zero, panics, misses `--tick-deadline`
(default 10s) or prints no plan; on a pass it switches and keeps the shadow's plan size and time
at `<target>.shadow.json`, beside the switch record. The contract is
[SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md) section 14, "install-canary-shadow-tick-r.w1".

### Adopting a build: nova-sprint live and nova-sprint adopt

`nova-sprint live [--bin-dir <dir>] [--agents-dir <dir>] [--launchctl <path>] [--dashboard <link>]... [--json]`
prints the manifest of this host and changes nothing: `LIVE SERVER binary= inode= version=
revision=`, `LIVE LIBRARY state= loaded= want= match=` (the installed nova-redis's `fn check`
against `--redis`, logged in as `NOVA_SPRINT_REDIS_USER`), one `LIVE DASHBOARD link= target=
current=` per `--dashboard`, and one line per `com.nova.*` launchd agent of `--agents-dir` (else
`NOVA_LAUNCH_AGENTS`, else `~/Library/LaunchAgents`): `LIVE SERVER` for `nova-sprint run`,
`LIVE FRIEND` for `nova-friend run` with its last beat and its lanes with a card in hand, else
`LIVE AGENT`, each with its pid and, for a nova tool, `loaded= stale= fresh= installed=
args_took= binary=` and the `why=` of a stale one; `--json` also lists the host's nova
processes, `holds` on this bin directory's nova-sprint and nova-swarm members. Exit 0 whatever it finds, 1 when the
installed nova-sprint or the agents directory cannot be read, 2 usage.

`nova-sprint adopt <version|path> --source <checkout> --sprint-release <dir> --inventory <file> --reason <text>
[--limit <host>] [--receipts <dir>] [--dry-run]` runs `<checkout>/fleet/tools.yml` for the
seat, as a window: the new build's checks first (its shadow tick on the store, its `nova-friend
install --dry-run` against every friend daemon's flags); then, when the build replaces a tool or
the library, the old server, member and every nova agent but the friends are booted out and ps
is waited on to show no old nova-sprint or member; the configuration store is migrated, the
library loaded and the tools installed; every agent the window stopped and every stale one is
bootstrapped and proved; the dashboard links point at the installed nova-sprint; and each stale
friend daemon is reinstalled after its lanes put their cards down. A refusal once the window
opened puts the tools of before back, loads and reads back their library and starts the stopped
agents on them before it is said. It prints
one `ADOPT step=<store|server|dashboard|friends> host= before= after=` line per step and `ADOPT
ADOPTED version= hosts= steps=`; with `--dry-run` (the play's `--check`) `ADOPT WOULD-ADOPT`, and
a machine with neither the candidate staged nor built prints `ADOPT step=seat host=<h>
after=<version> WOULD-ADOPT` in place of the steps. Exit 0 adopted (or, with `--dry-run`, said
what it would change); 1 refused, `ADOPT REFUSED step=<step>` said verbatim, when the play stops
or ends without a step's line (the steps before it are done; a refusal once the seat play's
window opened is said with what the rollback did and names those steps rolled back; the same
command again finishes it), or when `--source` holds no
`fleet/tools.yml`; 2 usage. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md) section 14,
"Adopting a build".

### Exit codes

| exit | meaning |
|---|---|
| 0 | done |
| 1 | failed or incomplete (including refused; `start` while every provider is out of credit) |
| 2 | usage, or a store that did not answer (under `fleet sync --check`, 2 is drift alone, with DRIFT lines; a store that did not answer is 3) |
| 3 | unreadable config (`fleet sync`, `friend sync`, `friend clean`), missing friend rows (`friend sync`, `friend clean`), or a replaced binary (`run`, `dashboard`) |

The exit codes do not separate a definite refusal from an unknown outcome: exit
2 is both `refuse()` (usage, `card@gen`, epoch, a server refusal) and
`changed=unknown` or no answer, and exit 1 is both a refused card and a
`CutError` that repair can still complete. The server's answer to a worker's
verb therefore also carries the typed `outcome` (`accepted | refused | unknown`)
and `changed` (`yes | no | unknown`) fields, set where the verb knows them
(`refuse()` and every pre-write refusal give `refused`; a `CutError`,
`ErrUnknown`, or a `ClearedError` with `Finished` gives `unknown`; a store error
that answered nothing gives `unknown` too; success gives `accepted`), and each
verb's `--json` output carries them too. A worker reads
these fields, not the printed words. The contract is
[SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md)
section 14, "The server".

### What it does not prove

A landed card records a completed flow: the worker reports done, two different
readers pass that head, the tick queues it for merging and tells the seat
("ready to merge", a notice; the coordinator's `accept` is for a primary the
tick holds), and the landing is reported. These are recorded judgments, not a
proof that the work is correct. A twin exercises that flow one command at a
time. It has no beats or ticks between commands, so it does not test fleet
timing, the `run` loop, `inbox --wait` or liveness. `finish` without `--head`,
then `merge`, records a landing without a push. The work table's cost column
is, per stream, the sum of every card's total cost in US dollars, landed, open
or dropped, its reads and decide calls included, a recut card's on its twin —
each record's actual cost where one was priced, else its predicted one, `-` when
none was, the unpriced runs counted beside it — so a total is a ledger of
recorded spend, not a proof of it.

### release-check-acceptance-r-b.w3: the acceptance sentinel's six checks

`nova-sprint release check` runs the acceptance sentinel's six checks beside
`no-stuck-friend`, source: the coordinator's answer over the bus, 2026-10-06
12:50 ET. Each prints one `RELEASE CHECK <name> ok|fail <evidence>`
line, and the release refuses on any fail: `cards-settled` (every card of the
stream landed or dropped with a reason), `base-gate-green` (the unit and
functional classes and `./internal/docs` and `./internal/ci` green on the base
at the stream's last landing), `two-ok-reads` (every landed card has the ok
reads its tier needs at its final head), `prose-true` (`nova-check links` and
`nocode` clean on the stream's specs and help), `landings-promoted` (the
landings are in dev or a promotion carries them) and `no-open-judgment` (no
open judgment names the stream). One check alone: `release check --check cards-settled`; one stream's facts: `release check --streams 's1*'`. With no
stream named there is no acceptance to check, so each passes and says so. The
contract is [SPEC-RELEASE.md](https://github.com/mas-bandwidth/nova-tools/blob/main/docs/SPEC-RELEASE.md) section 16, subsection
release-check-acceptance-r-b.w3.

### release-check-merge-queue-p90-b.w7: the merge queue's p90

`nova-sprint release check` also runs `merge-queue-p90`: over the last
`--window` (default 24 h) it takes the p90, by the nearest rank, of the time
each card spent in merging, read from the log's work-table moves into and out
of the `merging` column (a card still merging counts with its age now), and
fails above `--merge-p90` (default 30 m). The fail line prints the p90, the
number of cards and the oldest card still merging. One check alone:
`release check --check merge-queue-p90`; a shorter bar:
`release check --merge-p90 15m`; a week's window:
`release check --window 168h`. With no merge in the window it passes and says
n=0. The contract is [SPEC-RELEASE.md](https://github.com/mas-bandwidth/nova-tools/blob/main/docs/SPEC-RELEASE.md) section 16, subsection
release-check-merge-queue-p90-b.w7.

### First-time land rate goal

`nova-sprint where` shows a `first time` column on the work table (`<pct>% <first>/<counted>`, `-` when none counted) and a summary line under the work table (`first time: <overall> target 90%`, naming any streams under target), and `where --json` carries `first time` on each work row alongside top-level `first_time`, `first_time_target`, `first_time_window`, and `first_time_under_target`. A primary counts only when it has landed or been dropped; it counts as first-time only when it landed on attempt 1 with no rework, no brief replacement, and no carry.


## nova-card

nova-card is pre-alpha: not ready for production use.

```
nova-card generate --from ledger --ledger <name> --repo-dir <dir> --out <dir> [--tier flash|pro] [--prefix <p>] [--minutes <n>] [--max <n>] [--base <branch>] [--repo <owner/name>] [--name <n>...] [--dropped <id>...] [--dry-run]
nova-card generate --from findings --file <tsv> --out <dir> (--repo-dir <dir> | --repo <owner/name> --base <branch> --sha <40hex>) [--tier flash|pro] [--prefix <p>] [--minutes <n>] [--max <n>] [--name <n>...] [--dropped <id>...] [--dry-run]
nova-card generate --from help --tool <name> [--tool <name>...] --out <dir> [--bin-dir <dir>] (--repo-dir <dir> | --repo --base --sha) [--tier flash|pro] [--prefix <p>] [--minutes <n>] [--max <n>] [--name <n>...] [--dropped <id>...] [--dry-run]
nova-card lint --card <file> [--card <file>...] [--name <n>...] [--dropped <id>...] [--json]
nova-card template
nova-card version
nova-card help [<verb>]
```

A card a model writes by hand takes it half an hour and comes back with guessed
PATHS; one wrong PATHS line was rejected 262 times in one night. nova-card
writes the cards from the source the work comes from, with the PATHS computed,
the lint already green, and the waves already laid out, so the one thing left
to do is `nova-sprint add --stream <s> --brief-dir <dir>`.

The flow is three lines:

```sh
nova-card generate --from ledger --ledger serial-tests --repo-dir ./repo --out ./cards
nova-sprint add --stream debt --brief-dir ./cards
nova-sprint where
```

### First run

The included findings file is two files' worth of a reader's findings. No
checkout is needed when `--repo`, `--base` and `--sha` are given; with
`--repo-dir` the three are read off the checkout and every PATHS entry is
checked to exist in it:

```sh
nova-card generate --from findings --file ./cmd/nova-card/testdata/findings.tsv --repo example/repo --base dev --sha 0123456789abcdef0123456789abcdef01234567 --out ./cards
nova-card lint --card ./cards/finding-internal-bus-send.md
nova-card lint --card ./cards/finding-cmd-nova-bus-main.md
```

`./cards` then holds one `.md` per card, its name the card's id, and a
`manifest.tsv` (id, file, test, wave, deps). [TESTS.md](TESTS.md#nova-card)
carries the transcript; `cmd/nova-card/firstrun_test.go` runs it.

### Sources

`--from ledger --ledger <name>` reads one of internal/ci's ratchet ledgers from
the checkout: `serial-tests`, `slowwaits`, `sleeps-skips`, `fixed-waits`
(flash), `dead-code`, `namedpaths`, `transcripts`, `generality-fixtures` (pro).
One card per file the rows name; the card's PATHS are computed from its START
line, never typed: every directory a START file lives in, as its Go files and its
tests (`<dir>/*.go`, `<dir>/*_test.go`), and the docs the card names (a ledger
card: the row's file's package, its test's package and the ledger; a findings
card: the file's package and its test's package; a help card: `cmd/<tool>` and
`docs/CLI.md`); its TEST is the class test that holds the ledger;
its task is the ledger's template with the rows substituted, and it says to
write the draft early and commit before any probe. `--from findings --file
<tsv>` reads `file:line`, finding, remedy, test columns (a header row is
skipped); one card per file, the first finding's test as TEST, a card with no
test named given the one it must write. `--from help --tool <name>` runs
`<name> help` and writes one card per tool: the lines over 100 characters,
the undefined terms and the examples that do not run as printed.

### Waves and dependencies

Cards of one ordinary ledger delete adjacent lines of one file and would
conflict at land, so odd cards are wave 1 and even cards wave 2, each wave 2
card depending on its wave 1 neighbours (`DEPENDS-ON`). A generated ledger
(SPEC-SPRINT.md section 7, the generality family) is regenerated by `land`, so
its cards are one wave with no dependency. Wave 1 cards of one ledger share its
path and neither needs the other, so `add` refuses the pair unless a needs edge
chains them or the path is a lander-owned ledger the lander merges
(`--plan <dir>` prints the overlap table before a stream is admitted).

### What it refuses

Every brief is held to the lint `nova-sprint add` runs (the model lines, the
child rules under the default rule set, a tree card's steps, the stranded-header-key
and dashboard-ask rules the add runs after them), and to the template's unfilled
`<...>` lines, which the add does not read, before anything is written (a sprint
initialised with `--rules` holds a brief to that file at the add), and to the
card checks the add runs too: a tier on line 1, a TEST whose package PATHS names, no name `--name` gives
outside double-quoted words, no card `--dropped` gives; one red brief prints its
`LINT DRIFT card=<id> check=<check> line=<n>: <excerpt>` line and nothing is
written, exit 1. A PATHS entry that names nothing in `--repo-dir` is the same
refusal. An `--out` that already holds a brief is refused, exit 2. `--dry-run`
plans and lints, prints the manifest and the `CARDS OK` line with
`dry-run=yes`, and writes nothing.

### Exit codes

0 done; 1 a brief is red and nothing was written; 2 could not run.


## nova-work

nova-work is pre-alpha: not ready for production use.

Every issue of every repository of a GitHub organization in one tree file, with
each issue's full contents, and a check that the file holds exactly what GitHub
holds. The design is [SPEC-WORK-V1.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-WORK-V1.md); this section is how to
use it. It reads GitHub only (no GitLab, no Gitea), never writes to GitHub (the
seam refuses any GraphQL document that is not a query), and captures the fields
SPEC-WORK-V1 section 1.3 lists, not reactions or other timeline events.

**Try it with no login.** `nova-work example --dir ./example` writes a minimal pair of tree files and a recorded GraphQL replay fixture. Run `nova-work verify --tree ./example/tree.lisp --against ./example/against.lisp` with no network or credentials: `VERIFY OK ... differences=0`, exit 0. `nova-work verify -h` also prints the tree's grammar and a minimal tree. Save that tree as `a.lisp`, copy it to `b.lisp` with
`:archived true`, and run `nova-work verify --tree a.lisp --against b.lisp`: one
`VERIFY DRIFT ... field=archived` line under `VERIFY FAIL`, exit 1.

**First run against GitHub.** Needs `gh auth status` to pass and one repository
you can read: export `ORG` and `REPO`, then run the three lines of the banner's
`example:` block in a scratch directory (a dry run, the import to
`./tree.lisp`, the verify). The executed transcript of that sitting, with every
line of output, is in [TESTS.md](TESTS.md#nova-work).

**Output.** One result per run: `IMPORT OK`, `VERIFY OK`, `VERIFY FAIL` (exit 1,
the differences as `VERIFY MISSING`, `EXTRA` or `DRIFT` lines, values quoted),
or `<VERB> REFUSED: <why>; run: <next command>` (exit 2). `--json` prints the
same result as one JSON object.

### import

Reads every issue of `--org` (or of each `--repo`) through your `gh` login,
read-only, and writes one local tree file, `--out`, only after the encoded tree
has read back equal to what was fetched. `--dry-run` is not offline: it reads
GitHub exactly as the import does (every issue, the same calls), checks the
round trip, and writes nothing. A dry run costs what the import costs:
`IMPORT PLAN` names `est_calls`, and `--max-calls` (default 1500) refuses a plan
past it before any issue is read.

### verify

Reads the tree and GitHub again and writes nothing: zero differences is
`VERIFY OK ... differences=0`, the receipt that the tree holds what GitHub
holds. `--against <tree>` puts a second tree file where GitHub stands and reads
no network at all.

### example

Writes a minimal pair of tree files (`tree.lisp`, `against.lisp`) and a recorded
GraphQL import replay fixture (`call-01.json`, `call-02.json`) under `--dir`.
These allow running `nova-work verify` and `nova-work import` offline without `gh`
credentials or network access:

```
nova-work example --dir <dir> [--replace]
```

### roadmap check, list, add, remove, pull, done, note, render

```
nova-work roadmap check  [--repo <dir>]
nova-work roadmap list   [--repo <dir>] [--file roadmap|fixes] [--release <v> | --group <g>] [--state <s>] [--text <t>] [--max <n>]
nova-work roadmap add    [--repo <dir>] --id <id> (--release <v> | --group <g>) --title <t> [--text <t>] --origin <o> [--status planned|in-progress] [--why <w>]
nova-work roadmap remove [--repo <dir>] --id <id>...
nova-work roadmap pull   [--repo <dir>] --id <id>... (--release <v> | --group <g>)
nova-work roadmap done   [--repo <dir>] --id <id>... --evidence <PR #n | commit>
nova-work roadmap note   [--repo <dir>] --id <id>... [--text <t>] [--title <t>]
nova-work roadmap render [--repo <dir>] | --file <data.sexp> --out <page.md> [--source <name>]
```

Edit and read a repository's own work record: `docs/roadmap.sexp` (rendered to
`ROADMAP.md`) and `docs/fixes.sexp` (rendered to `FIXES.md`), in the shapes of
`internal/roadmapdoc`. Every writing verb edits the data, decodes it again, and
writes the data and its page in the same step, so neither is edited by hand and
the sync tests (`TestRoadmapIsGeneratedFromTheSexp`,
`TestFixesIsGeneratedFromTheSexp`) stay green. Every byte a verb does not change
(comments, spacing, the other records) is kept; a removed record takes only its
own trailing comment with it. `--id` repeats for more entries.

The verbs read and write local files only: no GitHub call per entry. A batch of
edits lands as one commit and one pull request, so GitHub sees an occasional push
rather than one API call per item (the 2026-10-10 cleanup made about 1,000 calls,
one per issue and pull request closed or moved, and tripped GitHub's secondary
rate limit).

- `check`: both files decode, no id stands in both, each page is what its data
  renders. Exit 1, one `ROADMAP-CHECK FAILED` line per problem.
- `list`: the entries, by file, release or group, state and words; `--json` is
  one object with an `entry` item per entry (id, file, kind, place, state, title).
- `add`: a roadmap item under `--group`, or a fix in `--release` (planned unless
  `--status in-progress`). A duplicate id (in either file, groups and releases
  included) is refused.
- `remove`: the entries go.
- `pull`: to `--release`, an item becomes a planned fix or a fix changes release;
  to `--group`, a fix becomes an item or an item changes group.
- `done`: a fix's status becomes `done`; an item moves to the roadmap's `:done`
  list, dated today. `--evidence` (a pull request or a commit) is appended to the
  text.
- `note`: `--text` is appended to each entry's text; `--title` retitles it, and
  the title it had is kept under `:kept` (`earlier-title`).

A move across the two files, and `done` of an item, carries every field. A field
the target shape has no key for is kept under the record's `:kept` as
`<from>-<key>` (`item-why` on a fix, `fix-status` on an item), and a move back
restores it, so roadmap -> fixes -> roadmap gives back the item it was.
- `render`: writes the pages from the data; `--file` and `--out` render data kept
  outside the repository whose page it is (a roadmap held in the private work
  repository) through the same renderer.

An entry is todo (a roadmap item), open (a fix, planned or in-progress) or done
(an item on `:done`, a fix done or shipped), and its rules are modelled in
[tla/RoadmapEntry.tla](../tla/RoadmapEntry.tla): done is final; a release whose
status is shipped is frozen; every group and release keeps one entry, so a
remove, pull or done that would empty one is refused; every entry names a group
or release that is there. A pull across the two files writes the destination
first, so a stop between the writes leaves the id in both files (check names
it, every verb refuses until one copy is removed), never in neither. On a
refusal (exit 2) nothing is written.

The roadmap verbs of nova-sprint 8c87a4b (`roadmap check`, `add`, `remove`,
`pull`, `note`) edited the private work record's `roadmaps/*.sexp` and were left
out by the v1.2.3 re-seed. These verbs replace them, on the repository's own files.


### A reader may read now

`cannot ask` means no up unit may read the primary at its read tier. The next
tick closes it with `a reader may read <card> now (<units>)` when an eligible
reader exists, even while that reader waits for room. An unchanged eligible
set raises at most one judgment per episode; losing eligibility starts a new one.
`ask <card> --answers <j>` answers it when at least one read is dealt. With no
eligible reader, ask names the reader reason and remedy. `rework`, `drop` and
`wait` are also decisions; rework ends the attempt and closes the judgment.

A `wait` on `cannot ask` quiets its reader condition until the requested review
time; if no reader becomes eligible, that time raises one reminder. Eligibility
still closes the wait earlier. The wait does not block an eligible read.

`nova-sprint where` begins with `DECISIONS OWED n, oldest <age>; server-defaults applied this hour n; delivery faults n`. A delivery fault means an escalation still
needs a successful bus receipt; it is not proof that the coordinator or owner saw
anything. The server folds overdue decisions to the coordinator once per changed set. When
the owner lane has the same recipient and set, that one notice serves both lanes;
otherwise the owner lane keeps its 30-minute delivery cadence, including changed
sets. Failed or ambiguous sends retry, including a renewed token after expiry. Safe defaults
and the terminal sprint-done exemption are listed in SPEC-SPRINT.md, "Decisions owed".

Server-default parked cards remain in DECISIONS OWED and folded escalation until
released, even after their audit report is older than one hour. Their hold reason
names server-default and the original judgment. Oldest decision age excludes
STOPPED time. The owner lane sends at most once every 30 minutes after a successful
receipt, including when the set changes; pending failed deliveries retain retries.
