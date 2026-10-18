package viewwire_test

// TestWhereViewGolden is the golden test of the view area's definitions
// (schemas/view/where.schema, schemas/view/tables.baseline and the generated
// pkg/viewwire): every table of the where --json object (the object of
// cmd/nova-sprint/reads.go whereView, the types it carries and the dashboard's
// reads of it) round-trips through pkg/schemaread in all three forms, the binary
// wire, padded standard base64 of it and the schema JSON text; a value past each
// declared bound (a string(N), a text list element, a map key at each level) and a
// damaged image of each form are refused by name, never clamped; a *string field, an
// unbounded text list element and an unbounded map key each take 70000 bytes, past
// every cap the first definitions chose, in all three forms; and generate.go pins the
// schema compiler that writes the area.
//
// Every table is a VARIABLE table (S1: the where object is a message an older
// reader meets) and travels in all three forms (S2).
//
// The JSON shape is proved against the current encoder: each sample is the bytes
// encoding/json writes for the real value (the types of internal/sprint,
// internal/sprint/store and internal/sprintdash, and a copy of the tags of the
// types cmd/nova-sprint keeps in package main), the table reads them, and its
// text holds every key and value of the sample. The text lists of the area
// (FirstTimeUnderTarget, Stalled, Blockers and the other []string fields) are
// compared byte for byte with what encoding/json writes for them.
//
// The test uses no clock, no socket, no network and no program: the times are
// fixed values, the wire is the generated writer's, and the baseline and the
// definitions are files of the repository.
//
// Rules cited: S1 (a variable table for a message an older reader may meet), S2
// (text transports carry padded standard base64 of the binary wire and the JSON
// text; a reader accepts each, refuses damage by name and counts reads per form),
// S5 (Go only), schema docs/SPEC-TABLES.md sections 2.8 (maps), 2.11 (timestamps),
// 2.13 (text lists), 16.4 (the json key) and 16.10 (the array root).

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/internal/sprintdash"
	wire "github.com/mas-bandwidth/nova-sprint/pkg/viewwire"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const viewHelp = "nova-sprint where --json"

// pin is the compiler generate.go names: the landing that carries text lists,
// array-root tables and unbounded text list elements and map keys on mas-bandwidth/schema.
const pin = "v2.5.1-0.20261017153557-54e0eda7d20b"

// variableTables is every table of the area, all variable.
var variableTables = []string{
	"ArchivedView", "CostCheck", "CostReconcileRecord", "CriticalCard", "DayGap", "DealtCard",
	"DecisionSummary", "FriendReport", "FriendRow", "GoalView", "HoldView", "JudgmentRef",
	"LandedSeries", "LaneRow", "MergeRow", "MergingCard", "NameList", "PrimaryRow",
	"ProviderGapReport", "ProviderProblem", "ProviderTier", "ProviderView", "ReadCardCounts",
	"ReadDay", "ReaderSpend", "ReadsWindow", "RouteGap", "SeatChange", "SeatWaits",
	"SeriesLastHour", "SeriesTotals", "ServerBeat", "StageStat", "StageTimes", "StatsResetView",
	"Stop", "StreamClock", "TierCosts", "WhereView",
}

// ---------------------------------------------------------------------------
// The types cmd/nova-sprint keeps in package main, with their tags as written
// there (reads.go, goal.go): the test cannot import a main package, so the tags
// are copied, and the key check below holds each copy to the schema's keys.

type goalView struct {
	Name  string    `json:"name"`
	Route string    `json:"route"`
	Last  time.Time `json:"last"`
	Count int       `json:"count"`
	State string    `json:"state"`
	Error string    `json:"error,omitempty"`
	Text  string    `json:"text,omitempty"`
}

type providerView struct {
	sprint.ProviderRow
	Billing string                `json:"billing,omitempty"`
	Funded  *bool                 `json:"funded,omitempty"`
	Problem string                `json:"problem,omitempty"`
	Tiers   []sprint.ProviderTier `json:"tiers,omitempty"`
}

type dealtCard struct {
	ID       string    `json:"id"`
	Primary  string    `json:"primary"`
	Stream   string    `json:"stream"`
	Member   string    `json:"member"`
	State    string    `json:"state"`
	Since    time.Time `json:"since,omitzero"`
	Deadline time.Time `json:"deadline,omitzero"`
	Branch   string    `json:"branch"`
	Priority string    `json:"priority,omitempty"`
	Tier     string    `json:"tier,omitempty"`
}

type mergingCard struct {
	ID      string `json:"id"`
	Stream  string `json:"stream"`
	Head    string `json:"head"`
	Attempt int    `json:"attempt,omitempty"`
}

type judgmentRef struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Card string `json:"card"`
}

type primaryRow struct {
	ID        string            `json:"id"`
	Stream    string            `json:"stream"`
	Column    string            `json:"column"`
	State     string            `json:"state"`
	Score     float64           `json:"score"`
	Fields    map[string]string `json:"fields"`
	TierShown string            `json:"tier_shown,omitempty"`
}

type archivedView struct {
	Streams []string `json:"streams"`
	Cards   int64    `json:"cards"`
	Landed  int64    `json:"landed"`
	Cost    string   `json:"cost"`
}

type statsResetView struct {
	At      time.Time        `json:"at"`
	By      string           `json:"by,omitempty"`
	Reason  string           `json:"reason"`
	Landed  int64            `json:"landed"`
	Streams map[string]int64 `json:"streams,omitempty"`
}

type seatWaitsView struct {
	sprint.SeatWaits
	OldestAgeSeconds int64     `json:"oldest_age_seconds"`
	At               time.Time `json:"at"`
}

type serverBeatView struct {
	Version string    `json:"version"`
	Commit  string    `json:"commit,omitempty"`
	Started time.Time `json:"started,omitempty"`
	At      time.Time `json:"at"`
}

// ---------------------------------------------------------------------------
// The samples: one instance of each table, as encoding/json writes the real value.

var (
	t0 = time.Date(2026, 10, 17, 5, 52, 21, 123456789, time.UTC)
	t1 = time.Date(2026, 10, 17, 5, 40, 0, 0, time.UTC)
	t2 = time.Date(2026, 10, 16, 23, 59, 59, 500000000, time.UTC)
)

func ptr[T any](v T) *T { return &v }

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func costReconcile() sprint.CostReconcileRecord {
	return sprint.CostReconcileRecord{Provider: "anthropic", At: "2026-10-17T05:00:00Z", Known: true, Note: "a provider count", Day: "2026-10-16",
		Used: 12.5, Internal: 11.25, InternalReads: 2.5, Gap: 1.25, Share: 0.1,
		Days: map[string]sprint.DayGap{"2026-10-15": {Provider: 3.5, Internal: 3.25}, "2026-10-16": {Provider: 9, Internal: 8}}}
}

func tierCosts() sprint.TierCosts {
	return sprint.TierCosts{
		Tiers:     map[string]int{"heavy": 2, "pro": 5},
		PerLanded: "$0.42", CostByTier: map[string]string{"heavy": "$1.00", "pro": "$2.50"},
		TotalCost: "$3.50", TotalUSD: "3.499871", LandedUSD: "3.1", WorkCost: "$2.00", ReadCost: "$1.25", DecideCost: "$0.25",
		DecideUnpriced: 1, DroppedCards: 2, CarriedCards: 1, ReadTokens: 12000,
		ReadsToday:   map[string]sprint.ReadDay{"subscription": {USD: "1.25", Tokens: 1200, Reads: 3}, "-": {Reads: 1}},
		UnpricedRuns: 2, ReadsNoTokens: 1,
		Readers:  map[string]sprint.ReaderSpend{"alpha": {Reads: 4, Priced: 3, USD: "0.75", HourPriced: 1, HourUSD: "0.25", Tokens: 900}},
		InFlight: "$0.10", Unreconciled: "$0.05", Reconciles: []sprint.CostReconcileRecord{costReconcile()},
	}
}

func stageStats() sprint.StageTimes {
	st := sprint.StageStat{Median: 12.5, P90: 40, N: 7}
	return sprint.StageTimes{All: map[string]sprint.StageStat{"work": st, "read": {Median: 3, P90: 9, N: 7}},
		Streams: map[string]map[string]sprint.StageStat{"v15-schema-tables": {"work": st}}}
}

func landedSeries() sprint.LandedSeries {
	return sprint.LandedSeries{Generated: "2026-10-17T05:52:21Z", GeneratedEpoch: 1792216341, BucketSeconds: 600, Start: 1792130000, Buckets: 3,
		Friends: []int{1, 0, 4}, Fleet: []int{0, 2, 1}, Totals: sprint.SeriesTotals{Friends: 5, Fleet: 3, Unknown: 1},
		LastHour: sprint.SeriesLastHour{Friends: 2, Fleet: 1}, Workers: map[string]int{"friend.alpha": 4, "m1": 3}}
}

func friendReport() sprint.FriendReport {
	return sprint.FriendReport{Running: []string{"card-a.r1.alpha", "card-b"}, Working: ptr(2), Queue: ptr(1), Width: ptr(4), StopReturns: ptr(0),
		Active: t1, Paced: ptr(3), Window: "5h 62% 7d 31%", Until: t0, Reason: "at the usage limit", Build: "v1.4.8", Started: t2, Present: t1}
}

func friendRow() store.FriendRow {
	r := friendReport()
	return store.FriendRow{Name: "alpha", Ready: 1, Working: 2, Held: 1, Stale: 1, DealtFleet: 1, Width: 4, OK: 12, Failed: 1, Redealt: 2,
		Status: "working", Class: "claude", Mode: "live", Session: "oneshot", Roles: "reader,worker", Load: 1.5, Report: &r, Active: t1,
		Tokens: 123456, Charged: "$1.25", Billing: "api", Beat: t0, Evidence: "beat 12s ago", Reason: "at the limit", Until: t0}
}

func mergeRow() sprintdash.MergeRow {
	return sprintdash.MergeRow{Merging: 2, Review: 3, LandedPer30m: 5, OldestMergingMin: ptr(7), BaseGate: "green", FailingTest: "TestSomething",
		BaseLacks: 1, DevLacks: 2, SyncMinutes: ptr(15), PromotionMinutes: ptr(240)}
}

func providerRow() providerView {
	return providerView{ProviderRow: sprint.ProviderRow{Name: "anthropic", Balance: "$12.00", BalanceAt: "2026-10-17T05:00:00Z", SpendHour: 1.5,
		State: "ok", Note: "balance not read", Limit: "4 concurrent"}, Billing: "api", Funded: ptr(true), Problem: "a problem",
		Tiers: []sprint.ProviderTier{{Tier: "pro", In: 2, Enabled: 1}}}
}

