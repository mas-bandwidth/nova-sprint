package store

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/cardcost"
	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/redisconn"
)

// RouteReader is a store that holds the model tiers' routes and route arrays
// nova-config applies (config.RoutesKey, config.RouteKey, config.TierKey): a step
// that deals or asks reads them with its tables (Step.Routes; internal/sprint/route.go). A store
// that is not one has no route, and the deal deals as before.
type RouteReader interface {
	// Routes is every route and each tier's route array, and the round trips
	// the read made.
	Routes(ctx context.Context) (RouteSet, int64, error)
}

// PriceReader is a store that holds the routes a worker's step prices with: the
// routes set and each route's record, and nothing else (Step.Prices). A store that
// is not one has no route, and nothing is priced.
type PriceReader interface {
	// PriceRoutes is every route, read from the set and the routes' records alone,
	// and the round trips the read made.
	PriceRoutes(ctx context.Context) ([]sprint.Route, int64, error)
}

// priceRoutes is the routes a worker's step prices with, by name; an empty list when
// the store holds none.
func (st *Store) priceRoutes(ctx context.Context) ([]sprint.Route, error) {
	pr, ok := st.B.(PriceReader)
	if !ok {
		return []sprint.Route{}, nil
	}
	rs, _, err := pr.PriceRoutes(ctx)
	if err != nil {
		return nil, fmt.Errorf("the routes to price with (%s, %s): %w", config.RoutesKey, config.RouteKey("<name>"), err)
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].Name < rs[j].Name })
	return append([]sprint.Route{}, rs...), nil
}

// PriceRoutes reads the set, then every route's record in one pipeline: two round
// trips, the second only when the set names a route. It touches the keys
// config.RoutesKey and config.RouteKey alone, which every role reads
// (pkg/redisacl, the routes family): never a tier's array or the sprint row.
func (r *Redis) PriceRoutes(ctx context.Context) ([]sprint.Route, int64, error) {
	names, err := r.C.SMembers(ctx, config.RoutesKey).Result()
	if err != nil || len(names) == 0 {
		return nil, 1, err
	}
	pipe := r.C.Pipeline()
	hs := make(map[string]interface{ Val() map[string]string }, len(names))
	for _, n := range names {
		hs[n] = pipe.HGetAll(ctx, config.RouteKey(n))
	}
	if err := redisconn.Exec(ctx, pipe); err != nil {
		return nil, 2, err
	}
	out := make([]sprint.Route, 0, len(names))
	for _, n := range names {
		out = append(out, RouteOf(n, hs[n].Val()))
	}
	return out, 2, nil
}

// PriceRoutes is the routes SetRoutes gave the store.
func (m *Mem) PriceRoutes(context.Context) ([]sprint.Route, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("routes"); err != nil {
		return nil, 0, err
	}
	return append([]sprint.Route(nil), m.routes...), 0, nil
}

// RouteSet is what a step that deals or asks plans with: the routes, the
// tiers' arrays, and the sprint row's nova-decide bars, read with them (Bars).
type RouteSet struct {
	Providers map[string]sprint.Provider
	Routes    []sprint.Route
	Tiers     map[string][]string
	Bars      Bars
}

// Bars is the sprint row's nova-decide bars, each by the name of its field, so no bar is
// ever read as another: the decide read's bounce and review bars, the attempt decision's
// no-result and nothing-to-do bars, the grade's, the landed score's, the gate decision's
// flaky and pre-existing bars, and the judgment decision's (nova-sprint answer reads it
// from routes --json). "" is no bar.
type Bars struct {
	Bounce, Review                      string // decide_bounce, decide_review
	AttemptNoResult, AttemptNothingToDo string // decide_attempt_no_result, decide_attempt_nothing_to_do
	Grade                               string // decide_grade
	Score                               string // decide_score_bar
	GateFlaky, GatePreexisting          string // decide_gate_flaky, decide_gate_preexisting
	Judgment                            string // decide_judgment_bar
	// RulesOff is the sprint row's rules the machine does not answer by (answer_rules_off: a
	// comma list of config.AnswerRules); "" turns none off.
	RulesOff string
	// GitIdentity is the sprint row's author and committer for every commit the sprint
	// machine makes (git_identity: "Name <email>"), as stored: "" is the row unapplied, which
	// its readers take as config.DefaultGitIdentity (WithDefaults; the snapshot's GitIdentity
	// and Store.GitIdentity), so the landing commits as the machine identity, never refuses.
	GitIdentity string
	// MaxAttempts is the sprint row's attempt bound (max_attempts): the attempts one card
	// runs before the sprint raises one judgment to the coordinator (sprint.AttemptsCap);
	// "" or unparseable is the default, sprint.AttemptsDefault.
	MaxAttempts string
	// RepeatPushes is the sprint row's repeat bound (repeat_pushes): "" or unparseable
	// is the default, 3 (sprint.RepeatPushes).
	RepeatPushes string
}

