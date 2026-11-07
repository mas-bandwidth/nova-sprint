# The seams

Every seam between the layers, as Go, and the adapter that crosses it. The rule: **the lower layer's types are the contract; the upper layer adapts.** No layer defines a type for another layer's use. An adapter lives in the layer above, and in run (layer 6) when it joins two layers beside each other (steps and policy, steps and edge).

```
           verbs / cmd (layer 6: one flag parser, one refusal, one receipt)
                |             \
              run (layer 6: the tick, the pusher, the dashboard feed, adapters)
          /     |      |      \        \
     steps    policy  roster  edge     dash
     (3)       (5)    (cfg)    (4)     (page)
          \     |      |      /
           card (2)   |     /
               \      |    /
                store (1)
```

## 1. store -> everyone

`store.Store` (layer 1) is the only way to the state: `Snapshot`, `Apply`, `Replay`, `Events(after, max)`, `EventIdentity`, `Restored`, `Clear`, `Policy`. One `Apply` is one op, one transaction, fenced by its expects. A row is sprint state only: `Row{Name, State, Session, Role, Flaps, Held, Reason, Gen, ...}`; width, tiers, model and harness belong to the roster. A stream holds its own settings: `Stream{Name, Repo, Base, Held, Pos, Mode, Forge}` (`Mode` auto, group or hand; `Forge` "" or "github"). `Snapshot.Stopped` and `StopReason` are the machine line; `Snapshot.Identity` is the git identity every commit of the sprint is made as (`SetIdentity`).

## 2. card -> steps and verbs

`card` (layer 2) is pure: `Move`, `MoveConsumer`, `MoveRow`, `MoveStream`, `MoveAdd`, `ReportPlan` and `Merge` return a `card.Plan` (writes and expects) or a `*card.Refusal` naming the fix. Steps and verbs build ops from plans. card reads no clock and no store.

## 3. steps -> run

```go
func steps.Build(s store.Snapshot, now time.Time) *steps.View   // once, at run's cold start
func (v *steps.View) Fold(events []store.Event)                 // every tick, the events since the cursor
func steps.Tick(v *steps.View, in steps.Inputs, ask steps.Asker) steps.Plan
func steps.Verb(v *steps.View, in steps.Inputs, r steps.Request) (store.Op, *card.Refusal)

type steps.Inputs struct {
    Now      time.Time
    Inbox    []steps.InboxItem     // the last tick's Carry first, then edge results in arrival order
    Inflight []steps.EdgeRequest   // dispatched, not yet answered by a consumed result
    Heads    map[string]string     // stream -> its base branch's tip sha
    Seen     map[string]time.Time  // row -> last seen in nova-bus who
    Refused  []steps.RefusedOp     // the last tick's refused ops, from their receipts (OpGroups)
    Roster   roster.Roster
}
type steps.Plan struct {
    Ops     []store.Op
    Edges   []steps.EdgeRequest
    Answers []steps.RowAnswer
    Carry   []steps.InboxItem
    Nudges  []steps.Nudge
}
type steps.EdgeRequest struct {
    ID, Kind, Stream       string
    Cards, Heads, Branches []string
    Base                   string
    Wait                   int
    Row, Consumer          string
    After                  string
}
type steps.InboxItem struct {
    ID, Kind               string   // report, take, land, stop
    Row, Session, Consumer string
    Result, Head, Reason   string
    Cost                   int64
    Priced                 bool
    Usage                  string
    Request, Outcome       string   // an edge result: the request it answers, and how it ended
    Landed                 []string
    Merge                  string
}
type steps.Asker interface { Ask(steps.Questions) steps.Answers }
```

The View is built **once** and folded after: a second `Build` loses `Since`, `TakenAt`, `Landed`, `Down` and `Faults`. Run applies `Plan.Ops` in order, one `Apply` each, folding after each; a refused op becomes a `steps.RefusedOp{ID, Reason, Groups: steps.OpGroups(op)}` for the next tick. `Plan.Carry` heads the next inbox. `Plan.Nudges` are pushed to their rows outside the log. An edge request goes out only when its `After` op was accepted ("" waits for none); one not sent is not kept, because steps reissues it with the same ID. A tick while `Snapshot.Stopped` is not run at all (`Run.tla` `NoTickWhileStopped`).

Verbs that move cards for a person (`judge`, `hold`, `drop`, `priority`, `edit`) build their op with `steps.Verb` on a View built from one snapshot, so a person's move is checked by the same actor rules as the tick's.