func gapReport() sprint.ProviderGapReport {
	return sprint.ProviderGapReport{InvalidProviders: []sprint.ProviderProblem{{Name: "acme", Problem: "no key"}},
		Routes: []sprint.RouteGap{{Route: "acme-pro", Provider: "acme", Tier: "pro"}}, Tiers: []string{"pro", "heavy"}}
}

func laneRow() sprint.LaneRow {
	return sprint.LaneRow{Kind: "work", Machine: "m1", Width: 4, Held: []string{"card-a"}, Waiting: []string{"card-b", "card-c"}, Since: t1}
}

func dealt() dealtCard {
	return dealtCard{ID: "card-a.w1", Primary: "card-a", Stream: "v15-schema-tables", Member: "friend.alpha", State: "working", Since: t1, Deadline: t0,
		Branch: "sprint/card-a.w1.g1.e21", Priority: "high", Tier: "pro"}
}

func primary() primaryRow {
	return primaryRow{ID: "card-a", Stream: "v15-schema-tables", Column: "working", State: "working", Score: 12.25,
		Fields: map[string]string{"tier": "pro", "needs": "card-b"}, TierShown: "pro"}
}

func stopRow() sprint.Stop {
	return sprint.Stop{Kind: "tier", Subject: "heavy", Since: t1, What: "the heavy tier has no live route", Effect: "no heavy card starts", Undo: "fund a route", Signal: "tier_blocked"}
}

func seatChange() sprint.SeatChange {
	return sprint.SeatChange{Holder: "coordinator", Generation: 4, From: "gamma", At: t1, By: "owner", Taken: true, ApprovedBy: "owner", Reason: "the seat returns"}
}

func holdView() sprint.HoldView {
	return sprint.HoldView{Kind: "stream", Name: "v15-schema-tables", Reason: "a red gate", By: "coordinator", At: "2026-10-17T05:40:00Z", Return: true,
		Cause: "the base is red", Who: "coordinator", Release: "nova-sprint release v15-schema-tables"}
}

// samples is the sample of every table: the text encoding/json writes for the
// real value.
func samples() map[string]string {
	costCheck := sprint.CostCheck{Streams: "3.50", Cards: "3.49", Gap: "0.01", OK: false, Orphans: []string{"card-a", "card-b.w1"}, Unlisted: 1}
	clock := sprint.StreamClock{Stream: "v15-schema-tables", Release: "v1.5", Display: "v15-schema-tables", State: "running", Since: t2, Progress: t1, Empty: true,
		Held: true, Reason: "a hold", HeldCards: 2, Quiet: t0, Promotion: "any", TierBlocked: "heavy"}
	decisions := sprint.DecisionSummary{Count: 3, Oldest: 90 * time.Second, DefaultsHour: 1, DeliveryFaults: 1, ReceiptUnknown: 2,
		ReceiptHistory: []string{"j12 delivered to friend.alpha", "j13 receipt unknown"}}
	goal := goalView{Name: "v15", Route: "claude", Last: t1, Count: 3, State: "waiting", Error: "push failed", Text: "push the stream"}
	waits := seatWaitsView{SeatWaits: sprint.SeatWaits{Judgments: 2, Overdue: 1, OldestID: "j12", OldestType: "card.stuck", OldestAt: t2, Level: 1}, OldestAgeSeconds: 300, At: t0}
	beat := serverBeatView{Version: "nova-sprint v1.4.8", Commit: "4d9941fbc7fd", Started: t2, At: t0}
	archived := archivedView{Streams: []string{"old-a", "old-b"}, Cards: 12, Landed: 10, Cost: "$4.20"}
	reset := statsResetView{At: t1, By: "coordinator", Reason: "a new epoch of cost", Landed: 7, Streams: map[string]int64{"v15-schema-tables": 5, "other": 2}}
	reads := sprint.ReadsWindowView{OK: 9, Broken: 1, Since: t1}
	cards := sprint.ReadCardCounts{Ready: 2, Working: 1, Done: 30}

	s := map[string]string{
		"NameList":            mustJSON([]string{"card-a", "card-b.r1.beta"}),
		"DecisionSummary":     mustJSON(decisions),
		"ReadDay":             mustJSON(sprint.ReadDay{USD: "1.25", Tokens: 1200, Reads: 3}),
		"ReaderSpend":         mustJSON(sprint.ReaderSpend{Reads: 4, Priced: 3, USD: "0.75", HourPriced: 1, HourUSD: "0.25", Tokens: 900}),
		"DayGap":              mustJSON(sprint.DayGap{Provider: 12.5, Internal: 11.25}),
		"CostReconcileRecord": mustJSON(costReconcile()),
		"TierCosts":           mustJSON(tierCosts()),
		"CostCheck":           mustJSON(costCheck),
		"StreamClock":         mustJSON(clock),
		"StageStat":           mustJSON(sprint.StageStat{Median: 12.5, P90: 40, N: 7}),
		"StageTimes":          mustJSON(stageStats()),
		"CriticalCard":        mustJSON(sprint.CriticalCard{ID: "card-a", Behind: 4, State: "working"}),
		"GoalView":            mustJSON(goal),
		"SeatChange":          mustJSON(seatChange()),
		"HoldView":            mustJSON(holdView()),
		"Stop":                mustJSON(stopRow()),
		"SeatWaits":           mustJSON(waits),
		"ServerBeat":          mustJSON(beat),
		"ProviderTier":        mustJSON(sprint.ProviderTier{Tier: "pro", In: 2, Enabled: 1}),
		"ProviderView":        mustJSON(providerRow()),
		"ProviderProblem":     mustJSON(sprint.ProviderProblem{Name: "acme", Problem: "no key"}),
		"RouteGap":            mustJSON(sprint.RouteGap{Route: "acme-pro", Provider: "acme", Tier: "pro"}),
		"ProviderGapReport":   mustJSON(gapReport()),
		"LaneRow":             mustJSON(laneRow()),
		"DealtCard":           mustJSON(dealt()),
		"MergingCard":         mustJSON(mergingCard{ID: "card-a", Stream: "v15-schema-tables", Head: "4d9941fbc7fd", Attempt: 2}),
		"JudgmentRef":         mustJSON(judgmentRef{ID: "j12", Kind: "judgment", Card: "card-a"}),
		"PrimaryRow":          mustJSON(primary()),
		"ArchivedView":        mustJSON(archived),
		"StatsResetView":      mustJSON(reset),
		"ReadsWindow":         mustJSON(reads),
		"ReadCardCounts":      mustJSON(cards),
		"MergeRow":            mustJSON(mergeRow()),
		"FriendReport":        mustJSON(friendReport()),
		"FriendRow":           mustJSON(friendRow()),
		"SeriesTotals":        mustJSON(sprint.SeriesTotals{Friends: 5, Fleet: 3, Unknown: 1}),
		"SeriesLastHour":      mustJSON(sprint.SeriesLastHour{Friends: 2, Fleet: 1}),
		"LandedSeries":        mustJSON(landedSeries()),
	}
	tables := map[string]map[string]map[string]string{
		"work":    {"v15-schema-tables": {"landed": "5", "status": "running", "cost": "$3.50", "per_landed": "$0.42"}, "other": {"landed": "0", "status": "held"}},
		"fleet":   {"m1": {"status": "up", "working": "2"}},
		"friends": {"alpha": {"status": "working", "ready": "1", "okpct": "92%"}},
	}
	s["WhereView"] = mustJSON(map[string]any{
		"decisions": decisions, "at": t0, "landed": 172, "all": 309, "held": 4,
		"summary":        "172/309 55.7% -> ETA 14h13m  machine: running",
		"archived_cards": 12, "archived_landed": 10, "done": true,
		"tables": tables, "tiers": map[string]int{"heavy": 2, "pro": 5}, "stream_costs": map[string]sprint.TierCosts{"v15-schema-tables": tierCosts()},
		"cost_check": costCheck, "first_time": "0.5/h (12 cards)", "first_time_target": 3, "first_time_window": 24,
		"first_time_under_target": []string{"v15-schema-tables", "other"}, "first_time_routes": map[string]string{"claude": "2/h"},
		"read_spend": "reads today: $1.25", "streams": []sprint.StreamClock{clock},
		"stage_times": stageStats(), "stalled": []string{"v15-schema-tables", "other"},
		"critical": []sprint.CriticalCard{{ID: "card-a", Behind: 4, State: "working"}},
		"blockers": []string{"card-a waits on card-b", "card-c waits on a sentinel"},
		"idle":     "idle: 2 cards wait on card-b", "coordinator": "coordinator", "pending": "op-12", "epoch": 21, "cleared": t2,
		"machine": "running", "server_version": "nova-sprint v1.4.8", "server": beat,
		"fleet_work": "on", "friends_work": "off",
		"goals": []goalView{goal}, "seat": seatChange(), "holds": []sprint.HoldView{holdView()}, "seat_waits": waits,
		"stops": []sprint.Stop{stopRow()}, "providers": []providerView{providerRow()}, "provider_gaps": gapReport(),
		"lanes": []sprint.LaneRow{laneRow()}, "cards": []dealtCard{dealt()},
		"merging":   []mergingCard{{ID: "card-a", Stream: "v15-schema-tables", Head: "4d9941fbc7fd", Attempt: 2}},
		"judgments": []judgmentRef{{ID: "j12", Kind: "judgment", Card: "card-a"}}, "rows": []primaryRow{primary()},
		"archived": archived, "stats_reset": reset, "ready": 6, "backup": "reads", "reads_waiting": 2,
		"reads_window": reads, "read_cards": cards, "fix": 1,
		"priorities":        map[string][]string{"high": {"card-a", "card-b"}, "low": {"card-c"}},
		"stream_priorities": map[string]string{"v15-schema-tables": "high"},
		"width":             8, "buffer": "6/16", "low": true, "merge_row": mergeRow(), "friends": []store.FriendRow{friendRow()},
		"releases": map[string]int64{"v1.5": 137, "v1.7": 12}, "store_rtt_p50_ms": 0.25, "store_rtt_p99_ms": 1.5,
		"landedSeries": landedSeries(),
	})
	return s
}

// textBound is the element bound of each text list, as the definitions declare it:
// the compiler's field info holds the count bound only (unbounded here), so the
// element bound is pinned here and held to the definitions' text below.
var textBound = map[string]int{
	"ArchivedView.streams":              128, // ValidID words (sprint.MaxIDLen)
	"CostCheck.orphans":                 515, // sprint.ValidCardID
	"LaneRow.held":                      257, // sprint.ValidLaneWho: 128 + 1 + 128
	"LaneRow.waiting":                   257,
	"NameList.names":                    515,
	"WhereView.first_time_under_target": 128,
	"WhereView.stalled":                 128,
}

