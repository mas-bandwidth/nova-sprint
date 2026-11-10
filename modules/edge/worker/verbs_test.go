package worker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

type OriginChecker interface {
	HasHead(ctx context.Context, repo, branch, sha string) (bool, error)
}
type StoreSprintClient struct {
	Store        store.Store
	Origin       OriginChecker
	RequireHook  bool
	BusPresent   map[string]bool
	BusRedisAddr string
	Hooked       map[string]string
	HookCalls    int
	UnhookCalls  int
	Now          func() time.Time
}

func (s *StoreSprintClient) Hook(ctx context.Context, row string) error {
	return s.HookRole(ctx, row, "worker")
}

func (s *StoreSprintClient) Unhook(_ context.Context, row string) error {
	if _, ok := s.Hooked[row]; !ok {
		return fmt.Errorf("%w: %s is not hooked", ErrRefused, row)
	}
	delete(s.Hooked, row)
	s.UnhookCalls++
	return nil
}

func (s *StoreSprintClient) HookRole(ctx context.Context, row, role string) error {
	if role != "worker" && role != "coordinator" {
		return fmt.Errorf("SPRINT REFUSED hook: invalid role %q: pass --role coordinator|worker", role)
	}
	if s.BusPresent != nil && !s.BusPresent[row] {
		addr := cmp.Or(s.BusRedisAddr, "127.0.0.1:6381")
		return fmt.Errorf("hook in to the bus first: run NOVA_BUS_REDIS=%s nova-bus hook --as %s as the Monitor tool command, then hook in to the sprint again", addr, row)
	}
	snap, err := s.Store.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreDown, err)
	}
	r, ok := snap.Rows[row]
	if !ok {
		return fmt.Errorf("SPRINT REFUSED hook: unknown row %s: run nova-sprint show", row)
	}
	if s.Hooked == nil {
		s.Hooked = make(map[string]string)
	}
	s.Hooked[row] = role
	s.HookCalls++
	op := store.Op{
		ID:    fmt.Sprintf("hook-%s-%s-e%d-%d", row, role, snap.Epoch, s.HookCalls),
		Epoch: snap.Epoch, Actor: row, Kind: "row", At: s.now_internal(),
		Writes: []store.Write{store.PutRow{Name: r.Name, Session: role + ":" + strconv.Itoa(snap.Epoch), Role: role}},
	}
	_, err = s.applyWithReplay_internal(ctx, op)
	return err
}

func (s *StoreSprintClient) checkHooked_internal(snap store.Snapshot, row string) error {
	if !s.RequireHook {
		return nil
	}
	if role := s.Hooked[row]; role == "worker" || role == "coordinator" {
		return nil
	}
	if r, ok := snap.Rows[row]; ok && (strings.HasPrefix(r.Session, "worker") || strings.HasPrefix(r.Session, "coordinator")) {
		return nil
	}
	return fmt.Errorf("hook in first: nova-sprint hook --as %s --role worker", row)
}

func (s *StoreSprintClient) now_internal() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Unix(1000, 0).UTC()
}
func (s *StoreSprintClient) Next(ctx context.Context, row string) (NextResponse, error) {
	snap, err := s.Store.Snapshot(ctx)
	if err != nil {
		return NextResponse{}, fmt.Errorf("%w: %v", ErrStoreDown, err)
	}
	r, ok := snap.Rows[row]
	if !ok {
		return NextResponse{}, fmt.Errorf("SPRINT REFUSED next: unknown row %s: run nova-sprint show", row)
	}
	harness, _ := fakeRowHarness_internal(r.Name)
	out := NextResponse{Epoch: snap.Epoch, Seq: snap.Seq, Row: r.Name, State: r.State, Width: 2, Held: r.Held, Harness: harness}
	for _, c := range snap.Cards {
		if (c.Kind != "work" && c.Kind != "read") || c.Holder != row {
			continue
		}
		p := snap.Cards[c.Of]
		sum := ConsumerSummary{
			ID: c.ID, Producer: c.Of, Kind: c.Kind, Stream: c.Stream,
			State: c.State, Gen: c.Gen, Attempt: c.Attempt,
			Priority: p.Priority, Score: p.Score, Head: c.Head,
		}
		switch c.State {
		case card.Ready:
			out.Ready = append(out.Ready, sum)
		case card.Working:
			out.Working = append(out.Working, sum)
		case card.Done, card.Fail:
			out.Final = append(out.Final, sum)
		}
	}
	sortConsumers_internal(out.Ready)
	sortConsumers_internal(out.Working)
	sortConsumers_internal(out.Final)
	return out, nil
}
func fakeRowHarness_internal(row string) (harness, model string) {
	if strings.HasSuffix(row, ".script") {
		return "script", "script-v1"
	}
	return "opencode", "deepseek-r1"
}

