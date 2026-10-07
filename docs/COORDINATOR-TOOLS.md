# Coordinator tools

The bring-up is what `nova-sprint start` (while the machine is running), `coordinator <name>` (given or taken), `handover`, and `check --bring-up` print after their own text. `seat` does not print it. `--json` stays one object. `coordinator --dry-run` changes nothing and prints no bring-up.

Each line is one thing the seat needs, the state just measured, and the one command that starts it. The state is `running`, `missing`, or `stale`. Missing is no evidence. Stale is evidence older than the thing's window. Running is fresh evidence that it answers. The command is a nova verb. It is not a shell script.

| thing | command |
|---|---|
| sprint server | `nova-sprint run --listen 127.0.0.1:6390` (or the address `NOVA_SPRINT_SERVER` names) |
| store | `nova-sprint where` |
| bus | `nova-bus peek --as <holder>` |
| judgment push | `nova-sprint inbox --wait --push seat` |
| event watch | `nova-sprint watch --events` |
| coordinator beat | `nova-sprint friend beat <name>`, or `nova-sprint fleet beat <name>` when the holder is a fleet member. When the beat is running and the holder is not held: `nova-sprint hold <name> --reason 'the coordinator is not taking cards'` |
| reader | `nova-sprint reader up <name>` |
| friend daemon | `nova-friend install --as <name>` |
| friend beat | `nova-sprint friend beat <name>` |
| dashboard | `nova-sprint dashboard --listen 127.0.0.1:7390` (or the address `NOVA_SPRINT_DASHBOARD` names) |

A reader line also names its width and the tiers it reads. When a tier has cards in review and fewer than two readers up can read it, the bring-up adds `BRING-UP warning tier=<t> review=<n> readers=<k> fewer than two readers can read it`.

`nova-sprint watch --events` does not wait and does not dial the bus. It prints one line per kind the epoch log holds, in this order: read-asked, read-verdict, work-finished, merge-queued, landing, blocked-merge, refused-drain, pushed-judgment. A kind with no line is omitted. Repeats of one kind are one line, `EVENT <kind> count=<n>`.

`check --bring-up` is the same check. The run loop runs it every pass and prints nothing. A line that was running and is then missing raises one judgment, type `bring-up down`, once for that transition. While it stays missing, no further judgment is written.