// openBytes is the length of an open text in the checks below: past every cap the
// first definitions chose (1024 at most) and past 64 KiB, so a value of it proves
// that no bound is declared.
const openBytes = 70000

// ---------------------------------------------------------------------------
// The harness.

// report reduces the generated read report to what the strict read decides on.
func report(r wire.TableReport, ok bool) schemaread.Report {
	return schemaread.Report{
		Unknown:      int(r.Unknown),
		KindMismatch: int(r.KindMismatch),
		Clamped:      int(r.Clamped),
		Malformed:    r.Malformed || !ok,
		Reason:       r.Reason,
	}
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// refusalOf asserts err is a refusal by name and returns its reason. The label
// is the table's name, then what was read after a "." or a ":".
func refusalOf(t *testing.T, label string, err error) string {
	t.Helper()
	require.Error(t, err, "%s: refused", label)
	var r *schemaread.Refusal
	require.True(t, errors.As(err, &r), "%s: every refusal is a *schemaread.Refusal: %v", label, err)
	name := label
	if i := strings.IndexAny(name, ".:"); i >= 0 {
		name = name[:i]
	}
	assert.True(t, strings.HasPrefix(err.Error(), strings.ToUpper(name)+" REFUSED: "), "%s: %s", label, err.Error())
	assert.NotEmpty(t, r.Reason, "%s: a refusal names its reason", label)
	return r.Reason
}

// decode reads JSON text into a generic value, numbers kept as written.
func decode(t *testing.T, text string) any {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(text))
	d.UseNumber()
	var v any
	require.NoError(t, d.Decode(&v), text)
	return v
}