// fakeTierDeadline is the seconds a test sprint prints in the receipt; the
// real number is policy's, and the worker takes whatever the receipt says.
const fakeTierDeadline = 900

// testCommitName and testCommitEmail are the sprint's commit identity every
// test receipt carries.
const (
	testCommitName  = "Sprint Bot"
	testCommitEmail = "1+sprint-bot@users.noreply.example.test"
)

func sortConsumers_internal(list []ConsumerSummary) {
	slices.SortFunc(list, func(a, b ConsumerSummary) int {
		if c := priorityRank_internal(b.Priority) - priorityRank_internal(a.Priority); c != 0 {
			return c
		}
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		return strings.Compare(a.ID, b.ID)
	})
}
func priorityRank_internal(level string) int {
	switch level {
	case "blocker":
		return 5
	case "critical":
		return 4
	case "high":
		return 3
	case "normal":
		return 2
	default:
		return 1
	}
}
func (s *StoreSprintClient) Take(ctx context.Context, row, consumerID string, gen int) (TakeInfo, error) {
	snap, err := s.Store.Snapshot(ctx)
	if err != nil {
		return TakeInfo{}, fmt.Errorf("%w: %v", ErrStoreDown, err)
	}
	if err := s.checkHooked_internal(snap, row); err != nil {
		return TakeInfo{}, fmt.Errorf("SPRINT REFUSED take: %w", err)
	}
	c, ok := snap.Cards[consumerID]
	if !ok {
		return TakeInfo{}, fmt.Errorf("SPRINT REFUSED take: unknown card %s: run nova-sprint next --as %s", consumerID, row)
	}
	if c.Gen != gen {
		return TakeInfo{}, fmt.Errorf("SPRINT REFUSED take: card %s moved to gen %d (expected %d): run nova-sprint show %s", consumerID, c.Gen, gen, consumerID)
	}
	p := snap.Cards[c.Of]
	if c.Of == "group" || c.Branch == "sprint/"+c.Stream+"/group" || strings.HasSuffix(c.Branch, "/group") || c.Branch == "sprint/"+c.Stream {
		return TakeInfo{}, fmt.Errorf("SPRINT REFUSED take: card %s branch %q collides with reserved stream integration branch sprint/%s/group", consumerID, c.Branch, c.Stream)
	}
	if c.Holder != row || c.State != card.Working {
		return TakeInfo{}, fmt.Errorf("SPRINT REFUSED take: %s is %s on %s, not in %s's hand: run nova-sprint show %s", consumerID, c.State, c.Holder, row, consumerID)
	}
	// the deal is the take: the acknowledgement is a cursor, as verbs' take
	op := store.Op{
		ID: store.ConsumerOpID("take", snap.Epoch, consumerID, gen), Epoch: snap.Epoch,
		Actor: row, Kind: "take", At: s.now_internal(), Expect: []store.Expect{store.CardGen{Card: consumerID, Gen: gen}},
		Writes: []store.Write{store.SetCursor{Name: steps.AckCursor(snap.Epoch, consumerID), Seq: max(snap.Seq, 1)}},
	}
	rec, err := s.applyWithReplay_internal(ctx, op)
	if err != nil {
		return TakeInfo{}, err
	}
	if !rec.Accepted {
		return TakeInfo{}, fmt.Errorf("SPRINT REFUSED take: %s: run nova-sprint show %s", rec.Reason, consumerID)
	}
	afterCard := c
	if rec.After != nil {
		if ac, ok := rec.After.Cards[consumerID]; ok {
			afterCard = ac
		}
	}
	st := snap.Streams[c.Stream]
	_, model := fakeRowHarness_internal(row)
	return buildTakeInfo_internal(snap.Epoch, afterCard, p, st, model, st.Forge), nil
}
func buildTakeInfo_internal(epoch int, c, p store.Card, st store.Stream, model, forge string) TakeInfo {
	head := c.Head
	if c.Kind == "read" && head == "" {
		head = p.Head
	}
	return TakeInfo{
		Epoch: epoch, Consumer: c.ID, Producer: c.Of, Kind: c.Kind, Stream: c.Stream,
		Repo: st.Repo, Base: st.Base, StreamRef: "sprint/" + c.Stream + "/group",
		Branch: c.Branch, BaseSHA: c.BaseSHA, PriorHead: c.Prior, Head: head,
		Tier: c.Tier, Brief: c.Brief, Prose: c.Prose, Finding: c.Finding,
		Paths: cloneStrings_internal(c.Paths), DoneWhen: cloneStrings_internal(c.DoneWhen),
		Gen: c.Gen, Attempt: c.Attempt, Review: c.Review, Model: model, Deadline: fakeTierDeadline, Forge: forge,
		CommitName: testCommitName, CommitEmail: testCommitEmail,
	}
}
func (s *StoreSprintClient) Report(ctx context.Context, req ReportRequest) (store.Receipt, error) {
	if strings.TrimSpace(req.ResultID) == "" {
		return store.Receipt{}, errors.New("SPRINT REFUSED report: missing result id: pass --id <result-id>")
	}
	if prev, found, err := s.Store.Replay(ctx, req.ResultID); err != nil {
		return store.Receipt{}, fmt.Errorf("%w: %v", ErrStoreDown, err)
	} else if found {
		return prev, nil
	}
	snap, err := s.Store.Snapshot(ctx)
	if err != nil {
		return store.Receipt{}, fmt.Errorf("%w: %v", ErrStoreDown, err)
	}
	if err := s.checkHooked_internal(snap, req.Row); err != nil {
		return store.Receipt{}, fmt.Errorf("SPRINT REFUSED report: %w", err)
	}
	op, err := s.buildReportOp_internal(ctx, snap, req)
	if err != nil {
		return store.Receipt{}, err
	}
	return s.applyWithReplay_internal(ctx, op)
}
func (s *StoreSprintClient) buildReportOp_internal(ctx context.Context, snap store.Snapshot, req ReportRequest) (store.Op, error) {
	c, ok := snap.Cards[req.Consumer]
	if !ok {
		return store.Op{}, fmt.Errorf("SPRINT REFUSED report: unknown consumer %s: run nova-sprint show", req.Consumer)
	}
	if c.Holder != req.Row {
		return store.Op{}, fmt.Errorf("SPRINT REFUSED report: consumer %s is held by %s, not %s: run nova-sprint show %s", c.ID, c.Holder, req.Row, c.ID)
	}
	if c.Gen != req.Gen {
		return store.Op{}, fmt.Errorf("SPRINT REFUSED report: consumer %s is at gen %d, expected %d: run nova-sprint show %s", c.ID, c.Gen, req.Gen, c.ID)
	}
	p := snap.Cards[c.Of]
	st := snap.Streams[c.Stream]
	if err := s.checkReportGates_internal(ctx, c, p, st, req); err != nil {
		return store.Op{}, err
	}
	plan, err := buildCombinedReportPlan_internal(snap, c, p, st, req)
	if err != nil {
		return store.Op{}, err
	}
	return store.Op{ID: req.ResultID, Epoch: snap.Epoch, Actor: req.Row, Kind: "report", At: s.now_internal(), Expect: plan.Expect, Writes: plan.Writes}, nil
}
func (s *StoreSprintClient) checkReportGates_internal(ctx context.Context, c, p store.Card, st store.Stream, req ReportRequest) error {
	switch {
	case c.Kind == "read" && req.Result == "fix" && strings.TrimSpace(req.Finding) == "":
		return fmt.Errorf("SPRINT REFUSED read: fix report on %s requires a finding: pass --finding <file:line and reason>", c.ID)
	case c.Kind != "work" || req.Result != "ok":
		return nil
	case len(req.Head) != 40 || strings.Trim(req.Head, "0123456789abcdef") != "":
		return fmt.Errorf("SPRINT REFUSED report: head %q is not a 40-hex commit sha: pass --head <40-hex>", req.Head)
	}
	if s.Origin != nil {
		ok, err := s.Origin.HasHead(ctx, st.Repo, c.Branch, req.Head)
		switch {
		case err != nil:
			return fmt.Errorf("SPRINT REFUSED report: origin check failed for %s: %v", c.Branch, err)
		case !ok:
			return fmt.Errorf("SPRINT REFUSED report: head %s is not on branch %s at origin: run git push origin %s", req.Head, c.Branch, c.Branch)
		}
	}
	return nil
}
func buildCombinedReportPlan_internal(snap store.Snapshot, c, p store.Card, st store.Stream, req ReportRequest) (card.Plan, error) {
	r := snap.Rows[req.Row]
	head := req.Head
	if c.Kind == "read" && head == "" {
		head = p.Head
	}
	reason := cmp.Or(req.Reason, req.Finding)
	cCard := card.FromStore(c)
	pCard := card.FromStore(p)
	cPlan, ref := card.MoveConsumer(cCard, pCard, nil, card.RowFromStore(r, 4, []string{"flash", "pro", "heavy", "frontier"}), card.Report{Row: req.Row, Result: req.Result, Head: head, Reason: reason, Cost: req.Cost, Priced: req.Priced})
	if ref != nil {
		return card.Plan{}, fmt.Errorf("SPRINT REFUSED report: %s: run nova-sprint show %s", ref.Reason, c.ID)
	}
	if c.Kind != "work" || p.State != card.Working || c.ID != fmt.Sprintf("%s.w%d", p.ID, p.Attempt) {
		return cPlan, nil
	}
	return mergeWorkProducerMove_internal(snap, cCard, pCard, st, req, cPlan)
}
func mergeWorkProducerMove_internal(snap store.Snapshot, cCard, pCard card.Card, st store.Stream, req ReportRequest, cPlan card.Plan) (card.Plan, error) {
	var cons []card.Card
	for _, sc := range snap.Cards {
		if sc.Of != pCard.ID {
			continue
		}
		cc := card.FromStore(sc)
		if cc.ID == cCard.ID {
			cc.State = card.Done
			cc.Result = req.Result
			cc.Head = cmp.Or(req.Head, cc.Head)
			cc.Reason = cmp.Or(req.Reason, cc.Reason)
		}
		cons = append(cons, cc)
	}
	var ev card.Event
	switch req.Result {
	case "ok":
		ev = card.Finished{Consumer: cCard.ID}
	case "fail":
		ev = card.Broken{Consumer: cCard.ID}
	case "fault":
		ev = card.Faulted{Consumer: cCard.ID}
	default:
		return cPlan, nil
	}
	pPlan, ref := card.Move(pCard, cons, nil, card.StreamFromStore(st), nil, ev)
	if ref != nil {
		return card.Plan{}, fmt.Errorf("SPRINT REFUSED report: %s: run nova-sprint show %s", ref.Reason, pCard.ID)
	}
	return combinePlans_internal(cPlan, pPlan), nil
}
func combinePlans_internal(a, b card.Plan) card.Plan {
	var out card.Plan
	out.Expect = append(out.Expect, a.Expect...)
	for _, e := range b.Expect {
		if !slices.Contains(out.Expect, e) {
			out.Expect = append(out.Expect, e)
		}
	}
	out.Writes = append(out.Writes, a.Writes...)
	out.Writes = append(out.Writes, b.Writes...)
	return out
}
func (s *StoreSprintClient) applyWithReplay_internal(ctx context.Context, op store.Op) (store.Receipt, error) {
	rec, err := s.Store.Apply(ctx, op)
	if err == nil {
		return rec, nil
	}
	if errors.Is(err, store.ErrUnknown) {
		if rep, found, rerr := s.Store.Replay(ctx, op.ID); rerr == nil && found {
			return rep, nil
		}
		if rec2, aerr := s.Store.Apply(ctx, op); aerr == nil {
			return rec2, nil
		}
	}
	return store.Receipt{}, fmt.Errorf("%w: %v", ErrStoreDown, err)
}

