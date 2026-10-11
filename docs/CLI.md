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

Try one card's whole flow with no Redis or git. `--redis mem:<file>` (or
`NOVA_SPRINT_REDIS=mem:<file>`) loads an in-memory twin from a file and saves it
after each command. A twin is for learning and tests; run one command at a
time. The first line sets the store and actor. `finish` without `--head` and
`merge` stand in for a worker's pushed commit and its landing, so this flow
needs no forge:

```sh
export NOVA_SPRINT_REDIS=mem:sprint.twin NOVA_SPRINT_ACTOR=boss
nova-sprint init --readers reader-a,reader-b --members m1
nova-sprint add --stream s1 --count 1 --one
nova-sprint start
nova-sprint tick
nova-sprint tick
nova-sprint take --as m1 --epoch 0
nova-sprint finish --as m1 s1-1.w1@1 --epoch 0 --report done
nova-sprint tick
nova-sprint read --as reader-a --begin --epoch 0
nova-sprint read --as reader-a --ok --epoch 0
nova-sprint tick
nova-sprint merge --stream s1 --batch 1   # twin only, a real store refuses a bare merge, use land
```

A twin beats every member and reader at every verb, so `m1` is up after the
first tick. Nothing runs between commands: tick by hand with `nova-sprint
tick`. `run`, `inbox --wait` and `where --watch` are refused. A card's move is
queued until the next tick prints `MOVED drain`; the tick after `merge`
completes its move to landed. The flow's output shows results, moves (`MOVED`),
refusals with reasons (`REFUSED`, on stderr), and the sprint's summary
(`landed/all percent -> ETA ...`).
[TESTS.md](TESTS.md#nova-sprint) carries the exact transcript through `merge`;
`cmd/nova-sprint/firstrun_test.go` runs it line by line.

### Landed series

`where --json` carries `landedSeries`: cards landed per 10 minutes over the last 24 hours, 144 buckets, split `friends` and `fleet`. A landing is a work-table move to `<stream>:landed` from any state but waiting. A sentinel's release is not work. Each card counts once, the first landing. The worker is the last `<who>:ok` of the card's work attempt (`<card>.wN`): `friend.<name>` is a friend and anything else is a fleet machine. The lander is not the worker. `where -h` states it. The text frame does not carry the series. `dashboard` reads one `where --json` and the series is on that object, so the page does not loop the log. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md), the `where` frame.

### Verbs