func encode(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// contains reports whether every key and value of want is in got: the sample's
// fields survive the round trip, whatever defaults the writer adds for a field the
// current encoder omits when empty.
func contains(want, got any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, wv := range w {
			gv, ok := g[k]
			if !ok || !contains(wv, gv) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !contains(w[i], g[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(want, got)
}

// withField is the sample with its top-level key set to v.
func withField(t *testing.T, sample, key string, v any) string {
	t.Helper()
	m := decode(t, sample).(map[string]any)
	m[key] = v
	return encode(t, m)
}

// stringFields is each top-level bounded string field of a table.
func stringFields(info *wire.TableTypeInfo) []wire.TableFieldInfo {
	var out []wire.TableFieldInfo
	for _, f := range info.Fields {
		if f.Kind == 12 && f.Counted && !f.IsArray && !f.Pointer && f.TextList == 0 && f.ArrayBound > 0 {
			out = append(out, f)
		}
	}
	return out
}

// textLists is each top-level text list of a table.
func textLists(info *wire.TableTypeInfo) []wire.TableFieldInfo {
	var out []wire.TableFieldInfo
	for _, f := range info.Fields {
		if f.TextList != 0 {
			out = append(out, f)
		}
	}
	return out
}

// openStrings is each top-level *string field of a table: text with no declared bound.
func openStrings(info *wire.TableTypeInfo) []wire.TableFieldInfo {
	var out []wire.TableFieldInfo
	for _, f := range info.Fields {
		if f.Pointer && f.DeclaredTypeName == "string" {
			out = append(out, f)
		}
	}
	return out
}

var mapKeyRE = regexp.MustCompile(`map\[string(?:\((\d+)\))?\]`)

// mapKeyLevels is the key bound of each level of a map field, outermost first, as
// the field's declared type spells them: 0 is an unbounded key (map[string]T).
func mapKeyLevels(t *testing.T, f wire.TableFieldInfo) []int {
	t.Helper()
	var out []int
	for _, m := range mapKeyRE.FindAllStringSubmatch(f.DeclaredTypeName, -1) {
		n := 0
		if m[1] != "" {
			var err error
			n, err = strconv.Atoi(m[1])
			require.NoError(t, err)
		}
		out = append(out, n)
	}
	return out
}

// firstValue is the value under the least key of a decoded JSON object.
func firstValue(t *testing.T, node any) any {
	t.Helper()
	m, ok := node.(map[string]any)
	require.True(t, ok && len(m) > 0, "the sample holds an entry at each level of its maps")
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return m[keys[0]]
}

// mapKeyDoc is the sample with an entry of key k added at level d (0 is the
// field's own) of the map field f, the entry a copy of the level's first.
func mapKeyDoc(t *testing.T, sample string, f wire.TableFieldInfo, d int, k string) string {
	t.Helper()
	m := decode(t, sample).(map[string]any)
	node := m[f.Json]
	for i := 0; i < d; i++ {
		node = firstValue(t, node)
	}
	obj := node.(map[string]any)
	obj[k] = decode(t, encode(t, firstValue(t, obj)))
	return encode(t, m)
}

// setOpenLeaf sets the first leaf of a map field of *string values, under levels
// maps, to text.
func setOpenLeaf(t *testing.T, node any, levels int, text string) {
	t.Helper()
	m := node.(map[string]any)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	require.NotEmpty(t, keys)
	if levels == 1 {
		m[keys[0]] = text
		return
	}
	setOpenLeaf(t, m[keys[0]], levels-1, text)
}

// pastBound is the wire image good holds with its string of n marker bytes grown
// to n+1, its length prefix rewritten: the image of a writer that ignored the
// bound. The table wire writes a string as kind 12, the LEB128 length and the
// bytes, so the image differs from good only there.
func pastBound(t *testing.T, good []byte, n int) []byte {
	t.Helper()
	enc := func(k int) []byte {
		b := binary.AppendUvarint([]byte{12}, uint64(k))
		return append(b, bytes.Repeat([]byte{'~'}, k)...)
	}
	at := enc(n)
	require.Equal(t, 1, bytes.Count(good, at), "the at-bound string is in the image once")
	return bytes.Replace(good, at, enc(n+1), 1)
}

// refuseDamage reads the damaged images of a good wire and asserts each is refused
// by name.
func refuseDamage[T any](t *testing.T, name string, r *schemaread.Reader[T], good []byte) {
	t.Helper()
	damaged := append([]byte(nil), good...)
	damaged[len(damaged)-1] ^= 0xff
	cases := map[string][]byte{
		"a damaged trailer":           damaged,
		"a damaged trailer in base64": []byte(b64(damaged)),
		"a truncated image":           good[:len(good)-1],
		"a truncated image in base64": []byte(b64(good[:len(good)-1])),
		"bad base64":                  []byte("!" + b64(good)[1:]),
		"an unknown form byte":        append([]byte{0x09}, good[1:]...),
	}
	for what, data := range cases {
		got, _, err := r.Read(data)
		refusalOf(t, name+": "+what, err)
		var zero T
		assert.True(t, reflect.DeepEqual(got, zero), "%s: %s: a refused read returns nothing", name, what)
	}
	_, _, err := r.Read(append([]byte{0x09}, good[1:]...))
	var ref *schemaread.Refusal
	require.True(t, errors.As(err, &ref))
	assert.Equal(t, schemaread.ReasonUnknownForm, ref.Reason, "%s: an unknown form byte is unknown_form", name)
}

// ---------------------------------------------------------------------------
// Variable tables: a document in a region, the binary wire, padded standard
// base64 of it and the JSON text.

type varOps[T any] struct {
	fromJSON    func([]byte, *wire.TableReport) (*T, *wire.TableArena, bool)
	measure     func(*T, ...wire.TableWriteContext) int64
	save        func(*T, []byte, ...wire.TableWriteContext) int64
	loadMeasure func([]byte) (int64, error)
	load        func(region, w []byte, rep *wire.TableReport) *T
	jsonMeasure func(*T, ...wire.TableAllocator) int64
	toJSON      func(*T, []byte, ...wire.TableAllocator) int64
	info        func() *wire.TableTypeInfo
	constraints func(**T, *schemaread.Checker)
}

// builder adapts a generated builder to varOps.fromJSON.
func builder[B any, T any](init func(*B, ...wire.TableAllocator) bool, root func(*B) *T, arena func(*B) *wire.TableArena, from func(*B, []byte, *wire.TableReport) bool) func([]byte, *wire.TableReport) (*T, *wire.TableArena, bool) {
	return func(text []byte, rep *wire.TableReport) (*T, *wire.TableArena, bool) {
		b := new(B)
		if !init(b) {
			return nil, nil, false
		}
		ok := from(b, text, rep)
		return root(b), arena(b), ok
	}
}

func (o varOps[T]) loadRegion(w []byte, rep *wire.TableReport) *T {
	n, err := o.loadMeasure(w)
	if err != nil {
		return nil
	}
	return o.load(make([]byte, n), w, rep)
}

// arrayRoot is whether the table's text is a top-level array (json = "array",
// SPEC-TABLES.md 16.10). schemaread detects the JSON text by its opening brace
// alone, so an array root's text is the codec's legacy form here.
func arrayRoot(info *wire.TableTypeInfo) bool { return info.JsonArray }

func (o varOps[T]) codec(name string, constrain bool) *schemaread.Codec[*T] {
	c := &schemaread.Codec[*T]{
		Name:  strings.ToUpper(name),
		Help:  viewHelp,
		Forms: []byte{1},
		Measure: func(w []byte) error {
			_, err := o.loadMeasure(w)
			return err
		},
		LoadWire: func(v **T, w []byte) schemaread.Report {
			var rep wire.TableReport
			out := o.loadRegion(w, &rep)
			*v = out
			return report(rep, out != nil)
		},
		SaveWire: func(v **T) []byte {
			m := o.measure(*v)
			if m < 0 {
				return nil
			}
			buf := make([]byte, m)
			n := o.save(*v, buf)
			if n < 0 {
				return nil
			}
			return buf[:n]
		},
		SaveJSON: func(v **T) []byte {
			buf := make([]byte, o.jsonMeasure(*v))
			return buf[:o.toJSON(*v, buf)]
		},
	}
	loadJSON := func(v **T, text []byte) schemaread.Report {
		var rep wire.TableReport
		root, arena, ok := o.fromJSON(text, &rep)
		if !ok || root == nil {
			return report(rep, false)
		}
		buf := make([]byte, o.measure(root, arena))
		n := o.save(root, buf, arena)
		if n < 0 {
			return report(rep, false)
		}
		var lrep wire.TableReport
		out := o.loadRegion(buf[:n], &lrep)
		*v = out
		return report(rep, out != nil)
	}
	if arrayRoot(o.info()) {
		c.Legacy = func(in []byte) bool { return len(bytes.TrimSpace(in)) > 0 && bytes.TrimSpace(in)[0] == '[' }
		c.LoadLegacy = func(v **T, in []byte) schemaread.Report { return loadJSON(v, bytes.TrimSpace(in)) }
	} else {
		c.LoadJSON = loadJSON
	}
	if constrain {
		c.Constraints = o.constraints
	}
	return c
}

// jsonForm is the form a read of the table's JSON text is counted under.
func jsonForm(info *wire.TableTypeInfo) schemaread.Form {
	if arrayRoot(info) {
		return schemaread.FormLegacy
	}
	return schemaread.FormJSON
}

// runVariable is the golden check of one variable table (S1, S2).
func runVariable[T any](t *testing.T, name, sample string, o varOps[T]) {
	info := o.info()
	require.True(t, info.Variable, "%s is a variable table", name)
	codec := o.codec(name, true)
	r := schemaread.NewReader(codec)
	form := jsonForm(info)

	v, res, err := r.Read([]byte(sample))
	require.NoError(t, err, "%s: the sample reads as JSON text", name)
	require.Equal(t, form, res.Form)
	canon := string(codec.SaveJSON(&v))
	require.True(t, contains(decode(t, sample), decode(t, canon)), "%s: every field of the sample survives: %s", name, canon)

	good, err := r.Write(&v, schemaread.FormBinary)
	require.NoError(t, err, "%s writes", name)
	require.NotEmpty(t, good)
	text, err := r.Write(&v, schemaread.FormBase64)
	require.NoError(t, err)
	assert.Equal(t, b64(good), string(text), "%s: base64 text is padded standard base64 of the wire", name)
	js, err := r.Write(&v, schemaread.FormJSON)
	require.NoError(t, err)

	// the three forms read back the one document, each counted once
	for _, data := range [][]byte{good, text, js} {
		got, res, err := r.Read(data)
		require.NoError(t, err, "%s", name)
		assert.Equal(t, canon, string(codec.SaveJSON(&got)), "%s reads the document back from %s", name, res.Form)
		again, err := r.Write(&got, schemaread.FormBinary)
		require.NoError(t, err)
		assert.Equal(t, good, again, "%s: the document read from %s writes the same wire", name, res.Form)
	}
	c := r.Counts()
	assert.EqualValues(t, 1, c.Reads(schemaread.FormBinary), name)
	assert.EqualValues(t, 1, c.Reads(schemaread.FormBase64), name)
	assert.EqualValues(t, 2, c.Reads(form), "%s: the sample and the written text", name)

	if !arrayRoot(info) {
		// each string past its bound is refused clamped in every form
		for _, f := range stringFields(info) {
			n := int(f.ArrayBound)
			atBound := withField(t, sample, f.Json, strings.Repeat("~", n))
			av, _, err := r.Read([]byte(atBound))
			require.NoError(t, err, "%s.%s at its bound", name, f.Name)
			img, err := r.Write(&av, schemaread.FormBinary)
			require.NoError(t, err)
			over := pastBound(t, img, n)
			for _, data := range [][]byte{over, []byte(b64(over)), []byte(withField(t, sample, f.Json, strings.Repeat("~", n+1)))} {
				got, _, err := r.Read(data)
				assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, name+"."+f.Name, err), "%s.%s past %d is refused clamped", name, f.Name, n)
				assert.Nil(t, got, "%s.%s: a refused read returns nothing", name, f.Name)
			}
		}
	}
	// each bounded text list element past its bound is refused clamped; an
	// unbounded element takes openBytes in the open-text check below
	for _, f := range textLists(info) {
		if arrayRoot(info) {
			break
		}
		n, bounded := textBound[name+"."+f.Name]
		if !bounded {
			continue
		}
		list, _ := decode(t, sample).(map[string]any)[f.Json].([]any)
		elems := append([]any(nil), list...)
		long := strings.Repeat("~", n+1)
		got, _, err := r.Read([]byte(withField(t, sample, f.Json, append(elems, long))))
		assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, name+"."+f.Name, err), "%s.%s: an element past the bound is refused clamped", name, f.Name)
		assert.Nil(t, got)
	}
	if arrayRoot(info) {
		elems := decode(t, sample).([]any)
		long := strings.Repeat("~", textBound["NameList.names"]+1)
		got, _, err := r.Read([]byte(encode(t, append(elems, long))))
		assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, name+".names", err), "%s: an element past the bound is refused clamped", name)
		assert.Nil(t, got)
		// an object is not the array root
		got, _, err = r.Read([]byte(`{"names":[]}`))
		refusalOf(t, name+": an object for an array root", err)
		assert.Nil(t, got)
	}

	if !arrayRoot(info) {
		// each bounded map key past its bound drops its entry and is refused
		// clamped, at every level of the map, in the JSON text; at the bound it is
		// kept. An unbounded key takes openBytes and survives all three forms.
		for _, f := range info.Fields {
			if !f.Map {
				continue
			}
			for d, n := range mapKeyLevels(t, f) {
				if n == 0 {
					long := strings.Repeat("k", openBytes)
					keyDoc := mapKeyDoc(t, sample, f, d, long)
					kv, _, err := r.Read([]byte(keyDoc))
					require.NoError(t, err, "%s.%s: an unbounded key of %d bytes at level %d reads", name, f.Name, openBytes, d)
					keyImg, err := r.Write(&kv, schemaread.FormBinary)
					require.NoError(t, err)
					keyB64, err := r.Write(&kv, schemaread.FormBase64)
					require.NoError(t, err)
					keyJSON, err := r.Write(&kv, schemaread.FormJSON)
					require.NoError(t, err)
					for _, data := range [][]byte{keyImg, keyB64, keyJSON} {
						back, res, err := r.Read(data)
						require.NoError(t, err, "%s.%s: the unbounded key reads from %s", name, f.Name, res.Form)
						canon := decode(t, string(codec.SaveJSON(&back)))
						node := canon.(map[string]any)[f.Json]
						for i := 0; i < d; i++ {
							node = firstValue(t, node)
						}
						_, ok := node.(map[string]any)[long]
						assert.True(t, ok, "%s.%s: the unbounded key survives %s at level %d", name, f.Name, res.Form, d)
					}
					continue
				}
				_, _, err := r.Read([]byte(mapKeyDoc(t, sample, f, d, strings.Repeat("k", n))))
				require.NoError(t, err, "%s.%s: a key of %d bytes at level %d is at its bound", name, f.Name, n, d)
				got, _, err := r.Read([]byte(mapKeyDoc(t, sample, f, d, strings.Repeat("k", n+1))))
				assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, name+"."+f.Name, err), "%s.%s: a key past %d at level %d is refused clamped", name, f.Name, n, d)
				assert.Nil(t, got)
			}
		}
		// each *string field, and a map's *string value, takes a text past every cap
		// the first definitions chose and past 64 KiB: no bound is declared, so none
		// is enforced, and the text survives the three forms
		// (a cell of a row is a field value of the nova-tables layer: held to 64 KiB by a
		// constraint, checked below, so the cells are left out here)
		m := decode(t, sample).(map[string]any)
		for _, f := range openStrings(info) {
			m[f.Json] = word(f.Name+":", openBytes)
		}
		for _, f := range textLists(info) {
			if _, bounded := textBound[name+"."+f.Name]; bounded {
				continue
			}
			m[f.Json] = append(m[f.Json].([]any), word(f.Name+":", openBytes))
		}
		for _, f := range info.Fields {
			if f.Map && f.Name != "tables" && !(name == "PrimaryRow" && f.Name == "fields") && strings.HasSuffix(f.DeclaredTypeName, "]*string") {
				setOpenLeaf(t, m[f.Json], len(mapKeyLevels(t, f)), word(f.Name+":", openBytes))
			}
		}
		openText := encode(t, m)
		ov, _, err := r.Read([]byte(openText))
		require.NoError(t, err, "%s: every open text of %d bytes is read", name, openBytes)
		openCanon := string(codec.SaveJSON(&ov))
		require.True(t, contains(decode(t, openText), decode(t, openCanon)), "%s: every open text survives", name)
		openImg, err := r.Write(&ov, schemaread.FormBinary)
		require.NoError(t, err)
		openB64, err := r.Write(&ov, schemaread.FormBase64)
		require.NoError(t, err)
		openJSON, err := r.Write(&ov, schemaread.FormJSON)
		require.NoError(t, err)
		for _, data := range [][]byte{openImg, openB64, openJSON} {
			back, res, err := r.Read(data)
			require.NoError(t, err, "%s: the open texts read from %s", name, r.Detect(data))
			assert.Equal(t, openCanon, string(codec.SaveJSON(&back)), "%s: the open texts read back from %s", name, res.Form)
		}
	}

	refuseDamage(t, name, r, good)
	got, _, err := r.Read([]byte(sample[:len(sample)/2]))
	assert.Equal(t, schemaread.ReasonMalformed, refusalOf(t, name+": truncated JSON", err))
	assert.Nil(t, got)
	assert.Positive(t, c.Refused(schemaread.ReasonClamped)+c.Refused(schemaread.ReasonMalformed), "%s: refusals are counted", name)
}

// ---------------------------------------------------------------------------
// The bounds a blob cannot declare: a cell is a field value of the nova-tables
// layer, at most ntable.LimitFieldValueBytes, and the reader holds it there.

func cellBound(field string, slot *wire.TableRef, c *schemaread.Checker) {
	s := wire.TableStringAt(slot)
	c.Text(field, string(s.Data[:s.Length]), ntable.LimitFieldValueBytes)
}

func primaryRowBound(r *wire.PrimaryRow, c *schemaread.Checker) {
	wire.PrimaryRowFieldsEach(&r.Fields, func(_ string, slot *wire.TableRef) bool {
		cellBound("fields", slot, c)
		return true
	})
}

func whereViewBounds(v **wire.WhereView, c *schemaread.Checker) {
	root := *v
	wire.WhereViewTablesEach(&root.Tables, func(_ string, rows *wire.TableMap[wire.WhereViewTablesEntryValueEntryRow]) bool {
		wire.WhereViewTablesEntryValueEach(rows, func(_ string, cells *wire.TableMap[wire.WhereViewTablesEntryValueEntryValueEntryRow]) bool {
			wire.WhereViewTablesEntryValueEntryValueEach(cells, func(_ string, slot *wire.TableRef) bool {
				cellBound("tables", slot, c)
				return true
			})
			return true
		})
		return true
	})
	for i := int32(0); i < root.Rows.Count; i++ {
		primaryRowBound(root.Rows.At(i), c)
	}
}