// fields is each bar by its sprint row field (config.SprintKey(field) holds it).
func (b *Bars) fields() map[string]*string {
	return map[string]*string{
		config.FieldDecideBounce:             &b.Bounce,
		config.FieldDecideReview:             &b.Review,
		config.FieldDecideAttemptNoResult:    &b.AttemptNoResult,
		config.FieldDecideAttemptNothingToDo: &b.AttemptNothingToDo,
		config.FieldDecideGrade:              &b.Grade,
		config.FieldDecideScoreBar:           &b.Score,
		config.FieldDecideGateFlaky:          &b.GateFlaky,
		config.FieldDecideGatePreexisting:    &b.GatePreexisting,
		config.FieldDecideJudgment:           &b.Judgment,
		config.FieldAnswerRulesOff:           &b.RulesOff,
		config.FieldGitIdentity:              &b.GitIdentity,
		config.FieldMaxAttempts:              &b.MaxAttempts,
		config.FieldRepeatPushes:             &b.RepeatPushes,
	}
}

// WithDefaults is the bars with each empty field whose sprint.Defaults row says ReadFill
// given that row's Value: the read-side default of nova-config's sprint row, which the
// sprint never writes (nova-config apply would undo it). An unapplied git_identity reads
// config.DefaultGitIdentity, as NewMem's does, so the landing commits as the machine
// identity, never the host's (docs/SPEC-SPRINT.md section 13, Epochs and clear, "Working defaults").
func (b Bars) WithDefaults() Bars {
	for name, p := range b.fields() {
		if d, ok := sprint.DefaultOf(name); ok && d.ReadFill && *p == "" {
			*p = d.Value
		}
	}
	return b
}

// into is the set as the snapshot carries it.
func (rs RouteSet) into(s *sprint.Snapshot) {
	s.Routes, s.Tiers, s.Providers = rs.Routes, rs.Tiers, rs.Providers
	rs.Bars.into(s)
}

// into is the bars as the snapshot carries them.
func (b Bars) into(s *sprint.Snapshot) {
	s.GitIdentityUnapplied = b.GitIdentity == ""
	b = b.WithDefaults()
	s.DecideBounce, s.DecideReview, s.DecideGrade = b.Bounce, b.Review, b.Grade
	s.DecideAttemptNoResult, s.DecideAttemptNothingToDo = b.AttemptNoResult, b.AttemptNothingToDo
	s.DecideScoreBar = b.Score
	s.DecideGateFlaky, s.DecideGatePreexisting = b.GateFlaky, b.GatePreexisting
	s.RulesOff = sprint.Split(b.RulesOff)
	s.GitIdentity = b.GitIdentity
	s.MaxAttempts = b.MaxAttempts
	s.RepeatPushes = b.RepeatPushes
}

// BarsReader is a store that holds the sprint row's bars alone (config.SprintKey(field)):
// what a worker's step reads for the attempt cap (Step.Bars; finish, read, merge, set),
// keys every role reads (pkg/redisacl, the sprint family) and never a tier's array, which
// the member role may not read (TestEveryStepReadsOnlyKeysItsRoleMayRead).
type BarsReader interface {
	// Bars is the sprint row's bars and the round trips the read made.
	Bars(ctx context.Context) (Bars, int64, error)
}

// sprintBars is the sprint row's bars a step plans with when it reads no route set
// (Step.Bars): the attempt cap among them (sprint.AttemptsCap); the zero Bars when the store
// holds none.
func (st *Store) sprintBars(ctx context.Context) (Bars, error) {
	br, ok := st.B.(BarsReader)
	if !ok {
		return Bars{}, nil
	}
	b, _, err := br.Bars(ctx)
	if err != nil {
		return Bars{}, fmt.Errorf("the sprint row's bars (%s): %w", config.SprintKey("<field>"), err)
	}
	return b, nil
}

// Bars reads the sprint row's bars in one pipeline: one round trip, the plain keys
// config.SprintKey(field) alone.
func (r *Redis) Bars(ctx context.Context) (Bars, int64, error) {
	var b Bars
	fields := b.fields()
	pipe := r.C.Pipeline()
	gets := make(map[string]interface{ Val() string }, len(fields))
	for f := range fields {
		gets[f] = pipe.Get(ctx, config.SprintKey(f))
	}
	if err := redisconn.Exec(ctx, pipe); err != nil {
		return Bars{}, 1, err
	}
	for f, v := range fields {
		*v = gets[f].Val()
	}
	return b, 1, nil
}