```
nova-sprint init [--readers <a,b,...>] [--members <m1[:<width>],m2,...>] [--coordinator <name>] [--rules <file>]
nova-sprint add --stream <s> (<id>... | --count <n> | --sentinel <id> | --brief-dir <dir> | --brief-file <f1> --brief-file <f2>...: a card per file, its id the file's name without .md) [--needs <a,b>] [--before <id> | --after <id> | --score <n>] [--brief <text> | --brief-file <path>: once, the brief of the cards named] [--rules <file>] [--replaces <old-id>[,<old-id>]]
nova-sprint quack --streams <a,b,...> --count <n> --repo <clone url> [--tiers <t,...>] [--base <branch>]
nova-sprint release (<sentinel>... | <selector> [--dry-run]) --reason <text> [--answers <note>]
nova-sprint release check [--json] [--streams <glob>] [--window <duration>] [--merge-p90 <duration>] [--check <name>]...
nova-sprint resolve [<id>...] [--stream <s>] [--limit <n>]
nova-sprint start
nova-sprint stop
nova-sprint run [--answer-rules=false] [--idle-alarm=false]
nova-sprint tick [--answer-rules] [--idle-alarm]
nova-sprint promote [--once] [--poll <duration>] [--every <duration>] [--landings <n>] [--branch <name>] [--repo-dir <clone>] [--base <branch>] [--check <command>] [--dry-run]
nova-sprint selftest [--dir <d>] [--keep]
nova-sprint goal set <name> [--file <path>] [--to file:<path>]
nova-sprint goal show [<name>]
nova-sprint goal drop <name>
nova-sprint take --as <member> [<card>@<gen>...] [--epoch <n>] [--limit <n>]
nova-sprint finish --as <member> <card>@<gen>... --epoch <n> (--head <commit> | --failed) [--report <text>] [--usage <text>]
nova-sprint progress --as <worker> <card>[@<gen>]... --epoch <n>
nova-sprint ask [<id>... | --group <id> [--expect <n>]] [--stream <s>] [--limit <n>] [--another] [--answers <note>]
nova-sprint queue --as <reader|member> | --stream <s>
nova-sprint read --as <reader> (--begin | --ok | --broken) [<card>...] --epoch <n> [--limit <n>] [--finding <text>] [--usage <text>] | --as <reader> --return <card> --reason <text> --epoch <n> [--usage <text>]
nova-sprint accept (<id>... | --stream <s> | --read-ok | --group <id> [--expect <n>]) [--answers <note>]   # the tick accepts every primary whose reads are all ok and tells the seat (ready to merge); accept is for a held primary or a stuck case, and accept --read-ok with nothing eligible says "nothing waits: the tick accepts"
nova-sprint rework (<id>... | --group <id> [--expect <n>] | <selector> [--dry-run]) [--fix <text>] [--answers <note>]
nova-sprint return (<id>... | --group <id> [--expect <n>] | <selector> [--dry-run]) [--reason <text>] [--answers <note>]
nova-sprint drop (<id>... | --stream <s> --col <state> | --repo <owner/name>... --expect <n> | --group <id> [--expect <n>] | <selector> [--dry-run]) --reason <text> [--answers <note>]
nova-sprint rank (<id>... | <selector> [--dry-run]) (--score <n> | --first) [--answers <note>]
nova-sprint priority <id>... | (<id>... | --stream <s>) (--blocker | --critical | --high | --normal | --low) --reason <text>
nova-sprint relink <old-id>[,<old-id>...] <new-id> [--reason <text>]
nova-sprint sentinel set <id> --needs <a,b>
nova-sprint brief <id> (--brief <text> | --brief-file <path>) [--rules <file>] [--answers <note>] | --dir <dir> [--rules <file>] | --group <id> [--expect <n>] (--brief-file <path> | --dir <dir>) [--answers <note>] | <id> --widen [--repo-dir <clone>] | <id> --tier <flash|pro|heavy|frontier> | <selector> (--set-base <branch> | --drop-who | --tier <t>)... [--dry-run]
nova-sprint recut <id> (--tier <flash|pro|heavy|frontier> | --brief-file <path> [--rules <file>]) [--new <id>] | <selector> (--tier <t> | --set-base <branch> | --drop-who)... [--dry-run]
nova-sprint twin <card> [--paths <extra,...>] [--needs <card,...>] [--before <card>] [--tier <t>] [--instruction <text>] [--carry]
nova-sprint move <id>... --stream <s> [--before <id> | --after <id> | --score <n>]
nova-sprint merge --stream <s> [--batch <n>] [--conflict <id> [--conflict-kind file|ledger] [--conflict-path <p>...] | --cross <id>=<other> | --red [--suspect <id>...] | --rejected | --base-red <error>] [--note <text>]
# a merge with no fact flag and no --landed is the twin's; on a real store use land, or record a pushed landing with --landed <id>@<head> --repo <dir> --base-ref <ref>
nova-sprint land [--stream <s>...] [--repo-dir <clone>] [--base <branch>] [--check <command>] [--dry-run]
nova-sprint rebase --from <branch> --to <branch> [--repo-dir <clone>] [--dry-run]
nova-sprint stream set <stream>... [--read-tier <flash|pro|heavy|default>] [--land-protected <owner/name,...|any|default>] [--release <name>] [--prose <glob,...|default>] [--attempts <n|default>] [--base <branch>] [--reason <text>] [--answers <notes>]
nova-sprint set [--read-tier <flash|pro|default>] [--read-cards <on|off|default>] [--dealt-max <duration|default>] [--go-lanes <n|default>] [--attempts <n|default>] [--friend-idle <duration|default>] [--friend-finish <duration|default>] [--fleet <on|off>] [--friends <on|off>] [--fleet-tiers <flash,pro,heavy,frontier|all>] [--friends-tiers <flash,pro,heavy,frontier|all>] [--reads <0|1|2|default>]
nova-sprint resume --stream <s> [--did <text>] [--answers <note>]
nova-sprint backup (--out <dir> [--part-bytes <n>] [--secrets-store <dir> --secrets-as <seat> --secrets-key <path> --sops <path>] | --file <path> [--dry-run])
nova-sprint demo load <backup.xz part>... [--sha256 <hex>] [--dir <dir>] [--xz <path>] [--redis-server <path>]
nova-sprint demo stop [--dir <dir>]
nova-sprint fleet beat <member> [--load <percent>] [--stop-returns <n>]
nova-sprint fleet up <member> [--width <n>]
nova-sprint fleet down <member>
nova-sprint fleet sync [--check] [--pg <dsn>]
nova-sprint fleet level
nova-sprint friend sync [--pg <dsn>] [--root <dir>] [--every <duration>]
nova-sprint friend sync install --every <duration> [--redis <addr>] [--pg <dsn>] [--root <dir>] [--dir <dir>] [--log <file>] [--dry-run]
nova-sprint friend sync uninstall [--dir <dir>] [--dry-run]
nova-sprint collect [<friend>...] [--dead-lanes] [--pg <dsn>] [--root <dir>] [--dry-run]
nova-sprint install server --listen <address:port> [--redis <addr>] [--land] [--decide <dir>] [--dir <dir>] [--log <file>] [--dry-run]
nova-sprint install member --as <member> --server <address:port> [--harness <path>] [--root <dir>] [--pass <NAME,...>] [--swarm <path>] [--dir <dir>] [--log <file>] [--dry-run]
nova-sprint install seat-push|friend-sync (seat install's and friend sync install's flags)
nova-sprint install table --out <file> [--every <duration>] [--redis <addr>] [--dir <dir>] [--log <file>] [--dry-run]
nova-sprint uninstall server|member|seat-push|friend-sync|table [--dir <dir>] [--dry-run]
nova-sprint units --check [--dir <dir>]
nova-sprint gc [--machine <m>] [--dry-run] [--max-age <d>] [--ai-root <dir>]
nova-sprint friend beat <friend> [--working <n>] [--queue <n>] [--width <n>] [--running <id>,...] [--load <percent>] [--stop-returns <n>]
nova-sprint friend down <friend> [--reason <text>] [--until <RFC3339>]
nova-sprint friend up <friend> [--width <n>]
nova-sprint friend cards <friend> [--json]
nova-sprint friend take <friend> (<id>... | --all-unstarted) [--reason <text>]
nova-sprint friend give <friend> <id>... [--reason <text>]
nova-sprint friend level
nova-sprint friend health <friend> (--state up|asleep|down --seen <RFC3339> --generation <n> [--queue <n>] [--working <n>] [--width <n>] [--reason <text>] [--until <RFC3339>] | --clear)
nova-sprint reader add <reader>... [--tiers <flash[,pro,heavy,frontier]|all|default>]
nova-sprint reader set <reader>... --tiers <flash[,pro,heavy,frontier]|all|default>
nova-sprint reader away <reader>...
nova-sprint reader up <reader>...
nova-sprint reader remove <reader>...
nova-sprint stream remove <stream>...
nova-sprint stream archive <stream>...
nova-sprint stream unarchive <stream>...
nova-sprint ci <id>... (--red | --green) --epoch <n> [--head <h>] [--run <id>] [--source <s>] [--note <text>]
nova-sprint wait <note> (--for <duration> | --until <RFC3339>)
nova-sprint ack <note>... --reason <text>
nova-sprint answer [--dry-run] [--bar <p>] [--every <duration>] [--timeout <duration>] [--backend jev|fixed] [--answers <file>] [--record <file>]
nova-sprint inbox [--open <group>] [--read] [--wait [--timeout <duration>]] [--deadline <duration>] [--stale <duration>]
nova-sprint card <id> [--brief | --fields] [--json] [--at-epoch <n>]
nova-sprint card (--all | --stream <s>) --json [--at-epoch <n>]
nova-sprint card base <id> <branch> [--repo-dir <clone>]
nova-sprint streams [--repo <owner/name>] [--release <name>] [--cards]
nova-sprint log [--card <id>] [--stream <s>] [--member <m>] [--since <10m|RFC3339>] [--at-epoch <n>]
nova-sprint check
nova-sprint repair
nova-sprint where [--watch] [--every <duration>] [--all] [--json [--cards] [--rows] [--archived] [--stale <duration>] [--at-epoch <n>]: includes landedSeries] [--release [<name>]]
nova-sprint view coordinator [--all] [--since <cursor>] [--json]
nova-sprint view cards [--col <c>] [--stream <s>] [--holder <member>] [--by tier|stream|col|holder] [--json]
nova-sprint view worker --as <member|friend> [--since <cursor>] [--json]
nova-sprint dashboard [--listen <address:port>[,...] | none] [--pull <address:port>[,...] | none] [--logo <file>] [--every <duration>]
nova-sprint seat
nova-sprint seat login --store <secrets dir> --as <seat> --key <keyfile> --secret <NAME> --user <redis user> --redis <addr> [--sops <path>]
nova-sprint seat login --check
nova-sprint seat logout
nova-sprint seat push [--harness <name> --target <dir> [--session <id>]] [--json]
nova-sprint seat pong <nonce>
nova-sprint seat watch <dir> [--json]
nova-sprint seat install --harness <name> --target <dir> [--session <id>] [--server <host:port>] [--config-seat <name> --config-dsn <dsn> --config-password-env <NAME>] [--dry-run]
nova-sprint seat check
nova-sprint routes
nova-sprint rules
nova-sprint funded <provider> --reason <text>
nova-sprint cost reconcile [--dry-run] [--json]
nova-sprint cost reprice [--route <r>]... [--since <RFC3339>] [--dry-run] [--json]
nova-sprint stats [--routes [--since <10m|RFC3339>]]
nova-sprint stats tidy (--friends | --fleet | --routes | --streams | --all)... --reason <text> [--dry-run]
nova-sprint play [--simulation] [--seed <n>] [--every <duration>] [--broken <p>] [--fail <p>] [--stuck <p>] [--cross <p>] [--down <p>] [--up <p>] [--red <p>] [--flap <p>] [--batch <n>] [--hold] [--silent <member>@<from>+<for>]... [--ticks <n>]
nova-sprint clear --confirm sprint
nova-sprint teardown --confirm sprint
```

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
`NOVA_REDIS_ADDR`, then the address `seat login` recorded), `--actor <name>` (else `NOVA_SPRINT_ACTOR`; no default — a
verb that writes wants one), `--op <id>` (the same id again returns the recorded
result), `--json` and `--max <n>` (listed items; 0 is all). The coordinator's
verbs are the coordinator's alone (the first `init` names it: `--coordinator`,
else the actor); `take`, `finish`, `read`, `fleet beat` and `friend beat` are the
workers', whose actor is the member, reader or friend named; `merge` and `ci`
are reports; `tick`, `run`, `friend clean` and `gc` are the machine's. Reads need no
actor except `inbox --read`, which moves the coordinator's cursor. A set is
ids, a stream, a column, `--max n` (`--limit` is an alias), or an inbox group:
`--group <id>`, the id `inbox` prints, with `--expect <n>` the size it printed,
which refuses a group that has changed. `nova-sprint help <verb>` (or
`<verb> -h`) prints one verb's usage, flags and exit codes; `nova-sprint help
<group>` (fleet, friend, reader, goal, stream) prints one group's.

### A card's priority

Every card carries a level of the ladder blocker, critical, high, reader, normal, low; a
read card is reader or its primary's higher level, a primary normal unless its brief's `PRIORITY: <level>` line, its
stream's default or `priority` sets it. `priority s1-4 --high --reason '<why>'` sets one card,
`priority --stream s2 --low --reason '<why>'` sets, in one call, every card now in the stream
(its own level overwritten) and the stream's default for cards added later, and `priority s1-4` prints the level and where it comes from;
each change is on the card's timeline (`log --card`) with the actor and the reason. Every
deal places the cards above reader first, then the reads, then normal and low work in the
room the reads leave; `where` prints the levels beside the critical list and the backup
state (`backup: reads (review ... > working ...)`) while there is one
(docs/SPEC-SPRINT.md section 1, "Priority").

### The seat's store login

`nova-sprint seat login --store <secrets dir> --as <seat> --key <keyfile> --secret <NAME> --user <redis user> --redis <addr>` records the store login in `~/.config/nova-sprint/login.json` (or under `$XDG_CONFIG_HOME`), mode 0600: the address, the user and where the password is in nova-secrets, never the password, and only once the secret resolves. After it, `nova-sprint <verb>` typed bare reaches that store as that user, the password read in the verb's own process through nova-secrets' checks, with no `nova-secrets exec` wrapper; `--redis`, `NOVA_SPRINT_REDIS`/`NOVA_REDIS_ADDR` and `NOVA_SPRINT_REDIS_USER` still win. `seat login --check` prints `SEAT LOGIN file=… redis=… user=… … resolves=yes|no` (exit 1 on no), the password never shown; `seat logout` removes the record. A recorded secret that does not resolve is refused naming the file and the remedy, never dialed without a password. `nova-sprint run --keys <NAME,...>` records the decision key and each provider key in `keys.json` beside that login (names only, never a value) and the run process reads each one from the seat; a name that cannot be read refuses at start, naming the name. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#the-seats-store-login) and [SPEC-SECRETS.md](SPEC-SECRETS.md) ("A tool's store login").

The seat is held only by a session the push loop reaches ([SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#the-push-proof)). `nova-sprint seat install --actor <seat> --harness <harness> --target <session dir>` records the seat's push target and installs the push loop; the loop delivers `NOVA SPRINT PUSH CHECK <nonce>` into the session through the harness's nova-friend adapter, and the session answers with `nova-sprint seat pong <nonce> --actor <seat>`. Until that pong is in, and again whenever it is older than 15 minutes (the loop asks every 10), every coordinator verb is refused with one line, `PUSH DOWN: <why>; ... run: nova-sprint seat install ...`, and `coordinator <name>` refuses a name with no live proof. `seat push` prints `PUSH OK` or `PUSH DOWN` with why and the remedy (exit 1). A harness with no deliver command uses the folder adapter: install resolves `--target` to an absolute existing directory, and the push loop writes `PROOF-<nonce>` there. The actual nonce appears only in that filename; status, refusals, errors, and pong responses never reveal it. `seat push --json` reports `proof=none|pending|proven` without `nonce` or `pong_of`; `live` separately reports whether the proof is still valid. Run the printed Monitor command, `nova-sprint seat watch <dir>`, from inside the session, then answer each proof filename with `seat pong`. The native Monitor prints complete regular files already present and each new file every second, one flushed path per line (`--json`: one object with `path` per file); it skips dot files and directories, prints a removed name again when it reappears, writes nothing, runs locally, and stops on an interrupt. `seat check` prints `proven=<age> ago` on OK and `proven=-` plus both commands on DOWN.

`nova-sprint seat install --server <host:port> --config-seat <name> --config-dsn <dsn> --config-password-env <NAME>` (beside the push loop's unit) records the sprint's server in `seat.json` beside that login and writes the nova-config seat profile, the row `<name>\t<dsn>\t<NAME>` of `~/.config/nova-config/seats.tsv`. After it, `nova-sprint seat check` measures the recorded server when `NOVA_SPRINT_SERVER` is not set and prints `MACHINERY config OK seat=<name> …` (or DOWN with the remedy), and `nova-config <verb> --seat <name>` (or `NOVA_SEAT`) reaches the config store with no `nova-secrets exec`: the password is read in process from the store login's nova-secrets seat under `<NAME>` when that variable is not set and `NOVA_PG_PASSWORD_ENV` is not given (`NOVA_PG_PASSWORD_ENV` given still wins). The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#handing-over-the-seat) and [SPEC-CONFIG.md](SPEC-CONFIG.md#connecting).

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

A running sprint needs nine units on its coordinator's machine: the store and the bus (`nova-redis install store|bus`), the server, the machine's member, the seat's push loop, the friend sync loop and the live table (`nova-sprint install server|member|seat-push|friend-sync|table`), and the disk guard (`nova-swarm install disk-guard`) and the mirrors' refresh (`nova-swarm install mirror-refresh`, owed: the `nova-swarm mirror` verb is written, its unit is not). Each verb writes its unit (a launchd agent on macOS, a systemd user unit on Linux, kept alive and started again at login) into `--dir` (default `~/Library/LaunchAgents` or `~/.config/systemd/user`) and loads it; `--dry-run` prints it and writes nothing, and `uninstall <kind>` unloads and removes it. A unit runs the verb itself by the tool's absolute path, never under `nova-secrets exec`, a shell or a wrapper, and carries no secret: the server, the push loop and the table open the store with the seat login recorded by `seat login` (above), read in their own process, and install refuses a store this shell reaches as a user with no login recorded for it. Not yet in process: the server's decision loop reads its API key and the member the providers' keys `--pass` names from the service's environment, which the unit does not set. `install seat-push` and `install friend-sync` are `seat install` and `friend sync install`. `install table` runs `where --watch --every <d>` with its lines to `--out`. `nova-sprint units --check` reads the unit files and prints `UNIT <kind> installed|missing|different unit=<path>` for each of the nine, a different one with `why=` (the file runs a wrapper, another verb, or is no unit) and each not installed with `; run: <the verb that installs it>` (or `; owed: <the verb> (<what it waits on>)`), then `UNITS CHECK OK|DIFFERENT installed=<n> missing=<n> different=<n>`; exit 1 when one is not installed. `--json` prints the same as one object.

### The sprint backup

`nova-sprint backup --file <path>` writes the store to a new file (owner-only; an existing file is refused, never overwritten), reads it back against its SHA-256, restores it into a twin and compares it with the store (on a twin store its sprint state part for part; on a Redis the RDB's header and checksum alone), and scans it for secret-shaped text. A file that fails any step is removed. On success it prints `BACKUP OK file=<path> sha256=<hex> bytes=<n> keys=<n> cards=<n> restore=<semantic|integrity> compared=<state+document+counts|header+checksum> secrets=none`, and a backup checked at the integrity level ends it `; integrity only, not a semantic restore: the sprint's state was not loaded and compared`; a refusal names the failed step and, for a secret, the lines (never the value). It runs on the store's host for a Redis, and on any twin (`--redis mem:<file>`) with no server. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#sprint-backup-verb).