// variableCases is one case for every table of the area: each table is a root of its own.
func variableCases(samples map[string]string) map[string]func(t *testing.T) {
	return map[string]func(t *testing.T){
		"NameList": func(t *testing.T) {
			runVariable(t, "NameList", samples["NameList"], varOps[wire.NameList]{
				builder((*wire.NameListBuilder).Init, (*wire.NameListBuilder).GetRoot, func(b *wire.NameListBuilder) *wire.TableArena { return &b.Arena }, wire.NameListFromJson),
				wire.NameListMeasure, wire.NameListSave, wire.NameListLoadMeasureReason, wire.NameListLoad, wire.NameListToJsonMeasure, wire.NameListToJson, wire.NameListTableType, nil})
		},
		"DecisionSummary": func(t *testing.T) {
			runVariable(t, "DecisionSummary", samples["DecisionSummary"], varOps[wire.DecisionSummary]{
				builder((*wire.DecisionSummaryBuilder).Init, (*wire.DecisionSummaryBuilder).GetRoot, func(b *wire.DecisionSummaryBuilder) *wire.TableArena { return &b.Arena }, wire.DecisionSummaryFromJson),
				wire.DecisionSummaryMeasure, wire.DecisionSummarySave, wire.DecisionSummaryLoadMeasureReason, wire.DecisionSummaryLoad, wire.DecisionSummaryToJsonMeasure, wire.DecisionSummaryToJson, wire.DecisionSummaryTableType, nil})
		},
		"ReadDay": func(t *testing.T) {
			runVariable(t, "ReadDay", samples["ReadDay"], varOps[wire.ReadDay]{
				builder((*wire.ReadDayBuilder).Init, (*wire.ReadDayBuilder).GetRoot, func(b *wire.ReadDayBuilder) *wire.TableArena { return &b.Arena }, wire.ReadDayFromJson),
				wire.ReadDayMeasure, wire.ReadDaySave, wire.ReadDayLoadMeasureReason, wire.ReadDayLoad, wire.ReadDayToJsonMeasure, wire.ReadDayToJson, wire.ReadDayTableType, nil})
		},
		"ReaderSpend": func(t *testing.T) {
			runVariable(t, "ReaderSpend", samples["ReaderSpend"], varOps[wire.ReaderSpend]{
				builder((*wire.ReaderSpendBuilder).Init, (*wire.ReaderSpendBuilder).GetRoot, func(b *wire.ReaderSpendBuilder) *wire.TableArena { return &b.Arena }, wire.ReaderSpendFromJson),
				wire.ReaderSpendMeasure, wire.ReaderSpendSave, wire.ReaderSpendLoadMeasureReason, wire.ReaderSpendLoad, wire.ReaderSpendToJsonMeasure, wire.ReaderSpendToJson, wire.ReaderSpendTableType, nil})
		},
		"DayGap": func(t *testing.T) {
			runVariable(t, "DayGap", samples["DayGap"], varOps[wire.DayGap]{
				builder((*wire.DayGapBuilder).Init, (*wire.DayGapBuilder).GetRoot, func(b *wire.DayGapBuilder) *wire.TableArena { return &b.Arena }, wire.DayGapFromJson),
				wire.DayGapMeasure, wire.DayGapSave, wire.DayGapLoadMeasureReason, wire.DayGapLoad, wire.DayGapToJsonMeasure, wire.DayGapToJson, wire.DayGapTableType, nil})
		},
		"CostReconcileRecord": func(t *testing.T) {
			runVariable(t, "CostReconcileRecord", samples["CostReconcileRecord"], varOps[wire.CostReconcileRecord]{
				builder((*wire.CostReconcileRecordBuilder).Init, (*wire.CostReconcileRecordBuilder).GetRoot, func(b *wire.CostReconcileRecordBuilder) *wire.TableArena { return &b.Arena }, wire.CostReconcileRecordFromJson),
				wire.CostReconcileRecordMeasure, wire.CostReconcileRecordSave, wire.CostReconcileRecordLoadMeasureReason, wire.CostReconcileRecordLoad, wire.CostReconcileRecordToJsonMeasure, wire.CostReconcileRecordToJson, wire.CostReconcileRecordTableType, nil})
		},
		"TierCosts": func(t *testing.T) {
			runVariable(t, "TierCosts", samples["TierCosts"], varOps[wire.TierCosts]{
				builder((*wire.TierCostsBuilder).Init, (*wire.TierCostsBuilder).GetRoot, func(b *wire.TierCostsBuilder) *wire.TableArena { return &b.Arena }, wire.TierCostsFromJson),
				wire.TierCostsMeasure, wire.TierCostsSave, wire.TierCostsLoadMeasureReason, wire.TierCostsLoad, wire.TierCostsToJsonMeasure, wire.TierCostsToJson, wire.TierCostsTableType, nil})
		},
		"CostCheck": func(t *testing.T) {
			runVariable(t, "CostCheck", samples["CostCheck"], varOps[wire.CostCheck]{
				builder((*wire.CostCheckBuilder).Init, (*wire.CostCheckBuilder).GetRoot, func(b *wire.CostCheckBuilder) *wire.TableArena { return &b.Arena }, wire.CostCheckFromJson),
				wire.CostCheckMeasure, wire.CostCheckSave, wire.CostCheckLoadMeasureReason, wire.CostCheckLoad, wire.CostCheckToJsonMeasure, wire.CostCheckToJson, wire.CostCheckTableType, nil})
		},
		"StreamClock": func(t *testing.T) {
			runVariable(t, "StreamClock", samples["StreamClock"], varOps[wire.StreamClock]{
				builder((*wire.StreamClockBuilder).Init, (*wire.StreamClockBuilder).GetRoot, func(b *wire.StreamClockBuilder) *wire.TableArena { return &b.Arena }, wire.StreamClockFromJson),
				wire.StreamClockMeasure, wire.StreamClockSave, wire.StreamClockLoadMeasureReason, wire.StreamClockLoad, wire.StreamClockToJsonMeasure, wire.StreamClockToJson, wire.StreamClockTableType, nil})
		},
		"StageStat": func(t *testing.T) {
			runVariable(t, "StageStat", samples["StageStat"], varOps[wire.StageStat]{
				builder((*wire.StageStatBuilder).Init, (*wire.StageStatBuilder).GetRoot, func(b *wire.StageStatBuilder) *wire.TableArena { return &b.Arena }, wire.StageStatFromJson),
				wire.StageStatMeasure, wire.StageStatSave, wire.StageStatLoadMeasureReason, wire.StageStatLoad, wire.StageStatToJsonMeasure, wire.StageStatToJson, wire.StageStatTableType, nil})
		},
		"StageTimes": func(t *testing.T) {
			runVariable(t, "StageTimes", samples["StageTimes"], varOps[wire.StageTimes]{
				builder((*wire.StageTimesBuilder).Init, (*wire.StageTimesBuilder).GetRoot, func(b *wire.StageTimesBuilder) *wire.TableArena { return &b.Arena }, wire.StageTimesFromJson),
				wire.StageTimesMeasure, wire.StageTimesSave, wire.StageTimesLoadMeasureReason, wire.StageTimesLoad, wire.StageTimesToJsonMeasure, wire.StageTimesToJson, wire.StageTimesTableType, nil})
		},
		"CriticalCard": func(t *testing.T) {
			runVariable(t, "CriticalCard", samples["CriticalCard"], varOps[wire.CriticalCard]{
				builder((*wire.CriticalCardBuilder).Init, (*wire.CriticalCardBuilder).GetRoot, func(b *wire.CriticalCardBuilder) *wire.TableArena { return &b.Arena }, wire.CriticalCardFromJson),
				wire.CriticalCardMeasure, wire.CriticalCardSave, wire.CriticalCardLoadMeasureReason, wire.CriticalCardLoad, wire.CriticalCardToJsonMeasure, wire.CriticalCardToJson, wire.CriticalCardTableType, nil})
		},
		"GoalView": func(t *testing.T) {
			runVariable(t, "GoalView", samples["GoalView"], varOps[wire.GoalView]{
				builder((*wire.GoalViewBuilder).Init, (*wire.GoalViewBuilder).GetRoot, func(b *wire.GoalViewBuilder) *wire.TableArena { return &b.Arena }, wire.GoalViewFromJson),
				wire.GoalViewMeasure, wire.GoalViewSave, wire.GoalViewLoadMeasureReason, wire.GoalViewLoad, wire.GoalViewToJsonMeasure, wire.GoalViewToJson, wire.GoalViewTableType, nil})
		},
		"SeatChange": func(t *testing.T) {
			runVariable(t, "SeatChange", samples["SeatChange"], varOps[wire.SeatChange]{
				builder((*wire.SeatChangeBuilder).Init, (*wire.SeatChangeBuilder).GetRoot, func(b *wire.SeatChangeBuilder) *wire.TableArena { return &b.Arena }, wire.SeatChangeFromJson),
				wire.SeatChangeMeasure, wire.SeatChangeSave, wire.SeatChangeLoadMeasureReason, wire.SeatChangeLoad, wire.SeatChangeToJsonMeasure, wire.SeatChangeToJson, wire.SeatChangeTableType, nil})
		},
		"HoldView": func(t *testing.T) {
			runVariable(t, "HoldView", samples["HoldView"], varOps[wire.HoldView]{
				builder((*wire.HoldViewBuilder).Init, (*wire.HoldViewBuilder).GetRoot, func(b *wire.HoldViewBuilder) *wire.TableArena { return &b.Arena }, wire.HoldViewFromJson),
				wire.HoldViewMeasure, wire.HoldViewSave, wire.HoldViewLoadMeasureReason, wire.HoldViewLoad, wire.HoldViewToJsonMeasure, wire.HoldViewToJson, wire.HoldViewTableType, nil})
		},
		"Stop": func(t *testing.T) {
			runVariable(t, "Stop", samples["Stop"], varOps[wire.Stop]{
				builder((*wire.StopBuilder).Init, (*wire.StopBuilder).GetRoot, func(b *wire.StopBuilder) *wire.TableArena { return &b.Arena }, wire.StopFromJson),
				wire.StopMeasure, wire.StopSave, wire.StopLoadMeasureReason, wire.StopLoad, wire.StopToJsonMeasure, wire.StopToJson, wire.StopTableType, nil})
		},
		"SeatWaits": func(t *testing.T) {
			runVariable(t, "SeatWaits", samples["SeatWaits"], varOps[wire.SeatWaits]{
				builder((*wire.SeatWaitsBuilder).Init, (*wire.SeatWaitsBuilder).GetRoot, func(b *wire.SeatWaitsBuilder) *wire.TableArena { return &b.Arena }, wire.SeatWaitsFromJson),
				wire.SeatWaitsMeasure, wire.SeatWaitsSave, wire.SeatWaitsLoadMeasureReason, wire.SeatWaitsLoad, wire.SeatWaitsToJsonMeasure, wire.SeatWaitsToJson, wire.SeatWaitsTableType, nil})
		},
		"ServerBeat": func(t *testing.T) {
			runVariable(t, "ServerBeat", samples["ServerBeat"], varOps[wire.ServerBeat]{
				builder((*wire.ServerBeatBuilder).Init, (*wire.ServerBeatBuilder).GetRoot, func(b *wire.ServerBeatBuilder) *wire.TableArena { return &b.Arena }, wire.ServerBeatFromJson),
				wire.ServerBeatMeasure, wire.ServerBeatSave, wire.ServerBeatLoadMeasureReason, wire.ServerBeatLoad, wire.ServerBeatToJsonMeasure, wire.ServerBeatToJson, wire.ServerBeatTableType, nil})
		},
		"ProviderTier": func(t *testing.T) {
			runVariable(t, "ProviderTier", samples["ProviderTier"], varOps[wire.ProviderTier]{
				builder((*wire.ProviderTierBuilder).Init, (*wire.ProviderTierBuilder).GetRoot, func(b *wire.ProviderTierBuilder) *wire.TableArena { return &b.Arena }, wire.ProviderTierFromJson),
				wire.ProviderTierMeasure, wire.ProviderTierSave, wire.ProviderTierLoadMeasureReason, wire.ProviderTierLoad, wire.ProviderTierToJsonMeasure, wire.ProviderTierToJson, wire.ProviderTierTableType, nil})
		},
		"ProviderView": func(t *testing.T) {
			runVariable(t, "ProviderView", samples["ProviderView"], varOps[wire.ProviderView]{
				builder((*wire.ProviderViewBuilder).Init, (*wire.ProviderViewBuilder).GetRoot, func(b *wire.ProviderViewBuilder) *wire.TableArena { return &b.Arena }, wire.ProviderViewFromJson),
				wire.ProviderViewMeasure, wire.ProviderViewSave, wire.ProviderViewLoadMeasureReason, wire.ProviderViewLoad, wire.ProviderViewToJsonMeasure, wire.ProviderViewToJson, wire.ProviderViewTableType, nil})
		},
		"ProviderProblem": func(t *testing.T) {
			runVariable(t, "ProviderProblem", samples["ProviderProblem"], varOps[wire.ProviderProblem]{
				builder((*wire.ProviderProblemBuilder).Init, (*wire.ProviderProblemBuilder).GetRoot, func(b *wire.ProviderProblemBuilder) *wire.TableArena { return &b.Arena }, wire.ProviderProblemFromJson),
				wire.ProviderProblemMeasure, wire.ProviderProblemSave, wire.ProviderProblemLoadMeasureReason, wire.ProviderProblemLoad, wire.ProviderProblemToJsonMeasure, wire.ProviderProblemToJson, wire.ProviderProblemTableType, nil})
		},
		"RouteGap": func(t *testing.T) {
			runVariable(t, "RouteGap", samples["RouteGap"], varOps[wire.RouteGap]{
				builder((*wire.RouteGapBuilder).Init, (*wire.RouteGapBuilder).GetRoot, func(b *wire.RouteGapBuilder) *wire.TableArena { return &b.Arena }, wire.RouteGapFromJson),
				wire.RouteGapMeasure, wire.RouteGapSave, wire.RouteGapLoadMeasureReason, wire.RouteGapLoad, wire.RouteGapToJsonMeasure, wire.RouteGapToJson, wire.RouteGapTableType, nil})
		},
		"ProviderGapReport": func(t *testing.T) {
			runVariable(t, "ProviderGapReport", samples["ProviderGapReport"], varOps[wire.ProviderGapReport]{
				builder((*wire.ProviderGapReportBuilder).Init, (*wire.ProviderGapReportBuilder).GetRoot, func(b *wire.ProviderGapReportBuilder) *wire.TableArena { return &b.Arena }, wire.ProviderGapReportFromJson),
				wire.ProviderGapReportMeasure, wire.ProviderGapReportSave, wire.ProviderGapReportLoadMeasureReason, wire.ProviderGapReportLoad, wire.ProviderGapReportToJsonMeasure, wire.ProviderGapReportToJson, wire.ProviderGapReportTableType, nil})
		},
		"LaneRow": func(t *testing.T) {
			runVariable(t, "LaneRow", samples["LaneRow"], varOps[wire.LaneRow]{
				builder((*wire.LaneRowBuilder).Init, (*wire.LaneRowBuilder).GetRoot, func(b *wire.LaneRowBuilder) *wire.TableArena { return &b.Arena }, wire.LaneRowFromJson),
				wire.LaneRowMeasure, wire.LaneRowSave, wire.LaneRowLoadMeasureReason, wire.LaneRowLoad, wire.LaneRowToJsonMeasure, wire.LaneRowToJson, wire.LaneRowTableType, nil})
		},
		"DealtCard": func(t *testing.T) {
			runVariable(t, "DealtCard", samples["DealtCard"], varOps[wire.DealtCard]{
				builder((*wire.DealtCardBuilder).Init, (*wire.DealtCardBuilder).GetRoot, func(b *wire.DealtCardBuilder) *wire.TableArena { return &b.Arena }, wire.DealtCardFromJson),
				wire.DealtCardMeasure, wire.DealtCardSave, wire.DealtCardLoadMeasureReason, wire.DealtCardLoad, wire.DealtCardToJsonMeasure, wire.DealtCardToJson, wire.DealtCardTableType, nil})
		},
		"MergingCard": func(t *testing.T) {
			runVariable(t, "MergingCard", samples["MergingCard"], varOps[wire.MergingCard]{
				builder((*wire.MergingCardBuilder).Init, (*wire.MergingCardBuilder).GetRoot, func(b *wire.MergingCardBuilder) *wire.TableArena { return &b.Arena }, wire.MergingCardFromJson),
				wire.MergingCardMeasure, wire.MergingCardSave, wire.MergingCardLoadMeasureReason, wire.MergingCardLoad, wire.MergingCardToJsonMeasure, wire.MergingCardToJson, wire.MergingCardTableType, nil})
		},
		"JudgmentRef": func(t *testing.T) {
			runVariable(t, "JudgmentRef", samples["JudgmentRef"], varOps[wire.JudgmentRef]{
				builder((*wire.JudgmentRefBuilder).Init, (*wire.JudgmentRefBuilder).GetRoot, func(b *wire.JudgmentRefBuilder) *wire.TableArena { return &b.Arena }, wire.JudgmentRefFromJson),
				wire.JudgmentRefMeasure, wire.JudgmentRefSave, wire.JudgmentRefLoadMeasureReason, wire.JudgmentRefLoad, wire.JudgmentRefToJsonMeasure, wire.JudgmentRefToJson, wire.JudgmentRefTableType, nil})
		},
		"PrimaryRow": func(t *testing.T) {
			runVariable(t, "PrimaryRow", samples["PrimaryRow"], varOps[wire.PrimaryRow]{
				builder((*wire.PrimaryRowBuilder).Init, (*wire.PrimaryRowBuilder).GetRoot, func(b *wire.PrimaryRowBuilder) *wire.TableArena { return &b.Arena }, wire.PrimaryRowFromJson),
				wire.PrimaryRowMeasure, wire.PrimaryRowSave, wire.PrimaryRowLoadMeasureReason, wire.PrimaryRowLoad, wire.PrimaryRowToJsonMeasure, wire.PrimaryRowToJson, wire.PrimaryRowTableType, func(v **wire.PrimaryRow, c *schemaread.Checker) { primaryRowBound(*v, c) }})
		},
		"ArchivedView": func(t *testing.T) {
			runVariable(t, "ArchivedView", samples["ArchivedView"], varOps[wire.ArchivedView]{
				builder((*wire.ArchivedViewBuilder).Init, (*wire.ArchivedViewBuilder).GetRoot, func(b *wire.ArchivedViewBuilder) *wire.TableArena { return &b.Arena }, wire.ArchivedViewFromJson),
				wire.ArchivedViewMeasure, wire.ArchivedViewSave, wire.ArchivedViewLoadMeasureReason, wire.ArchivedViewLoad, wire.ArchivedViewToJsonMeasure, wire.ArchivedViewToJson, wire.ArchivedViewTableType, nil})
		},
		"StatsResetView": func(t *testing.T) {
			runVariable(t, "StatsResetView", samples["StatsResetView"], varOps[wire.StatsResetView]{
				builder((*wire.StatsResetViewBuilder).Init, (*wire.StatsResetViewBuilder).GetRoot, func(b *wire.StatsResetViewBuilder) *wire.TableArena { return &b.Arena }, wire.StatsResetViewFromJson),
				wire.StatsResetViewMeasure, wire.StatsResetViewSave, wire.StatsResetViewLoadMeasureReason, wire.StatsResetViewLoad, wire.StatsResetViewToJsonMeasure, wire.StatsResetViewToJson, wire.StatsResetViewTableType, nil})
		},
		"ReadsWindow": func(t *testing.T) {
			runVariable(t, "ReadsWindow", samples["ReadsWindow"], varOps[wire.ReadsWindow]{
				builder((*wire.ReadsWindowBuilder).Init, (*wire.ReadsWindowBuilder).GetRoot, func(b *wire.ReadsWindowBuilder) *wire.TableArena { return &b.Arena }, wire.ReadsWindowFromJson),
				wire.ReadsWindowMeasure, wire.ReadsWindowSave, wire.ReadsWindowLoadMeasureReason, wire.ReadsWindowLoad, wire.ReadsWindowToJsonMeasure, wire.ReadsWindowToJson, wire.ReadsWindowTableType, nil})
		},
		"ReadCardCounts": func(t *testing.T) {
			runVariable(t, "ReadCardCounts", samples["ReadCardCounts"], varOps[wire.ReadCardCounts]{
				builder((*wire.ReadCardCountsBuilder).Init, (*wire.ReadCardCountsBuilder).GetRoot, func(b *wire.ReadCardCountsBuilder) *wire.TableArena { return &b.Arena }, wire.ReadCardCountsFromJson),
				wire.ReadCardCountsMeasure, wire.ReadCardCountsSave, wire.ReadCardCountsLoadMeasureReason, wire.ReadCardCountsLoad, wire.ReadCardCountsToJsonMeasure, wire.ReadCardCountsToJson, wire.ReadCardCountsTableType, nil})
		},
		"MergeRow": func(t *testing.T) {
			runVariable(t, "MergeRow", samples["MergeRow"], varOps[wire.MergeRow]{
				builder((*wire.MergeRowBuilder).Init, (*wire.MergeRowBuilder).GetRoot, func(b *wire.MergeRowBuilder) *wire.TableArena { return &b.Arena }, wire.MergeRowFromJson),
				wire.MergeRowMeasure, wire.MergeRowSave, wire.MergeRowLoadMeasureReason, wire.MergeRowLoad, wire.MergeRowToJsonMeasure, wire.MergeRowToJson, wire.MergeRowTableType, nil})
		},
		"FriendReport": func(t *testing.T) {
			runVariable(t, "FriendReport", samples["FriendReport"], varOps[wire.FriendReport]{
				builder((*wire.FriendReportBuilder).Init, (*wire.FriendReportBuilder).GetRoot, func(b *wire.FriendReportBuilder) *wire.TableArena { return &b.Arena }, wire.FriendReportFromJson),
				wire.FriendReportMeasure, wire.FriendReportSave, wire.FriendReportLoadMeasureReason, wire.FriendReportLoad, wire.FriendReportToJsonMeasure, wire.FriendReportToJson, wire.FriendReportTableType, nil})
		},
		"FriendRow": func(t *testing.T) {
			runVariable(t, "FriendRow", samples["FriendRow"], varOps[wire.FriendRow]{
				builder((*wire.FriendRowBuilder).Init, (*wire.FriendRowBuilder).GetRoot, func(b *wire.FriendRowBuilder) *wire.TableArena { return &b.Arena }, wire.FriendRowFromJson),
				wire.FriendRowMeasure, wire.FriendRowSave, wire.FriendRowLoadMeasureReason, wire.FriendRowLoad, wire.FriendRowToJsonMeasure, wire.FriendRowToJson, wire.FriendRowTableType, nil})
		},
		"SeriesTotals": func(t *testing.T) {
			runVariable(t, "SeriesTotals", samples["SeriesTotals"], varOps[wire.SeriesTotals]{
				builder((*wire.SeriesTotalsBuilder).Init, (*wire.SeriesTotalsBuilder).GetRoot, func(b *wire.SeriesTotalsBuilder) *wire.TableArena { return &b.Arena }, wire.SeriesTotalsFromJson),
				wire.SeriesTotalsMeasure, wire.SeriesTotalsSave, wire.SeriesTotalsLoadMeasureReason, wire.SeriesTotalsLoad, wire.SeriesTotalsToJsonMeasure, wire.SeriesTotalsToJson, wire.SeriesTotalsTableType, nil})
		},
		"SeriesLastHour": func(t *testing.T) {
			runVariable(t, "SeriesLastHour", samples["SeriesLastHour"], varOps[wire.SeriesLastHour]{
				builder((*wire.SeriesLastHourBuilder).Init, (*wire.SeriesLastHourBuilder).GetRoot, func(b *wire.SeriesLastHourBuilder) *wire.TableArena { return &b.Arena }, wire.SeriesLastHourFromJson),
				wire.SeriesLastHourMeasure, wire.SeriesLastHourSave, wire.SeriesLastHourLoadMeasureReason, wire.SeriesLastHourLoad, wire.SeriesLastHourToJsonMeasure, wire.SeriesLastHourToJson, wire.SeriesLastHourTableType, nil})
		},
		"LandedSeries": func(t *testing.T) {
			runVariable(t, "LandedSeries", samples["LandedSeries"], varOps[wire.LandedSeries]{
				builder((*wire.LandedSeriesBuilder).Init, (*wire.LandedSeriesBuilder).GetRoot, func(b *wire.LandedSeriesBuilder) *wire.TableArena { return &b.Arena }, wire.LandedSeriesFromJson),
				wire.LandedSeriesMeasure, wire.LandedSeriesSave, wire.LandedSeriesLoadMeasureReason, wire.LandedSeriesLoad, wire.LandedSeriesToJsonMeasure, wire.LandedSeriesToJson, wire.LandedSeriesTableType, nil})
		},
		"WhereView": func(t *testing.T) {
			runVariable(t, "WhereView", samples["WhereView"], varOps[wire.WhereView]{
				builder((*wire.WhereViewBuilder).Init, (*wire.WhereViewBuilder).GetRoot, func(b *wire.WhereViewBuilder) *wire.TableArena { return &b.Arena }, wire.WhereViewFromJson),
				wire.WhereViewMeasure, wire.WhereViewSave, wire.WhereViewLoadMeasureReason, wire.WhereViewLoad, wire.WhereViewToJsonMeasure, wire.WhereViewToJson, wire.WhereViewTableType, whereViewBounds})
		},
	}
}