// Release is the release verb's withdraw: the consumer gives back its card
// with the reason "released: <why>", no result; the producer goes back with
// its bounces unchanged, at head when one is named.
func (s *StoreSprintClient) Release(ctx context.Context, row, consumer string, gen int, head, reason string) error {
	snap, err := s.Store.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreDown, err)
	}
	c, ok := snap.Cards[consumer]
	if !ok || c.Gen != gen || c.Holder != row {
		return fmt.Errorf("%w: %s is not held by %s at generation %d", ErrRefused, consumer, row, gen)
	}
	p := snap.Cards[c.Of]
	ev := card.Withdraw{Reason: "released: " + reason, Head: cmp.Or(head, c.Head), Stream: card.StreamFromStore(snap.Streams[p.Stream])}
	plan, why := card.MoveConsumer(card.FromStore(c), card.FromStore(p), nil, card.RowFromStore(snap.Rows[row], 0, nil), ev)
	if why != nil {
		return fmt.Errorf("%w: %s", ErrRefused, why.Reason)
	}
	op := store.Op{ID: fmt.Sprintf("release-%s-%d", consumer, gen), Epoch: snap.Epoch, Actor: row, Kind: "release", At: s.now_internal(), Expect: plan.Expect, Writes: plan.Writes}
	_, err = s.applyWithReplay_internal(ctx, op)
	return err
}
func (s *StoreSprintClient) Beat(ctx context.Context, row string, hb Heartbeat) error {
	snap, err := s.Store.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreDown, err)
	}
	op := store.Op{ID: fmt.Sprintf("beat-%s-%d", row, snap.Seq), Epoch: snap.Epoch, Actor: row, Kind: "beat", At: s.now_internal(),
		Writes: []store.Write{store.SetRowLoad{Row: row, Load1Centi: int(hb.Load1*100 + 0.5), Cores: hb.Cores, SlotsUsed: hb.SlotsUsed, SlotsTotal: hb.SlotsTotal, CPUPeakDeci: int(hb.CPUPeak*10 + 0.5), Tiers: hb.Tiers}}}
	_, err = s.applyWithReplay_internal(ctx, op)
	return err
}

