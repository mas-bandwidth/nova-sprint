package sprint

import (
	"encoding/base64"
	"encoding/json"
	"github.com/mas-bandwidth/nova-tools/pkg/cardcost"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestFleetSubscriptionReceiptSurvivesPairedAdoption(t *testing.T) {
	t.Parallel()
	for _, billing := range []string{"subscription", "plan", "metered"} {
		t.Run(billing, func(t *testing.T) {
			t.Parallel()
			w := setup(t, 1)
			w.s.Routes = []Route{{Name: "fleet-muse", Provider: "provider", Model: "model", Enabled: true, Prices: cardcost.Prices{Billing: billing, Input: "0.3", Output: "1"}}}
			rec := costRecord(w.s, "input=1000000 output=0 actual_usd=0.25 actual_by=harness", "fleet-muse", "provider/model", false, "", "")
			u := cardcost.ParseUsage(rec)
			want := "0.3"
			charged := "0.25"
			if billing == "subscription" {
				want = "0"
				charged = "0"
			}
			require.Equal(t, want, u.Predicted)
			require.Equal(t, "0.25", u.Actual, "retain raw harness-reported dollars")
			require.Equal(t, "harness", u.ActualBy)
			con := Consumer{Key: "fleet.w1#1", Kind: "work", Card: "fleet.w1", Route: "fleet-muse", Usage: u}
			round := parseConsumer(con.Key, con.line())
			require.Equal(t, u.String(), round.Usage.String())
			b, err := json.Marshal(round)
			require.NoError(t, err)
			var restarted Consumer
			require.NoError(t, json.Unmarshal(b, &restarted))
			require.Equal(t, u.String(), restarted.Usage.String())
			total := cardcost.NoTotal().Add(restarted.Usage)
			require.Equal(t, charged, total.Charged)
		})
	}
	t.Run("subscription without a token report", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		w.s.Routes = []Route{{Name: "subscription", Prices: cardcost.Prices{Billing: "subscription", Input: "999"}}}
		u := cardcost.ParseUsage(costRecord(w.s, "", "subscription", "", false, "", ""))
		require.Equal(t, "0", u.Predicted)
		require.Empty(t, u.ActualBy)
		require.Equal(t, cardcost.Unreported, u.Tokens.Input)
	})
	t.Run("legacy subscription read retains reported dollars", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		read := &Card{Fields: map[string]string{}}
		u := cardcost.ParseUsage(readCostRecord(w.s, read, "billing=subscription input=12 actual_usd=0 actual_by=harness", "", ""))
		require.Equal(t, "0", u.Actual, "reported zero is evidence, not absence")
		require.Equal(t, "harness", u.ActualBy)
		require.Equal(t, "0", cardcost.NoTotal().Add(u).Charged)
	})
}

func subscriptionRawFixture(t *testing.T) cardcost.Usage {
	t.Helper()
	raw := `{"version":1,"records":[{"harness":"muse","execution":"fleet","source_schema":"muse-jsonl-v1","producer_version":"paired-test-source","billing":"subscription","tokens":{"input_tokens":12,"output_tokens":7,"cached_tokens":5,"cache_write_tokens":3,"cache_read_tokens":5,"reasoning_tokens":2}}]}`
	spend := "input:12,output:7,cache_write:3,cache_read:5,reasoning:2,model:provider/model,receipt_version:2,billing:subscription,raw_usage:" + base64.RawURLEncoding.EncodeToString([]byte(raw))
	u, err := cardcost.ParseSpendChecked(spend)
	require.NoError(t, err)
	encoded, err := cardcost.EncodeSpend(u)
	require.NoError(t, err)
	u, err = cardcost.ParseSpendChecked(encoded)
	require.NoError(t, err)
	return u
}