// ---------------------------------------------------------------------------
// The shape of the text against the current encoder.

// keysOf is the JSON keys a table's text carries.
func keysOf(info *wire.TableTypeInfo) map[string]bool {
	out := map[string]bool{}
	for _, f := range info.Fields {
		out[f.Json] = true
	}
	return out
}

// rawAt is the compact bytes of the value at the key path of a JSON object.
func rawAt(t *testing.T, doc string, path ...string) string {
	t.Helper()
	cur := json.RawMessage(doc)
	for _, k := range path {
		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(cur, &m), "%s of %.80s", k, string(cur))
		next, ok := m[k]
		require.True(t, ok, "%s is in %.80s", k, string(cur))
		cur = next
	}
	var buf bytes.Buffer
	require.NoError(t, json.Compact(&buf, cur))
	return buf.String()
}

// textListPaths is each text list of the area, as the key path from the root of
// the WhereView text, and the table it belongs to.
var textListPaths = map[string][]string{
	"first_time_under_target":   {"first_time_under_target"},
	"stalled":                   {"stalled"},
	"blockers":                  {"blockers"},
	"decisions.receipt_history": {"decisions", "receipt_history"},
	"cost_check.orphans":        {"cost_check", "orphans"},
	"archived.streams":          {"archived", "streams"},
	"provider_gaps.tiers":       {"provider_gaps", "tiers"},
}