func TestCLISprintClientCanonicalOpIDsAndFlags(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var calls [][]string
	cli := &CLISprintClient{
		Epoch: 3,
		Run: func(_ context.Context, argv []string) ([]byte, int, error) {
			calls = append(calls, slices.Clone(argv))
			if slices.Contains(argv, "take") {
				return []byte(`{"Consumer":"p1.w1","Epoch":3,"Gen":4,"model":"m1","deadline":900,"forge":"","commit_name":"Sprint Bot","commit_email":"1+sprint-bot@users.noreply.example.test"}`), 0, nil
			}
			opID := resultID_internal(3, "p1.w1", 4)
			return []byte(fmt.Sprintf(`{"accepted":true,"op":%q}`, opID)), 0, nil
		},
	}
	wantTakeID := store.ConsumerOpID("take", 3, "p1.w1", 4)
	if !strings.HasPrefix(wantTakeID, "consumer:") || len(wantTakeID) != len("consumer:")+64 {
		t.Fatalf("unexpected canonical take op id: %s", wantTakeID)
	}
	if _, err := cli.Take(ctx, "vision.ds", "p1.w1", 4); err != nil {
		t.Fatalf("Take: %v", err)
	}
	wantRepID := resultID_internal(3, "p1.w1", 4)
	rec, err := cli.Report(ctx, ReportRequest{
		Epoch: 3, Row: "vision.ds", Consumer: "p1.w1", Gen: 4,
		Result: "ok", Head: strings.Repeat("a", 40), Cost: 950, Priced: true, Usage: "input=10 output=2 model=deepseek/deepseek-flash",
	})
	if err != nil || !rec.Accepted || rec.Op != wantRepID {
		t.Fatalf("Report: rec=%+v err=%v", rec, err)
	}
	takeArgs := strings.Join(calls[0], " ")
	if !strings.Contains(takeArgs, "take p1.w1@4 --epoch 3 --as vision.ds --json") || strings.Contains(takeArgs, "--id") {
		t.Fatalf("unexpected take argv: %s", takeArgs)
	}
	repArgs := strings.Join(calls[1], " ")
	if !strings.Contains(repArgs, "--result ok --epoch 3 --as vision.ds --json") || !strings.Contains(repArgs, "--cost 0.00095") || strings.Contains(repArgs, "--id") {
		t.Fatalf("unexpected report argv: %s", repArgs)
	}
	if i := slices.Index(calls[1], "--usage"); i < 0 || calls[1][i+1] != "input=10 output=2 model=deepseek/deepseek-flash" {
		t.Fatalf("the run's tokens ride as one --usage argument: %q", calls[1])
	}
}