func TestFleetSubscriptionRawReplayAndMarginalProjections(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	route := Route{Name: "fleet-muse", Provider: "provider", Model: "model", Harness: "muse", Enabled: true, Prices: cardcost.Prices{Billing: "subscription", Input: "999", Output: "999"}}
	w.s.Routes = []Route{route}
	u := subscriptionRawFixture(t)
	require.Equal(t, map[string]int64{"input_tokens": 12, "output_tokens": 7, "cached_tokens": 5, "cache_write_tokens": 3, "cache_read_tokens": 5, "reasoning_tokens": 2}, u.Raw.Records[0].Tokens)
	require.Equal(t, int64(29), u.Tokens.Total(), "raw cached counter is evidence, never an extra normalized charge/budget class")
	u.Actual = "9.25"
	u.ActualBy = "harness"
	rawLine := u.String()
	card := &Card{ID: "fleet.w1", Fields: map[string]string{FieldRoute: route.Name, FieldModel: "provider/model", FieldHarness: "muse", FieldBilling: "subscription"}}
	require.Empty(t, w.s.receiptBillingRefusal(card, rawLine))
	for _, billing := range []string{"plan", "metered"} {
		w.s.Routes[0].Prices.Billing = billing
		require.Empty(t, w.s.receiptBillingRefusal(card, rawLine), "replay keeps the launch's billing")
	}
	w.s.Routes = nil
	require.Empty(t, w.s.receiptBillingRefusal(card, rawLine), "deleting current route cannot invalidate original evidence")
	w.s.Routes = []Route{route}
	u = cardcost.ParseUsage(costRecord(w.s, rawLine, route.Name, "provider/model", false, "", ""))
	require.Equal(t, "0", u.Predicted)
	require.Equal(t, "9.25", u.Actual)
	require.Equal(t, "0", u.MarginalUSD())
	require.Equal(t, int64(5), u.Raw.Records[0].Tokens["cached_tokens"])
	set := map[string]string{}
	con := Consumer{Kind: "work", Card: card.ID, Key: card.ID + "#1", Route: route.Name, Model: "provider/model", At: stamp(w.s.Now), Usage: u}
	pr := w.s.Work.Card("s1-1")
	addConsumer(pr, set, con)
	for k, v := range set {
		pr.Fields[k] = v
	}
	rawStored := pr.F(FieldCostRecord + con.Key)
	require.Contains(t, rawStored, "raw_usage=")
	b, err := json.Marshal(pr)
	require.NoError(t, err)
	var restored Card
	require.NoError(t, json.Unmarshal(b, &restored))
	view := CardCostOf(&restored)
	require.Len(t, view.Consumers, 1)
	require.Equal(t, int64(5), view.Consumers[0].Usage.Raw.Records[0].Tokens["cached_tokens"])
	require.Equal(t, "0", view.Total.Charged)
	require.Equal(t, "9.25", view.Total.Actual)
	require.Zero(t, SpendLastHour(w.s, "provider"))
	all, reads := internalSpendSplit(w.s, "provider", w.s.Now.UTC().Format(time.DateOnly))
	require.Zero(t, all)
	require.Zero(t, reads)
	require.Zero(t, RecordedSpendIn(w.s, "provider", w.s.Now.Add(-time.Hour), w.s.Now.Add(time.Hour)))
	stats := rtFromUsage(u.String())
	require.True(t, stats.priced)
	require.Zero(t, stats.usd)
	var rs ReaderSpend
	rs.addRead(Consumer{Kind: "read", Usage: u, At: stamp(w.s.Now)}, w.s.Now)
	require.Empty(t, rs.USD, "subscription raw dollars are not reader burn")
	_, words := SplitCost(Line{Card: pr.ID, Set: set})
	require.NotEmpty(t, words)
	require.Contains(t, strings.Join(words, " "), "cost $0.00")
	w.s.Routes[0].Prices.Billing = "metered"
	repriced, _ := RepriceOf(w.s, RepriceReq{})
	w.must(repriced)
	require.Equal(t, "0", CardCostOf(pr).Total.Charged, "repricing cannot reinterpret declared subscription evidence")
}