// shape holds the text lists of the area to the bytes the current encoder writes
// for them, and the whole text to the keys and values of the current encoder's.
func shape(t *testing.T, samples map[string]string) {
	canon := whereCanon(t, samples["WhereView"])
	for name, path := range textListPaths {
		want := rawAt(t, samples["WhereView"], path...)
		got := rawAt(t, canon, path...)
		assert.Equal(t, want, got, "%s: the text list is the array encoding/json writes for the []string", name)
		assert.True(t, strings.HasPrefix(want, `["`), "%s: the sample list is not empty", name)
	}
	// the lists inside lists of tables
	for _, c := range []struct {
		name string
		path []string
		idx  int
	}{{"lanes.held", []string{"held"}, 0}, {"lanes.waiting", []string{"waiting"}, 0}} {
		var wantLanes, gotLanes []json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(rawAt(t, samples["WhereView"], "lanes")), &wantLanes))
		require.NoError(t, json.Unmarshal([]byte(rawAt(t, canon, "lanes")), &gotLanes))
		assert.Equal(t, rawAt(t, string(wantLanes[c.idx]), c.path...), rawAt(t, string(gotLanes[c.idx]), c.path...), c.name)
	}
	assert.Equal(t, rawAt(t, samples["WhereView"], "priorities"), rawAt(t, canon, "priorities"),
		"priorities: a map of lists is an object of plain arrays, byte for byte")
	assert.Equal(t, rawAt(t, samples["WhereView"], "tables"), rawAt(t, canon, "tables"),
		"tables: the maps of maps of cells are the object encoding/json writes, byte for byte")
	assert.Equal(t, rawAt(t, samples["WhereView"], "landedSeries", "friends"), rawAt(t, canon, "landedSeries", "friends"))
	assert.Equal(t, rawAt(t, samples["WhereView"], "landedSeries", "workers"), rawAt(t, canon, "landedSeries", "workers"))
}

// whereCanon is the text the table writes for the sample.
func whereCanon(t *testing.T, sample string) string {
	t.Helper()
	ops := whereOps()
	codec := ops.codec("WhereView", true)
	v, _, err := schemaread.NewReader(codec).Read([]byte(sample))
	require.NoError(t, err)
	var compact bytes.Buffer
	require.NoError(t, json.Compact(&compact, codec.SaveJSON(&v)))
	return compact.String()
}

func whereOps() varOps[wire.WhereView] {
	return varOps[wire.WhereView]{
		builder((*wire.WhereViewBuilder).Init, (*wire.WhereViewBuilder).GetRoot, func(b *wire.WhereViewBuilder) *wire.TableArena { return &b.Arena }, wire.WhereViewFromJson),
		wire.WhereViewMeasure, wire.WhereViewSave, wire.WhereViewLoadMeasureReason, wire.WhereViewLoad, wire.WhereViewToJsonMeasure, wire.WhereViewToJson, wire.WhereViewTableType, whereViewBounds}
}

// divergences pins what the text form cannot keep of today's shape, by name: each
// is refused as the reader names it, never read as something else.
func divergences(t *testing.T, sample string) {
	r := schemaread.NewReader(whereOps().codec("WhereView", true))
	cases := []struct {
		what, text string
	}{
		{"a nil Go slice is null today and the list here", withField(t, sample, "stalled", nil)},
	}
	for _, c := range cases {
		got, _, err := r.Read([]byte(c.text))
		assert.Equal(t, schemaread.ReasonKindMismatch, refusalOf(t, "WhereView: "+c.what, err), c.what)
		assert.Nil(t, got, c.what)
	}
	// a zero time.Time is 0001-01-01T00:00:00Z today and has no timestamp
	zero, err := json.Marshal(time.Time{})
	require.NoError(t, err)
	got, _, err := r.Read([]byte(withField(t, sample, "at", strings.Trim(string(zero), `"`))))
	assert.Equal(t, "timestamp_not_rfc3339: at", refusalOf(t, "WhereView: a zero Go time", err), "a zero Go time is refused by name")
	assert.Nil(t, got)
	// the zero timestamp is 1970-01-01T00:00:00Z and reads back as it
	v, _, err := r.Read([]byte(withField(t, sample, "at", "1970-01-01T00:00:00Z")))
	require.NoError(t, err)
	assert.Equal(t, int64(0), v.At, "the zero timestamp is the count 0")
}

// ---------------------------------------------------------------------------
// The keys: the schema holds the keys of the Go structs, no more and no fewer.

// keyCheck holds each table's fields to the keys of its sample: every key the
// current encoder writes is a field, and every field is in a sample (so a field
// the Go struct has and the table lacks, or the reverse, shows here).
func keyCheck(t *testing.T, samples map[string]string) {
	infos := map[string]*wire.TableTypeInfo{}
	for _, v := range wire.UnitView().Tables {
		infos[v.Name] = v.Type
	}
	for _, name := range variableTables {
		info := infos[name]
		require.NotNil(t, info, name)
		if info.JsonArray {
			continue
		}
		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(samples[name]), &m), name)
		want := keysOf(info)
		for k := range m {
			assert.True(t, want[k], "%s: the key %q the current encoder writes is a field of the table", name, k)
		}
		for k := range want {
			_, ok := m[k]
			assert.True(t, ok, "%s: the field with key %q is in the sample, so its shape is proved", name, k)
		}
	}
}