// Bars is the bars the setters gave the store (SetDecideBars, SetMaxAttempts, ...).
func (m *Mem) Bars(context.Context) (Bars, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("routes"); err != nil {
		return Bars{}, 0, err
	}
	return m.bars, 0, nil
}

// RoutesInto reads the routes and the sprint row's bars (Routes) into the snapshot, as a
// step with Routes set plans with them: what a reader of a loaded snapshot calls when it
// asks a question the bars decide (the attempt cap, sprint.AttemptsCap; the rules off).
func (st *Store) RoutesInto(ctx context.Context, s *sprint.Snapshot) error {
	set, err := st.routes(ctx)
	if err != nil {
		return err
	}
	set.into(s)
	return nil
}

// routes is the routes a dealing step plans with, by name, and the tiers'
// arrays; an empty, non-nil list when the store holds none (read: the
// no-stall rule reads none again).
func (st *Store) routes(ctx context.Context) (RouteSet, error) {
	rs, _, err := st.routesTrips(ctx)
	return rs, err
}

// routesTrips is routes with the round trips its read made.
func (st *Store) routesTrips(ctx context.Context) (RouteSet, int64, error) {
	rr, ok := st.B.(RouteReader)
	if !ok {
		return RouteSet{Routes: []sprint.Route{}}, 0, nil
	}
	set, trips, err := rr.Routes(ctx)
	if err != nil {
		return RouteSet{}, trips, fmt.Errorf("the routes (%s): %w", config.RoutesKey, err)
	}
	if set.Routes == nil {
		set.Routes = []sprint.Route{}
	}
	sort.Slice(set.Routes, func(i, j int) bool { return set.Routes[i].Name < set.Routes[j].Name })
	return set, trips, nil
}

// RouteCache is the routes read once and shared: a tick's, read by its first part
// that deals or checks and handed to every later one (the routes are config, read
// once a tick; tla/DirtyTick.tla holds them constant). Trips is the round trips
// the one read made, as the backend counts them (a shared client's trip counter
// also counts its other goroutines').
type RouteCache struct {
	read  bool
	set   RouteSet
	Trips int64
}

// cached is the route set from the cache, read through it the first time; a nil
// cache reads it for this step alone.
func (st *Store) cached(ctx context.Context, c *RouteCache) (RouteSet, error) {
	if c == nil {
		return st.routes(ctx)
	}
	if !c.read {
		set, trips, err := st.routesTrips(ctx)
		if err != nil {
			return RouteSet{}, err
		}
		c.read, c.set, c.Trips = true, set, trips
	}
	return c.set, nil
}

// routeArrayTiers are the tiers whose route array the load reads: every tier a card is
// dealt on, frontier too (a frontier card is drawn from its routes like every tier,
// tla/RouteIndex.tla EveryTierIsDealtFromItsRoutes). It is this list and not
// config.RouteTiers, which leaves frontier out until the config side holds it.
var routeArrayTiers = []string{cardhdr.RouteFlash, cardhdr.RoutePro, cardhdr.RouteHeavy, cardhdr.RouteFrontier}