## 4. policy -> steps (adapter in run: `run/asker.go`)

```go
func policy.NewEngine(clock policy.Clock) *policy.Engine
func (e *policy.Engine) AnswerContext(ctx context.Context, vs policy.Versions, snap store.Snapshot, ro roster.Roster, q policy.Questions) (policy.Answers, []policy.Failure)
func policy.Judgements(snap store.Snapshot, ro roster.Roster, failed []policy.Failure) []store.PutJudgement
func policy.PrevOf(e store.Event) (int, bool)   // run folds it: the version active before
```

`run.asker` implements `steps.Asker` over one tick's snapshot, roster and versions. It turns `steps.Questions` (facts per subject) into `policy.Questions` (ids per subject), calls `AnswerContext` under the tick's budget, and turns `policy.Answers` into `steps.Answers` (rows keyed by card id for `kind work` and `card#read` for reads; limits and timeouts copied field by field; `Failed` from the failures). The failures' judgements (`policy.Judgements`) go into the tick as one op of their own. `Versions` come from the snapshot's active version and the previous one that run keeps by folding `policy.PrevOf`; there is no walk from seq 0, and at a cold start there is no previous version until the next activation, so the built-in policy backs the active version until then.

Policy owns the run's numbers: `limits().fleet_width_cap` (the roster cap), `timeouts().tick` (the tick's budget), `timeouts().consumer[tier]` (the deadline the take receipt carries).

## 5. roster -> run (the real Source: `run/config_source.go`)

```go
func roster.Load(ctx context.Context, src roster.Source, cap int) (roster.Roster, error)
type roster.Source interface {
    Machines(ctx context.Context) ([]roster.Machine, error)
    Friends(ctx context.Context) ([]roster.Friend, error)
}
```