// ---------------------------------------------------------------------------
// Long text and long lists.

func repeat(n int, f func(i int) any) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = f(i)
	}
	return out
}

// word is a text of exactly n bytes that starts with the prefix.
func word(prefix string, n int) string { return prefix + strings.Repeat("y", n-len(prefix)) }

// longEntries is the number of entries in each long list.
const longEntries = 200

// longBlockers is the number of entries in each long list whose element runs to tens of
// kilobytes (the lines of blockers and of the receipt history).
const longBlockers = 3

// longSample is the WhereView sample with each text at its bound, a cell at
// ntable.LimitFieldValueBytes and longEntries entries in each list.
func longSample(t *testing.T, sample string) string {
	t.Helper()
	m := decode(t, sample).(map[string]any)
	texts := func(n int, prefix string, bytes int) []any {
		return repeat(n, func(i int) any { return word(fmt.Sprintf("%s%03d-", prefix, i), bytes) })
	}
	for _, f := range openStrings(wire.WhereViewTableType()) {
		m[f.Json] = word(f.Name+": ", openBytes)
	}
	m["stalled"] = texts(longEntries, "s", 128)
	m["first_time_under_target"] = texts(longEntries, "f", 128)
	m["blockers"] = texts(longBlockers, "b", openBytes)
	nested := func(key, sub string, v []any) {
		o := m[key].(map[string]any)
		o[sub] = v
	}
	nested("decisions", "receipt_history", texts(longBlockers, "h", openBytes))
	nested("cost_check", "orphans", texts(longEntries, "o", 515))
	nested("archived", "streams", texts(longEntries, "a", 128))
	nested("provider_gaps", "tiers", texts(longEntries, "g", openBytes))
	m["priorities"] = map[string]any{"high": texts(longEntries, "p", 515), "low": texts(longEntries, "q", 515)}
	for _, key := range []string{"streams", "critical", "goals", "holds", "stops", "providers", "lanes", "cards", "merging", "judgments", "rows", "friends"} {
		first := m[key].([]any)[0]
		m[key] = repeat(longEntries, func(int) any { return first })
	}
	tables := m["tables"].(map[string]any)
	work := tables["work"].(map[string]any)
	work["v15-schema-tables"].(map[string]any)["a cell of 64 KiB"] = word("cell:", ntable.LimitFieldValueBytes)
	return encode(t, m)
}

// longChecks holds the long sample to the three forms and its lists to their
// length.
func longChecks(t *testing.T, sample string) {
	long := longSample(t, sample)
	ops := whereOps()
	codec := ops.codec("WhereView", true)
	r := schemaread.NewReader(codec)

	v, _, err := r.Read([]byte(long))
	require.NoError(t, err, "the long sample reads, every text at its bound")
	canon := string(codec.SaveJSON(&v))
	want, got := decode(t, long).(map[string]any), decode(t, canon).(map[string]any)
	require.True(t, contains(want, got), "every long text and list survives")
	for _, key := range []string{"stalled", "first_time_under_target", "streams", "rows", "cards", "friends"} {
		assert.Len(t, got[key], longEntries, "%s keeps its %d entries", key, longEntries)
	}
	assert.Len(t, got["priorities"].(map[string]any)["high"], longEntries)
	assert.Len(t, got["blockers"], longBlockers)
	assert.Len(t, got["decisions"].(map[string]any)["receipt_history"], longBlockers)

	good, err := r.Write(&v, schemaread.FormBinary)
	require.NoError(t, err)
	text, err := r.Write(&v, schemaread.FormBase64)
	require.NoError(t, err)
	assert.Equal(t, b64(good), string(text))
	js, err := r.Write(&v, schemaread.FormJSON)
	require.NoError(t, err)
	for _, data := range [][]byte{good, text, js} {
		back, res, err := r.Read(data)
		require.NoError(t, err, "the long document reads from %s", r.Detect(data))
		assert.Equal(t, canon, string(codec.SaveJSON(&back)), "the long document reads back from %s", res.Form)
	}
	refuseDamage(t, "WhereView", r, good)

	// the cell is a blob: one byte past ntable.LimitFieldValueBytes is refused by
	// the reader's constraint in every form, written by a codec without it
	over := decode(t, long).(map[string]any)
	over["tables"].(map[string]any)["work"].(map[string]any)["v15-schema-tables"].(map[string]any)["a cell of 64 KiB"] = word("cell:", ntable.LimitFieldValueBytes+1)
	overText := encode(t, over)
	bare := schemaread.NewReader(ops.codec("WhereView", false))
	bv, _, err := bare.Read([]byte(overText))
	require.NoError(t, err, "without the constraint the oversize cell reads")
	img, err := bare.Write(&bv, schemaread.FormBinary)
	require.NoError(t, err)
	strict := schemaread.NewReader(ops.codec("WhereView", true))
	for _, data := range [][]byte{img, []byte(b64(img)), []byte(overText)} {
		got, _, err := strict.Read(data)
		assert.Equal(t, schemaread.ReasonClamped, refusalOf(t, "WhereView.tables", err), "a cell past %d bytes is refused clamped", ntable.LimitFieldValueBytes)
		assert.Nil(t, got)
	}
}

// ---------------------------------------------------------------------------
// The definitions, the baseline and the pin.

func TestWhereViewGolden(t *testing.T) {
	t.Parallel()

	sample := samples()
	cases := variableCases(sample)
	require.Len(t, cases, len(variableTables), "a case for every table")
	for _, name := range variableTables {
		require.Contains(t, cases, name)
	}

	// the unit holds exactly these tables
	var names []string
	for _, v := range wire.UnitView().Tables {
		names = append(names, v.Name)
	}
	want := append([]string(nil), variableTables...)
	sort.Strings(want)
	sort.Strings(names)
	assert.Equal(t, want, names, "the area's tables")

	for _, name := range variableTables {
		run := cases[name]
		t.Run("variable/"+name, func(t *testing.T) {
			t.Parallel()
			run(t)
		})
	}
	t.Run("keys", func(t *testing.T) {
		t.Parallel()
		keyCheck(t, sample)
	})
	t.Run("shape", func(t *testing.T) {
		t.Parallel()
		shape(t, sample)
	})
	t.Run("divergences", func(t *testing.T) {
		t.Parallel()
		divergences(t, sample["WhereView"])
	})
	t.Run("long", func(t *testing.T) {
		t.Parallel()
		longChecks(t, sample["WhereView"])
	})

	// the definition and the baseline are committed, the baseline covers every
	// table of the area, and generate.go pins the compiler
	t.Run("baseline and pin", func(t *testing.T) {
		t.Parallel()
		root := filepath.Join("..", "..", "schemas", "view")
		_, err := os.Stat(filepath.Join(root, "where.schema"))
		require.NoError(t, err, "schemas/view/where.schema is committed")
		base, err := os.ReadFile(filepath.Join(root, "tables.baseline"))
		require.NoError(t, err, "schemas/view/tables.baseline is committed")
		var inBase []string
		for _, line := range strings.Split(string(base), "\n") {
			if n, ok := strings.CutPrefix(line, "table "); ok && !strings.Contains(n, ".") {
				inBase = append(inBase, strings.TrimSpace(n))
			}
		}
		sort.Strings(inBase)
		assert.Equal(t, want, inBase, "the baseline covers every table of the area")

		// the text lists' element bounds are the definitions' own
		src, err := os.ReadFile(filepath.Join(root, "where.schema"))
		require.NoError(t, err)
		declared, in := map[string]int{}, ""
		for _, line := range strings.Split(string(src), "\n") {
			l := strings.Fields(strings.TrimSpace(line))
			switch {
			case len(l) >= 2 && l[0] == "table":
				in = l[1]
			case len(l) >= 2 && strings.HasPrefix(l[1], "[]string("):
				n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(l[1], "[]string("), ")"))
				require.NoError(t, err)
				declared[in+"."+l[0]] = n
			}
		}
		assert.Equal(t, textBound, declared, "the element bounds this test pins are the text lists of where.schema")

		// the bound walk is not vacuous: it finds the declared bounds
		assert.Len(t, textLists(wire.WhereViewTableType()), 3, "WhereView's text lists")
		assert.Len(t, stringFields(wire.StreamClockTableType()), 1, "StreamClock's bounded strings: the stream key")
		assert.Len(t, stringFields(wire.GoalViewTableType()), 2, "GoalView's bounded strings: the name and the text")
		assert.Len(t, stringFields(wire.WhereViewTableType()), 0, "WhereView declares no bounded string")
		assert.Len(t, openStrings(wire.WhereViewTableType()), 12, "WhereView's open texts")
		assert.Len(t, openStrings(wire.ProviderViewTableType()), 8, "ProviderView's open texts, the state among them")
		assert.Len(t, openStrings(wire.HoldViewTableType()), 8, "HoldView's open texts, the name among them")
		assert.Len(t, openStrings(wire.StopTableType()), 6, "Stop's open texts, the subject among them")
		// the config names and the member are open texts: no length bounds a row name of nova-config
		// (pkg/config/kind.go:1018-1025), so none is declared and the open-text check above holds each
		// to 70000 bytes in the three forms
		for table, field := range map[string]struct {
			info  *wire.TableTypeInfo
			names []string
		}{
			"CostReconcileRecord": {wire.CostReconcileRecordTableType(), []string{"provider"}},
			"ProviderTier":        {wire.ProviderTierTableType(), []string{"tier"}},
			"ProviderView":        {wire.ProviderViewTableType(), []string{"name"}},
			"ProviderProblem":     {wire.ProviderProblemTableType(), []string{"name"}},
			"RouteGap":            {wire.RouteGapTableType(), []string{"route", "provider", "tier"}},
			"DealtCard":           {wire.DealtCardTableType(), []string{"member"}},
			"FriendRow":           {wire.FriendRowTableType(), []string{"name"}},
		} {
			open := map[string]bool{}
			for _, f := range openStrings(field.info) {
				open[f.Json] = true
			}
			for _, n := range field.names {
				assert.True(t, open[n], "%s.%s is an open text", table, n)
			}
		}

		gen, err := os.ReadFile("generate.go")
		require.NoError(t, err)
		assert.Contains(t, string(gen), "schema/v2/cmd/schema@"+pin+" generate --lang go --out . ../../schemas/view", "generate.go pins the compiler")
	})
}