// Routes reads the set and the sprint row's nova-decide bars (Bars) in one
// pipeline: one round trip when neither set names a route or provider, so a read card with no
// route still draws its bars and git_identity, as the twin's Routes returns
// them. With a route, every route's hash and each tier's array ride in a second
// pipeline alongside provider hashes: two round trips when either set has a row.
func (r *Redis) Routes(ctx context.Context) (RouteSet, int64, error) {
	var set RouteSet
	fields := set.Bars.fields()
	pipe := r.C.Pipeline()
	namesCmd := pipe.SMembers(ctx, config.RoutesKey)
	providerNamesCmd := pipe.SMembers(ctx, config.ProvidersKey)
	bars := make(map[string]interface{ Val() string }, len(fields))
	for f := range fields {
		bars[f] = pipe.Get(ctx, config.SprintKey(f))
	}
	if err := redisconn.Exec(ctx, pipe); err != nil {
		return RouteSet{}, 1, err
	}
	for f, v := range fields {
		*v = bars[f].Val()
	}
	names := namesCmd.Val()
	providerNames := providerNamesCmd.Val()
	if len(names) == 0 && len(providerNames) == 0 {
		return RouteSet{Routes: []sprint.Route{}, Bars: set.Bars}, 1, nil
	}
	pipe2 := r.C.Pipeline()
	phs := make(map[string]interface{ Val() map[string]string }, len(providerNames))
	for _, name := range providerNames {
		phs[name] = pipe2.HGetAll(ctx, config.ProviderKey(name))
	}
	hs := make(map[string]interface{ Val() map[string]string }, len(names))
	for _, n := range names {
		hs[n] = pipe2.HGetAll(ctx, config.RouteKey(n))
	}
	arrays := make(map[string]interface {
		Val() string
		Err() error
	}, len(routeArrayTiers))
	for _, t := range routeArrayTiers {
		arrays[t] = pipe2.HGet(ctx, config.TierKey(t), "routes")
	}
	if err := redisconn.Exec(ctx, pipe2); err != nil {
		return RouteSet{}, 2, err
	}
	out := make([]sprint.Route, 0, len(names))
	for _, n := range names {
		out = append(out, RouteOf(n, hs[n].Val()))
	}
	tiers := map[string][]string{}
	for _, t := range routeArrayTiers {
		// Preserve a present empty field: the deal falls back to enabled native routes.
		if arrays[t].Err() == nil {
			tiers[t] = sprint.Split(arrays[t].Val())
		}
	}
	set.Providers = map[string]sprint.Provider{}
	for _, name := range providerNames {
		p, err := ProviderOf(name, phs[name].Val())
		if err != nil {
			return RouteSet{}, 2, err
		}
		set.Providers[name] = p
	}
	set.Routes, set.Tiers = out, tiers
	return set, 2, nil
}

// RouteOf is a route from its hash as nova-config's apply writes it: a field
// missing or unreadable is its zero, and the price sheet its price fields.
// First is the hash's first field (docs/SPEC-CONFIG.md, route; docs/SPEC-SPRINT.md,
// the deal): true draws this route before the others of its tier.
func RouteOf(name string, h map[string]string) sprint.Route {
	n := func(k string) int { v, _ := strconv.Atoi(h[k]); return v }
	enabled, _ := strconv.ParseBool(h["enabled"])
	first, _ := strconv.ParseBool(h["first"])
	return sprint.Route{Name: name, Tier: h["tier"], Provider: h["provider"], Model: h["model"], Harness: h["harness"], Tokens: n("tokens"), USD: h["usd"],
		Deadline: n("deadline"), Enabled: enabled, First: first, Prices: cardcost.PricesOf(h)}
}

// Routes is the routes SetRoutes gave the store and the arrays SetTiers gave it.
func (m *Mem) Routes(context.Context) (RouteSet, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("routes"); err != nil {
		return RouteSet{}, 0, err
	}
	tiers := map[string][]string{}
	for t, a := range m.tiers {
		tiers[t] = append([]string(nil), a...)
	}
	return RouteSet{Routes: append([]sprint.Route(nil), m.routes...), Tiers: tiers, Providers: maps.Clone(m.providers), Bars: m.bars}, 0, nil
}

// SetRepeatPushes gives the store the sprint row's repeat bound, as nova-config's
// apply does a live one (sprint:repeat_pushes); "" is none applied (default 3).
func (m *Mem) SetRepeatPushes(n string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bars.RepeatPushes = n
}

// SetMaxAttempts gives the store the sprint row's attempt bound, as nova-config's apply
// does a live one (sprint:max_attempts); "" is none applied (sprint.AttemptsDefault).
func (m *Mem) SetMaxAttempts(n string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bars.MaxAttempts = n
}

// SetGateBars gives the store the gate decision's flaky and pre-existing bars, as
// nova-config's apply does a live one (sprint:decide_gate_flaky, sprint:decide_gate_preexisting).
func (m *Mem) SetGateBars(flaky, preExisting string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bars.GateFlaky, m.bars.GatePreexisting = flaky, preExisting
}

// GateBars is the gate decision's flaky and pre-existing bars as the routes read takes
// them: the lander's, for its red batch gate (docs/SPEC-SPRINT.md section 7).
func (st *Store) GateBars(ctx context.Context) ([2]string, error) {
	set, err := st.routes(ctx)
	return [2]string{set.Bars.GateFlaky, set.Bars.GatePreexisting}, err
}

// SetDecideBars gives the store the sprint row's nova-decide bars, as nova-config's apply
// does a live one (sprint:decide_bounce, sprint:decide_review,
// sprint:decide_attempt_no_result, sprint:decide_attempt_nothing_to_do, sprint:decide_grade).
func (m *Mem) SetDecideBars(b Bars) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bars = b
}

// SetScoreBar gives the store the landed score's bar, as nova-config's apply does a live
// one (sprint:decide_score_bar).
func (m *Mem) SetScoreBar(bar string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bars.Score = bar
}