// TestCLISprintClientHookNamesWhatFailed: a hook the sprint refused for
// something other than the store (here a stream's origin) is never called
// "store unreachable"; the store's own failure, or a sprint that printed
// nothing, still is.
func TestCLISprintClientHookNamesWhatFailed(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		out       string
		storeDown bool
	}{
		"an origin":       {out: "SPRINT REFUSED hook: finish and read could not reach stream d from this process: git ls-remote of https://github.com/o/d.git failed (exit 128): give stream d --forge github, then hook again\n"},
		"the store":       {out: "SPRINT REFUSED hook: the store did not answer (connection refused): nova-sprint check\n", storeDown: true},
		"nothing printed": {storeDown: true},
	} {
		cli := &CLISprintClient{Run: func(context.Context, []string) ([]byte, int, error) { return []byte(tc.out), 2, nil }}
		err := cli.Hook(context.Background(), "amd-1")
		if err == nil || errors.Is(err, ErrStoreDown) != tc.storeDown || strings.Contains(err.Error(), "store unreachable") != tc.storeDown {
			t.Fatalf("%s: err = %v, want store down %v", name, err, tc.storeDown)
		}
		if !tc.storeDown && !strings.Contains(err.Error(), "could not reach stream d") {
			t.Fatalf("%s: err = %v lacks the sprint's line", name, err)
		}
	}
}