func TestFleetSubscriptionPairedCompatibilityRefusesBeforeWrite(t *testing.T) {
	t.Parallel()
	t.Run("work", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		w.s.Routes = []Route{{Name: "subscription", Tier: "pro", Provider: "provider", Model: "model", Enabled: true, Harness: "muse", Prices: cardcost.Prices{Billing: "subscription"}}}
		w.s.Tiers = map[string][]string{"pro": {"subscription"}}
		for _, m := range []string{"m1", "m2"} {
			w.s.MemberCtl(m).Fields[FieldHarnesses] = "muse"
		}
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		wc := w.s.Fleet.Card(w.s.Work.Card("s1-1").F("work"))
		w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: w.gens(wc.ID)}))
		p := Finish(w.s, FinishReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: w.gens(wc.ID), Usage: "input=12 output=7"})
		require.NotEmpty(t, p.Refused)
		require.Contains(t, p.Refused[0].Why, "contract 2")
		require.Empty(t, p.Units)
		require.Empty(t, wc.F(FieldUsage))
		require.Empty(t, w.s.Work.Card("s1-1").F(FieldCostTotal))
		u := subscriptionRawFixture(t)
		p = Finish(w.s, FinishReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: w.gens(wc.ID), Usage: u.String(), Report: "done"})
		require.Empty(t, p.Refused)
		w.must(p)
	})
	t.Run("routed fleet read", func(t *testing.T) {
		t.Parallel()
		w, reads := routedReads(t)
		w.s.Routes[0].Prices.Billing = "subscription"
		w.s.Routes[0].Harness = "muse"
		rc := reads[0]
		req := ReadReq{As: rc.Row, Sel: Sel{IDs: []string{rc.ID}}, Verdict: "ok", Usage: "input=12 output=7"}
		p := Read(w.s, req)
		require.NotEmpty(t, p.Refused)
		require.Empty(t, p.Units)
		req.Usage = subscriptionRawFixture(t).String()
		p = Read(w.s, req)
		require.Empty(t, p.Refused)
		w.must(p)
		stored := cardcost.ParseUsage(w.s.Readers.Card(rc.ID).F(FieldUsage))
		require.NotNil(t, stored.Raw)
		require.Equal(t, int64(5), stored.Raw.Records[0].Tokens["cached_tokens"])
		require.Equal(t, "0", stored.MarginalUSD())
	})
}

func TestSubscriptionMalformedEnvelopeCannotMutateConsumer(t *testing.T) {
	t.Parallel()
	for _, line := range []string{
		"receipt_version=3 billing=subscription",
		"receipt_version=2 billing=subscription raw_usage=bad",
		"receipt_version=2 billing=subscription input=-1",
		"receipt_version=2 billing=subscription input=9223372036854775808",
	} {
		t.Run(line, func(t *testing.T) {
			t.Parallel()
			w := setup(t, 1)
			p := Finish(w.s, FinishReq{Usage: line})
			require.NotEmpty(t, p.Refused)
			require.Empty(t, p.Units)
			p = Read(w.s, ReadReq{Usage: line})
			require.NotEmpty(t, p.Refused)
			require.Empty(t, p.Units)
			require.Empty(t, w.s.Work.Card("s1-1").F(FieldCostTotal))
		})
	}
}

func TestSubscriptionTypedReceiptCannotClaimFriendExecutionAsFleet(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.s.Routes = []Route{{Name: "declared", Prices: cardcost.Prices{Billing: "subscription"}}}
	card := &Card{ID: "friend-read", Row: FriendRow("fake-reader"), Fields: map[string]string{FieldRoute: "declared", FieldBilling: "subscription"}}
	require.Contains(t, w.s.receiptBillingRefusal(card, subscriptionRawFixture(t).String()), "fleet execution only")
	card.Row = "m1"
	require.Empty(t, w.s.receiptBillingRefusal(card, subscriptionRawFixture(t).String()))
	// No new wire/value is admitted into the historical friend lane.
	card.Row = FriendRow("fake-reader")
	card.Fields[FieldBilling] = "plan"
	w.s.Routes[0].Prices.Billing = "plan"
	require.Empty(t, w.s.receiptBillingRefusal(card, "input=12"))
}