// SetJudgmentBar gives the store the judgment decision's bar, as nova-config's apply
// does a live one (sprint:decide_judgment_bar).
func (m *Mem) SetJudgmentBar(bar string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bars.Judgment = bar
}

// SetRoutes gives the store its routes, as nova-config's apply does a live one.
func (m *Mem) SetRoutes(rs []sprint.Route) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routes = append([]sprint.Route(nil), rs...)
}

// SetTiers gives the store its tiers' route arrays, as nova-config's apply does a
// live one (tier:<name>).
func (m *Mem) SetTiers(tiers map[string][]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tiers = tiers
}

// Routes is the store's routes, by name, and the tiers' arrays, as a dealing step
// reads them: for the read verbs (where, routes).
func (st *Store) Routes(ctx context.Context) ([]sprint.Route, map[string][]string, error) {
	set, err := st.routes(ctx)
	return set.Routes, set.Tiers, err
}

// OutOfCredit is the words of sprint.FundsCause when every enabled route of the store rests
// for its provider's funds (sprint.AllOutOfCredit), "" otherwise: what a start is refused
// with. It reads the routes and the fleet table's properties, no card.
func (st *Store) OutOfCredit(ctx context.Context) (string, error) {
	routes, _, err := st.Routes(ctx)
	if err != nil || len(routes) == 0 {
		return "", err
	}
	shapes, err := st.B.Shapes(ctx, []string{st.Names.Table(sprint.Fleet)})
	if err != nil {
		return "", err
	}
	fleet := sprint.NewTable(sprint.Fleet)
	fleet.SetProps(shapes[0].Props)
	why := sprint.AllOutOfCredit(routes, sprint.RouteRests(routes, fleet), st.now())
	if why == "" {
		return "", nil
	}
	// a friend up keeps the machine running: the paid routes rest, friends are dealt
	// (sprint.AnyFriendUp; the owner, 2026-10-04)
	seats, err := st.friendSeats(ctx, nil, st.now())
	if err != nil {
		return "", err
	}
	if sprint.AnyFriendUp(seats) {
		return "", nil
	}
	return why, nil
}

// JudgmentBar is the judgment decision's bar as nova-config applied it, read with the
// routes: for routes --json, which answer reads. "" when none is applied.
func (st *Store) JudgmentBar(ctx context.Context) (string, error) {
	set, err := st.routes(ctx)
	return set.Bars.Judgment, err
}

// SetRulesOff gives the store the rules the machine does not answer by, as nova-config's
// apply does a live one (sprint:answer_rules_off).
func (m *Mem) SetRulesOff(rules ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bars.RulesOff = strings.Join(rules, ",")
}

// SetGitIdentity gives the store the sprint row's git identity, as nova-config's apply does
// a live one (sprint:git_identity).
func (m *Mem) SetGitIdentity(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bars.GitIdentity = id
}

// RulesOff is the rules the machine does not answer by as nova-config applied them, read
// with the routes: the lander reads base-gate there (cmd/nova-sprint, landgo.go).
func (st *Store) RulesOff(ctx context.Context) ([]string, error) {
	set, err := st.routes(ctx)
	return sprint.Split(set.Bars.RulesOff), err
}

// GitIdentity is the sprint row's author and committer for every commit the sprint machine
// makes, as nova-config applied it (sprint:git_identity), read with the routes: the lander
// reads it there (cmd/nova-sprint, land.go). An unapplied row reads config.DefaultGitIdentity
// (Bars.WithDefaults; sprint.Defaults).
func (st *Store) GitIdentity(ctx context.Context) (string, error) {
	set, err := st.routes(ctx)
	return set.Bars.WithDefaults().GitIdentity, err
}

// ProviderOf reads an applied provider hash. Malformed funding is non-fatal
// for tick route reads, but remains a named diagnostic and never counts funded.
func ProviderOf(name string, h map[string]string) (sprint.Provider, error) {
	p := sprint.Provider{Name: name, Billing: h["billing"]}
	funded, err := strconv.ParseBool(h["funded"])
	if err == nil {
		p.Funded = funded
		return p, nil
	}
	switch {
	case len(h) == 0:
		p.Problem = "provider hash is missing"
	case h["funded"] == "":
		p.Problem = "funded field is missing or empty"
	default:
		p.Problem = fmt.Sprintf("funded field is invalid: %q", h["funded"])
	}
	return p, nil
}

// SetProviders gives the twin the provider rows nova-config apply writes.
func (m *Mem) SetProviders(providers map[string]sprint.Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers = maps.Clone(providers)
}