// TestCLISprintClientNextReadsEveryFinishedCard: `next --json` lists every
// finished card the row held this epoch, and on 2026-11-09 nine of twelve
// benches' answers passed edge's 64 KiB default cap: the capped bytes went to
// the parser ("parse failed at byte 32759"), no row took anything, and every
// dealt card timed out and bounced. The client reads the whole answer (here
// ~600 finished cards, well past the default cap, through edge's own fake,
// which caps as OSExec does), and an answer that still overran its cap is
// named as truncated, never parsed.
func TestCLISprintClientNextReadsEveryFinishedCard(t *testing.T) {
	t.Parallel()
	final := make([]ConsumerSummary, 600)
	for i := range final {
		final[i] = ConsumerSummary{ID: fmt.Sprintf("card-nc-conformance-%03d.w1", i), Producer: fmt.Sprintf("card-nc-conformance-%03d", i),
			Kind: "work", Stream: "netcode", State: "done", Priority: "normal", Head: strings.Repeat("a", 40), Gen: 1, Attempt: 1}
	}
	body, err := json.Marshal(NextResponse{Epoch: 7, Width: 16, Seq: 4242, Row: "mini-m5", State: "up", Final: final})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) <= 2*edge.DefaultMaxBytes {
		t.Fatalf("the answer is %d bytes, not past the %d-byte default cap", len(body), edge.DefaultMaxBytes)
	}
	fx := edge.NewFakeExec()
	fx.On("nova-sprint", func(edge.FakeCall) edge.FakeOutcome { return edge.FakeOutcome{Stdout: string(body)} })
	cli := &CLISprintClient{Exec: fx}
	resp, err := cli.Next(context.Background(), "mini-m5")
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if resp.Epoch != 7 || resp.Seq != 4242 || len(resp.Final) != 600 || resp.Final[599].ID != final[599].ID {
		t.Fatalf("Next read epoch %d seq %d with %d finished cards, want 7 4242 600", resp.Epoch, resp.Seq, len(resp.Final))
	}
	if calls := fx.Calls(); len(calls) != 1 || calls[0].Cmd.MaxBytes <= edge.DefaultMaxBytes {
		t.Fatalf("next --json ran with MaxBytes %d: the worker asks for more than edge's %d-byte default", calls[0].Cmd.MaxBytes, edge.DefaultMaxBytes)
	}
	_, err = (&CLISprintClient{Exec: truncatingExec_internal{out: string(body[:edge.DefaultMaxBytes])}}).Next(context.Background(), "mini-m5")
	if err == nil || !errors.Is(err, ErrTruncated) || !strings.Contains(err.Error(), "truncated") || !strings.Contains(err.Error(), "nova-sprint next --as mini-m5 --json") || strings.Contains(err.Error(), "parse failed") {
		t.Fatalf("a truncated answer is named, with its command, never parsed: %v", err)
	}
}

// truncatingExec_internal is an exec whose child's output overran its cap.
type truncatingExec_internal struct{ out string }

func (e truncatingExec_internal) Run(context.Context, edge.Cmd) (edge.Result, error) {
	return edge.Result{Stdout: e.out, Truncated: true}, nil
}