`nova-sprint backup --out <dir>` is the backup for the work record: the sprint's keys of its epoch, the keys every epoch shares and the records an older epoch left as a RESTORE text dump (`sprint-epoch<n>.restore.txt`, one `RESTORE <key> <ttl ms> <payload>` line a key), compressed with `xz -9` and split into parts under 100 MB (`--part-bytes`, default 95000000). It records the sums of the text and of the xz (and of each part) in `SHA256SUMS`, puts the parts together again, checks both sums, restores the dump into a throwaway store (a `redis-server` on a unix socket holding this build's function library, or a twin) and compares the keys and the cards of each column with the store's, and the sprint state part for part on both a twin and Redis; it then scans the dump and the restored values for every sealed nova-secrets value of the seat (`--secrets-store`, `--secrets-as`, `--secrets-key`, `--sops`, default the seat login's) in a child of `nova-secrets exec`, which prints counts only; and it writes `README.md`, naming the parts in order, the sums and the load command. `--out` must not exist or be empty; it is written only when every step passed. It prints one `BACKUP FILE <name> bytes=<n> sha256=<hex>` line per file and `BACKUP OK out=<dir> epoch=<n> keys=<n> cards=<n> (<col>=<n> ...) restored=<twin> keys=<n> cards=<n> (...) parts=<n> text_sha256=<hex> xz_sha256=<hex> secrets=<n> matched=0 restore=<semantic|integrity> compared=<state+counts|counts>`, and a count-only fallback, whose source or loader cannot expose logical state, ends it `; integrity only, not a semantic restore: the sprint's state was not loaded and compared`. A match fails it with exit 1 and `secrets=<n> matched=<k>`, never a value or where it was. It needs `xz` and `split` on PATH (`--xz`, `--split`), and `redis-server` (`--redis-server`) for a Redis store. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#sprint-backup-out).

