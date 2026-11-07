package cost

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// fakeTransport answers the balance endpoint with each body in turn.
type fakeTransport struct {
	bodies []string
	status int
	auth   []string
}

func (f *fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	body := f.bodies[0]
	if len(f.bodies) > 1 {
		f.bodies = f.bodies[1:]
	}
	return &http.Response{StatusCode: max(f.status, 200), Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
}

func balance(usd string) string {
	return `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"1.00"},{"currency":"USD","total_balance":"` + usd + `","granted_balance":"0.00","topped_up_balance":"` + usd + `"}]}`
}

var t0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

// seeded is a Fake with one stream, a coordinator row and two consumers.
func seeded(t *testing.T) *store.Fake {
	f := store.NewFake()
	apply(t, f, "seed", store.PutStream{Name: "s"}, store.PutRow{Name: "rowan", Session: "x", Role: "coordinator"}, store.SetRowState{Row: "rowan", State: "up"},
		store.PutCard{ID: "p", Kind: "producer", Stream: "s", Tier: "flash"},
		store.PutCard{ID: "p.w1", Kind: "work", Of: "p", Stream: "s", Tier: "flash", Holder: "rowan"},
		store.PutCard{ID: "p.r1", Kind: "read", Of: "p", Stream: "s", Tier: "flash", Holder: "rowan"},
		store.PutCard{ID: "p.r2", Kind: "read", Of: "p", Stream: "s", Tier: "flash", Holder: "rowan"})
	return f
}

func apply(t *testing.T, f *store.Fake, id string, ws ...store.Write) {
	t.Helper()
	r, err := f.Apply(context.Background(), store.Op{ID: id, Epoch: 1, Actor: "t", Kind: "t", At: t0, Writes: ws})
	require.NoError(t, err)
	require.True(t, r.Accepted, r.Reason)
}

func ds(tr *fakeTransport, key string) DeepSeek {
	return DeepSeek{Transport: tr, Getenv: func(n string) string {
		if n == "DEEPSEEK_API_KEY" {
			return key
		}
		return ""
	}}
}

func TestDeepSeekReadsUSDBalance(t *testing.T) {
	t.Parallel()
	tr := &fakeTransport{bodies: []string{balance("110.50")}}
	r := ds(tr, "sk-1").Read(context.Background())
	assert.Equal(t, Reading{Provider: "deepseek", Known: true, USD: "110.5", Raw: balance("110.50")}, r)
	assert.Equal(t, []string{"Bearer sk-1"}, tr.auth)
	r = ds(tr, "").Read(context.Background())
	assert.False(t, r.Known)
	assert.Contains(t, r.Why, "nova-secrets exec --only DEEPSEEK_API_KEY")
	r = ds(&fakeTransport{bodies: []string{`{"error":"x"}`}, status: 401}, "sk-1").Read(context.Background())
	assert.Equal(t, "GET https://api.deepseek.com/user/balance answered HTTP 401", r.Why)
	r = ds(&fakeTransport{bodies: []string{`{"balance_infos":[{"currency":"CNY","total_balance":"5"}]}`}}, "sk-1").Read(context.Background())
	assert.Equal(t, "the balance answer has no USD total_balance", r.Why)
	assert.False(t, Inception{}.Read(context.Background()).Known)
}

// The balance's fall between two reads is set beside the store's priced API
// records of the provider written in the same window: a subscription run, a
// run of another provider, and records before the first read do not count.
// A gap over 5% and $1 opens one judgement, never a second while it is open.
func TestReconcileWindowAndOneJudgement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := seeded(t)
	// before the first read: not in any window
	apply(t, f, "r0", store.SetCardCost{Card: "p.r2", Cost: 9_000_000, Priced: true}, store.SetCardUsage{Card: "p.r2", Usage: "input=1 model=deepseek/deepseek-flash billing=api charged=-"})
	tr := &fakeTransport{bodies: []string{balance("100.00"), balance("97.00"), balance("90.00"), balance("95.00")}}
	readers := []Reader{ds(tr, "sk"), Inception{}}
	lines, err := Reconcile(ctx, f, t0, "rowan", readers)
	require.NoError(t, err)
	require.Len(t, lines, 2)
	assert.Equal(t, "COST deepseek balance=$100: the first read: the next one measures the spend between them", lines[0].Text())
	assert.Contains(t, lines[1].Text(), "COST inception unknown: inception publishes no balance or usage endpoint")

	// in the window: a priced API run of deepseek ($2.50), a subscription run, an inception run
	apply(t, f, "r1", store.SetCardCost{Card: "p.w1", Cost: 2_500_000, Priced: true}, store.SetCardUsage{Card: "p.w1", Usage: "input=1000 model=deepseek/deepseek-flash billing=api charged=-"})
	apply(t, f, "r2", store.SetCardCost{Card: "p.r1", Cost: 0, Priced: true}, store.SetCardUsage{Card: "p.r1", Usage: "input=5 billing=subscription charged=1"})
	lines, err = Reconcile(ctx, f, t0.Add(time.Hour), "rowan", readers[:1])
	require.NoError(t, err)
	l := lines[0]
	assert.Equal(t, int64(3_000_000), l.Spent)
	assert.Equal(t, int64(2_500_000), l.Recorded)
	assert.Equal(t, "", l.Judgement, "a $0.50 gap is under the $1 floor")
	assert.Equal(t, "COST deepseek 2026-11-07T12:00:00Z..2026-11-07T13:00:00Z provider=$3 recorded=$2.5 gap=$0.5", l.Text())

	// $7 spent, $0 recorded: the judgement, then never a second
	lines, err = Reconcile(ctx, f, t0.Add(2*time.Hour), "rowan", readers[:1])
	require.NoError(t, err)
	assert.Equal(t, "cost-gap.rowan.1", lines[0].Judgement)
	s, _ := f.Snapshot(ctx)
	j := s.Judgements["cost-gap.rowan.1"]
	assert.Equal(t, "open", j.State)
	assert.Equal(t, "rowan", j.About)
	assert.Contains(t, j.Command, "choices: ack | retry")
	// a top-up: no spend measured, no judgement
	lines, err = Reconcile(ctx, f, t0.Add(3*time.Hour), "rowan", readers[:1])
	require.NoError(t, err)
	assert.Contains(t, lines[0].Why, "a top-up")
	tr.bodies = []string{balance("80.00")}
	lines, err = Reconcile(ctx, f, t0.Add(4*time.Hour), "rowan", readers[:1])
	require.NoError(t, err)
	assert.Equal(t, "cost-gap.rowan.1", lines[0].Judgement, "the open judgement is named, never a second")
	s, _ = f.Snapshot(ctx)
	n := 0
	for _, j := range s.Judgements {
		if j.Kind == Kind {
			n++
		}
	}
	assert.Equal(t, 1, n)
	last, ok, err := f.LastBalance(ctx, "deepseek")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "80", last.USD)
}

func TestOverBothBounds(t *testing.T) {
	t.Parallel()
	assert.False(t, over(900_000, 1_000_000), "under $1")
	assert.False(t, over(1_500_000, 100_000_000), "under 5%")
	assert.True(t, over(-6_000_000, 100_000_000), "a store over the provider is a gap too")
	assert.Equal(t, int64(1_234_568), micro("1.2345675"))
}

func TestHourly(t *testing.T) {
	t.Parallel()
	var h Hourly
	assert.True(t, h.Due(t0), "the first beat reconciles")
	assert.False(t, h.Due(t0.Add(59*time.Minute)))
	assert.True(t, h.Due(t0.Add(time.Hour)))
	assert.False(t, h.Due(t0.Add(time.Hour+time.Second)))
}