func TestSubscriptionLegacyMarkerCannotOverrideStampedBilling(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	for _, billing := range []string{"plan", "metered"} {
		w.s.Routes = []Route{{Name: "declared", Prices: cardcost.Prices{Billing: billing}}}
		card := &Card{Fields: map[string]string{FieldRoute: "declared", FieldBilling: billing}}
		require.Contains(t, w.s.receiptBillingRefusal(card, "billing=subscription input=12"), "original launch declaration")
		require.Empty(t, w.s.receiptBillingRefusal(card, "input=12"), "old plan bridge makes no conflicting billing claim")
		delete(card.Fields, FieldBilling)
		require.Empty(t, w.s.receiptBillingRefusal(card, "billing=subscription input=12"), "unstamped historical read marker remains compatible")
	}
}

func TestSubscriptionAbsentTokensRetainDeclaredZeroAfterRouteChanges(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	u := cardcost.NoUsage()
	u.Billing = "subscription"
	u.ReceiptVersion = 2
	for _, billing := range []string{"plan", "metered", ""} {
		w.s.Routes = []Route{{Name: "original", Prices: cardcost.Prices{Billing: billing, Input: "999"}}}
		if billing == "" {
			w.s.Routes = nil
		}
		got := cardcost.ParseUsage(costRecord(w.s, u.String(), "original", "", false, "", ""))
		require.Equal(t, "0", got.Predicted)
		require.Empty(t, got.Unpriced, "absence is not a missing-price demotion of declared subscription")
		require.Empty(t, got.ActualBy)
		require.Equal(t, cardcost.Unreported, got.Tokens.Input)
	}
}

func TestSubscriptionReturnedReadBindsReceiptBeforeDurableWrite(t *testing.T) {
	t.Parallel()
	for _, billing := range []string{"subscription", "plan", "metered"} {
		t.Run(billing, func(t *testing.T) {
			t.Parallel()
			w, reads := routedReads(t)
			rc := reads[0]
			w.s.Routes[0].Prices.Billing = billing
			w.s.Routes[0].Harness = "muse"
			rc.Fields[FieldBilling] = billing
			rc.Fields[FieldHarness] = "muse"
			good := subscriptionRawFixture(t)
			good.Billing = billing
			for i := range good.Raw.Records {
				good.Raw.Records[i].Billing = billing
			}
			missing := cardcost.NoUsage()
			missing.ReceiptVersion = 2
			missing.Billing = billing
			mismatch := subscriptionRawFixture(t)
			if billing == "subscription" {
				mismatch.Billing = "plan"
				for i := range mismatch.Raw.Records {
					mismatch.Raw.Records[i].Billing = "plan"
				}
			}
			negatives := []string{missing.String(), mismatch.String()}
			if billing == "subscription" {
				negatives = append(negatives, "input=12")
			}
			before := w.s.Work.Card("s1-1").F(FieldCostTotal)
			req := ReadReq{As: rc.Row, Sel: Sel{IDs: []string{rc.ID}}, Return: true, Reason: "no verdict yet"}
			for _, usage := range negatives {
				req.Usage = usage
				p := Read(w.s, req)
				require.NotEmpty(t, p.Refused)
				require.Empty(t, p.Units)
				require.Empty(t, rc.F(FieldReturned))
				require.Equal(t, before, w.s.Work.Card("s1-1").F(FieldCostTotal))
			}
			req.Usage = good.String()
			p := Read(w.s, req)
			require.Empty(t, p.Refused)
			w.must(p)
			rc = w.s.Readers.Card(rc.ID)
			require.NotEmpty(t, rc.F(FieldReturned))
			require.Contains(t, rc.F(FieldReadTake+"1"), "raw_usage=")
		})
	}
}

func TestSubscriptionAbsentDataDoesNotInventMeteredZero(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	for _, billing := range []string{"metered", "plan", ""} {
		w.s.Routes = []Route{{Name: "ordinary", Prices: cardcost.Prices{Billing: billing, Input: "0.3"}}}
		u := cardcost.ParseUsage(costRecord(w.s, "", "ordinary", "", false, "", ""))
		require.Empty(t, u.Predicted)
		require.Empty(t, u.Actual)
		require.Equal(t, cardcost.WhyNoTokens, u.Unpriced)
		require.Empty(t, cardcost.NoTotal().Add(u).Charged, "absence remains unknown outside explicit subscription")
	}
}