### A backup as a demo

`nova-sprint demo load sprint-store-2026-10-04-2336.redis.txt.xz.part-*` loads a store backup (the RESTORE text dump, xz, split into parts) into a throwaway Redis on a free 127.0.0.1 port, with the function library of this nova-sprint binary (never the installed nova-redis's), and prints `where` against it and the line `DEMO UP --addr 127.0.0.1:<port>`: point any read verb at the demo with `--redis 127.0.0.1:<port>`. The parts are joined in name order and checked against the sum beside them when there is one: `<file>.sha256` (the hand backup's), else the line of `SHA256SUMS` naming the joined file (`backup --out`'s), or `--sha256 <hex>`. It takes no `--redis`: the only store it opens is the one it starts. The server's directory and the state file (`demo.json`: address, port, pid, directory) are under `--dir`, by default the user cache directory's `nova-sprint/demo`; a second load while one is up is refused. `nova-sprint demo stop` stops that Redis by the pid it recorded, only when the Redis at the recorded address is that pid, and removes the recorded directory and nothing else. The live store is never opened. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#demo-load-verb).

### The machinery's scratch

`nova-sprint gc [--machine <m>] [--dry-run] [--max-age <d>] [--ai-root <dir>]` reclaims, on the machine it runs on (or, with `--machine`, on that machine through the fleet runner, which runs the same verb there), exactly the scratch the machinery made and no longer needs: the job directories of finished or absent lanes, reader checkouts of recorded findings, lander worktrees, bench directories under `~/nova-bench` older than `--max-age` (default `2d`; days or a Go duration), and the go caches trimmed to their cap. It refuses a path under no known scratch root (the AI root: `--ai-root`, else `NOVA_AI_ROOT`, else `~/ai`, else the one the home's `<name>-working` links name, `<root>/<name>/working` or `<root>/buds/<name>/working`, as on a machine that exports none; the bench root; land's clone root; a plain `<home>/<name>-working` directory, as a bench keeps one), and keeps a clone with uncommitted work, a stash or unpushed commits. It prints one line per class, `GC jobs|reads|landers|bench|cache count=<n> bytes=<b> kept=<n> refused=<n> failed=<n>`, and `GC OK freed=<bytes> volume=<use%>`; `--dry-run` says `GC WOULD-REMOVE` and removes nothing. `nova-sprint run` runs it on every machine once an hour and as soon as a machine's volume is at 80%. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md) section 1, "gc".

### A card re-cut as its twin

A card re-cut under a new id is its old card's twin: `add --stream s1 lint-pkg-cairn-tb
--brief-file lint-pkg-cairn-tb.md --replaces lint-pkg-cairn-t` admits the twin, makes
every waiting card that needed the old id need the twin instead (`card <dependent>` shows
the new need), drops the old card `replaced by lint-pkg-cairn-tb`, and raises no "blocked
on something dropped" judgment, in one step. Where the drop and the add were made apart,
`relink lint-pkg-cairn-t lint-pkg-cairn-tb` re-points the edges and answers the blocked
judgments of that pair. The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md) section 2, "A
card replaced by its twin".

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

### Role views: what a model reads instead of the dashboard

The owner, 2026-10-04: "i'd rather you hit this vs. hitting my dashboard which is for human
eyes". `nova-sprint view coordinator` is everything that needs the seat now, ranked by the
cards behind each item: open judgments, notes addressed to the coordinator, alarms (the
machine stopped, the fleet idle, nothing ready, a review or merge backlog, a stream stopped),
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
--cards` read at most once a second however many pull. An unknown name is a 404 of one
line; `where --json` also carries `landedSeries` (cards landed per 10-minute bucket over 24 hours, split between friends and fleet). The contract is [SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md), the dashboard.

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
stops the machine (`machine: STOPPED (every provider is out of credit)`) and `start` is refused
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
(`PROMOTE WAIT ... checks pending: <names>`), looking again every `--poll` (default 1m).
Without `--once` the verb repeats every `--every`. `--dry-run` prints the cut it would make,
or the promotion in flight, and writes, enqueues and records nothing. The contract is
[SPEC-SPRINT.md section 11](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md), promote.

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
nothing; `--every 60s` runs it as the seat's loop until the machine is STOPPED. Jev's key comes from `JEV_API_KEY`:
`nova-secrets exec --only JEV_API_KEY -- nova-sprint answer`. The
contract is [SPEC-SPRINT.md section 8](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#answered-by-nova-decide)
and [SPEC-NOVA-DECIDE.md section 13](SPEC-NOVA-DECIDE.md#13-the-judgment-decision).

`add` under `JEV_API_KEY` (`nova-secrets exec --only JEV_API_KEY -- nova-sprint add
...`) asks nova-decide's brief decision of every card it names with a brief after its
own checks and before it writes, one deadline for the batch: one `BRIEF card=<id>
op=<card>@brief-<hex> p_converges= minutes= failed= uncalibrated=true recorded=` line
per card, recorded in `~/nova-sprint/decide/brief.jsonl` (or `--decide-record <file>`),
the op stored on the card for land and drop to attach its end. The decision is
uncalibrated: nova-config's `sprint` row `decide_brief_bar` stays empty, which reports
only, until the brief record's own outcomes support a bar
([SPEC-NOVA-DECIDE.md](SPEC-NOVA-DECIDE.md) section 14).

`add` holds every card brief (one with a `PATHS:` line) to the brief checks at its base
before it writes: the repository `REPO:` names, in the lander's clone, at the tip of `BASE:`,
fetched once a call, read with git and no go command. The tokens are `paths-at-base`,
`donewhen-test-name`, `paths-cover-named`, `paths-cover-test`, `paths-cover-testdata`,
`paths-cover-ledgers`, `paths-cover-docs`, `base-is-live`, `tier-set`, `tla-is-frontier` and
`who-serves-tier`; each finding is a `LINT DRIFT card=<id> check=<token> line=<n>: <excerpt>
remedy=<remedy>` line and each corrected header line a `LINT FIX card=<id> <line>` line (the
`PATHS:`, `NEW:` or `SHARED:` line, or line 1, with every addition applied), then one refusal,
exit 2, nothing written. A base that cannot be read refuses with `MISSING: <what>`; a brief
naming no `REPO:` or no `BASE:` is held only to the checks that need no tree
([SPEC-SPRINT.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md) section 11, the brief checks).

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
| 2 | usage, or a store that did not answer (`fleet sync --check`: there is drift) |
| 3 | unreadable config (`fleet sync`, `friend sync`, `friend clean`), missing friend rows (`friend sync`, `friend clean`), or a replaced binary (`run`, `dashboard`) |

### What it does not prove

A landed card records a completed flow: the worker reports done, two different
readers pass that head, the tick queues it for merging and tells the seat
("ready to merge", a notice; the coordinator's `accept` is for a primary the
tick holds), and the landing is reported. These are recorded judgments, not a
proof that the work is correct. A twin exercises that flow one command at a
time. It has no beats or ticks between commands, so it does not test fleet
timing, the `run` loop, `inbox --wait` or liveness. `finish` without `--head`,
then `merge`, records a landing without a push. The work table's cost column
is, per stream, the sum of its landed cards' total cost in US dollars — each
card's actual cost where one was priced, else its predicted one, `-` when
none was — so a total is a ledger of recorded spend, not a proof of it.

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
contract is [SPEC-RELEASE.md](SPEC-RELEASE.md) section 16, subsection
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
n=0. The contract is [SPEC-RELEASE.md](SPEC-RELEASE.md) section 16, subsection
release-check-merge-queue-p90-b.w7.


## nova-card

nova-card is pre-alpha: not ready for production use.

```
nova-card generate --from ledger --ledger <name> --repo-dir <dir> --out <dir> [--tier flash|pro] [--prefix <p>] [--minutes <n>] [--max <n>] [--base <branch>] [--repo <owner/name>] [--name <n>...] [--dropped <id>...] [--dry-run]
nova-card generate --from findings --file <tsv> --out <dir> (--repo-dir <dir> | --repo <owner/name> --base <branch> --sha <40hex>) [--tier flash|pro] [--prefix <p>] [--minutes <n>] [--max <n>] [--name <n>...] [--dropped <id>...] [--dry-run]
nova-card generate --from help --tool <name> [--tool <name>...] --out <dir> [--bin-dir <dir>] (--repo-dir <dir> | --repo --base --sha) [--tier flash|pro] [--prefix <p>] [--minutes <n>] [--max <n>] [--name <n>...] [--dropped <id>...] [--dry-run]
nova-card lint --card <file> [--card <file>...] [--name <n>...] [--dropped <id>...]
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
nova-sprint add --stream debt --brief-dir ./cards --allow-shared-paths
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
path and neither needs the other, so the add wants `--allow-shared-paths`; the
`CARDS OK` line says `shared-paths=yes` when it does.

### What it refuses

Every brief is held to the lint `nova-sprint add` runs (the model lines, the
child rules under the default rule set, a tree card's steps), and past the add
to the typed header and the template's unfilled `<...>` lines, which the add
does not read, before anything is written (a sprint initialised with `--rules`
holds a brief to that file at the add), and to the card checks the add runs
too: a tier on line 1, a TEST whose package PATHS names, no name `--name` gives
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

**Try it with no login.** `nova-work verify -h` prints the tree's grammar and a
minimal tree. Save that tree as `a.lisp`, copy it to `b.lisp` with
`:archived true`, and run `nova-work verify --tree a.lisp --against b.lisp`: one
`VERIFY DRIFT ... field=archived` line under `VERIFY FAIL`, exit 1. The same
file against itself is `VERIFY OK ... differences=0`, exit 0.

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