Run opens one `config.Store` (nova-tools' `config/lib`, `config.Open(ctx, dsn, password)`) for its life, takes **one** `(*config.Store).Snapshot(ctx)` per tick (machines and friends in one REPEATABLE READ transaction), and hands it to `roster.Load` through `snapshotSource`, which maps `config.Machine.Roles`, `config.Friend.Tiers` (comma lists) and `Cores *int` (nil is 0: `Load` holds such a bench at width 0 with `Worker.Held` = `roster.NoCores`, and the board shows it held with that reason). Calls are serialised by run, one tick at a time. A failed read is the tick's: no tick that second, and the board shows the failure (section 7).

## 6. edge -> run (`run/edges.go`)

```go
type bus.Bus interface {
    Send(ctx context.Context, to, subject, body string) (msgID string, err error)
    Who(ctx context.Context) (map[string]bus.Presence, error)
}
func git.LandPass(ctx context.Context, g git.Git, fg forge.Forge, req git.LandRequest) (git.LandResult, error)
func forge.New(forge.Config) *forge.Client
func git.New(git.Config) *git.Client
```

Run is the one pusher and the one caller of `nova-bus`: `bus.Edge` runs `nova-bus who` once a tick and `nova-bus send` for each envelope. A `steps.EdgeRequest` of kind `land` (`Cards`, `Heads`, `Branches`: the card branches `sprint/<stream>/<card>`; `Wait`: policy's `ci_wait` seconds) becomes one `git.LandRequest` run on a goroutine of its own, never under the tick, with the stream's forge (`Forge` "github": `forge.New`, acting as the host's gh login, with `GH_GATE_TOKEN` as an optional override). A stream with mode hand or no forge is answered at once with outcome `hand`. The pass is asked again after `LandPoll` on `ci_pending` and `base_moved`, and on `ci_absent` until `Wait` (10 minutes when none), when it is `no-ci`. Its `LandResult` comes back as one `steps.InboxItem{Kind: "land", Request, Outcome, Landed, Merge, Reason}`: landed -> `landed` with the landed heads and the merge sha; every other outcome (`ci_red`, `no-ci`, `conflict`, `merge_refused`, `pr_unopenable`, `branch_failed`, `ci_fault`, `missing_head`, `head_moved`, `hand`) keeps its name and carries the facts in `Reason` (rung, base tip, group tip, gated sha, PR, failing checks with their URL and first lines, conflicts, the pass's reason); an error is `fault`. Layer 3 opens one `land` judgement per card for each of those. A request of kind `stop` is one bus message to the row (`STOP <consumer>`) and, once sent, an inbox item `{Kind: "stop", Request, Outcome: "ack"}`. `Inputs.Heads` is the base tip of each stream as the edge last read it (`LsRemote` at start, after each landing, and every ten seconds).

The verbs' landing edge is `verbs.Lander` (`cmd/nova-sprint/lander.go`): `Land(ctx, stream, branch, head)` (the hand merge: the card branch's PR merged by the allowed method, or with no forge a fast-forward push of the head merged onto base; returns the merge sha), `Heads` (origin's branches) and `CreateGroup(ctx, stream, sha)` (`sprint-group/<stream>` at the base tip, at `stream add`).

The worker (`nova-worker`, layer 4) talks to the sprint only through verbs: `nova-sprint hook --as <row> --role worker`, `next --json`, `take --json`, `report`. The take receipt (`take <card>@<gen> --as <row> --json`) is the seam:

```
{consumer, producer, kind, stream, repo, base, branch, head, tier, brief, prose, finding,
 stream_ref, base_sha, prior_head, paths, done_when, epoch, gen, attempt, review, model,
 deadline, forge, commit_name, commit_email}
```

`stream_ref` is the stream's base branch. `deadline` is in seconds, from policy's `timeouts().consumer[tier]`. `forge` comes from the stream. `commit_name` and `commit_email` are the sprint's commit identity (`Snapshot.Identity`, set by `nova-sprint identity`); take refuses without one. `model` is the one the deal named and wrote on the consumer: a friend's own, from the roster, or for a bench the active policy's `catalog().bench[tier]` in provider/model form. Take reads it from the consumer and never chooses one, and a consumer dealt with no model is refused at take. The worker runs that model, and faults naming the model and the tier when its probe proved the harness does not serve it. `next --as <row> --json` carries the bench model per tier as `tier_models` (the probe's input). The worker holds no deadline, model or forge of its own.

The deal is the take: a consumer is `working` in the row's hand from the deal, so the worker runs every card `next --json` lists under `working` that has no job of its own yet, each in its own job, all at once. `take` is the row's acknowledgement (a cursor, the card's gen unchanged) and prints the receipt; a repeat prints the same. A card the row has not acknowledged is nudged (`NUDGE card=<id> ...`) each tick after a short delay from the deal, and the deal's push names the model (`model=`). A fault whose reason starts `missing-route: <route>` (the route's name as a brief declares it with `NEEDS-ROUTE:`) blocks the producer for the coordinator instead of dealing it again.

## 7. dash -> run

```go
type dash.Feed struct {
    Snap      store.Snapshot
    Roster    roster.Roster
    RosterErr error
    At        time.Time
    Landings  []time.Time   // the epoch's landing times, from the store's events; nil: unreadable, the ETA shows "-"
}
func dash.NewFromFeed(f dash.Feed, now func() time.Time) *dash.Cache
func (c *dash.Cache) Publish(f dash.Feed) error
func (c *dash.Cache) CheckFreshness(at time.Time)
func dash.Handler(c *dash.Cache) http.Handler
```

Run publishes its folded snapshot and the roster it read with it, as one pair, every tick (stopped or not). A failed roster read shows every row at width 0 with the alarm raised, never an old roster as current. The machine line is `machine: RUNNING` or `machine: STOPPED <reason>`. `run --listen` serves the board; `dash --pull` serves a pulled copy on a public host. The board's Why waiting panel calls `steps.Waits(snap.Cards)` and `steps.IdleWhy`, pure functions of the cards (no View) that `check` and the steps' idle judgement read too, so all three say one thing.

## 8. verbs -> run

```go
func run.Serve(ctx context.Context, cfg run.Config) error   // the `run` verb: lock, hook, cold start, ticks until ctx ends
type run.Config struct {
    Store         store.Store
    DSN, Password string          // the store, and the chooser lock's own session
    Roster        *run.ConfigRoster  // nova-config, for the roster
    Bus           bus.Bus
    Hook          run.Hook        // the `sprint` hook
    Policy        run.Asking
    Git           run.Git
    Forge         forge.Forge
    Listen        string          // "" serves no board
    Ready         chan<- string   // the board's address, once it listens
    Clock         run.Clock
    Log           io.Writer
    Host          string
    PID           int
    ReposDir      string          // where run keeps a clone per stream repo
    Reconcile     func(ctx context.Context, now time.Time, snap store.Snapshot) error // hourly provider-balance check; nil: none
}
func run.InspectChooser(ctx context.Context, dsn, password string) (run.ChooserStatus, error)   // `show run`
func run.OpenRestoreLock(ctx context.Context, dsn, password, host string, pid int) (run.RestoreLock, error) // `restore`
```
