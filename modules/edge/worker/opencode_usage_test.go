package worker

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// opencode's assistant messages, as its session database keeps them: the
// tokens by class, the model, and its own cost. A user message never reaches
// the harness (the query reads assistant rows only).
var opencodeRows = []string{
	`{"role":"assistant","providerID":"deepseek","modelID":"deepseek-flash","cost":0.0125,"tokens":{"input":1000,"output":200,"reasoning":30,"cache":{"read":5000,"write":0}}}`,
	`{"role":"assistant","providerID":"deepseek","modelID":"deepseek-flash","cost":0.0100004,"tokens":{"input":800,"output":100,"reasoning":0,"cache":{"read":6000,"write":40}}}`,
	`{"role":"assistant","providerID":"inception","modelID":"mercury-3","cost":0.001,"tokens":{"input":50,"output":10,"reasoning":0,"cache":{"read":0,"write":0}}}`,
	`not json`,
}

func TestOpenCodeUsageFromItsOwnRecord(t *testing.T) {
	t.Parallel()
	h := &OpenCodeHarness{}
	var asked string
	h.messages = func(db string) ([]string, error) { asked = db; return opencodeRows, nil }
	dir := t.TempDir()
	c, ok := h.Usage(Job{JobDir: dir})
	if !ok || c.MicroUSD != 23500 || !c.Priced {
		t.Fatalf("opencode's own cost is the charged evidence: %+v %v", c, ok)
	}
	if want := "input=1850 cache_read=11000 cache_write=40 output=310 reasoning=30 model=deepseek/deepseek-flash models=2 requests=3"; c.Usage != want {
		t.Fatalf("usage %q, want %q", c.Usage, want)
	}
	if asked != filepath.Join(dir, "data", "opencode", "opencode.db") {
		t.Fatalf("read %s", asked)
	}
	h.messages = func(string) ([]string, error) {
		return []string{`{"providerID":"deepseek","modelID":"deepseek-flash","cost":0,"tokens":{"input":7,"output":1,"reasoning":0,"cache":{"read":0,"write":0}}}`}, nil
	}
	c, ok = h.Usage(Job{JobDir: t.TempDir()})
	if !ok || c.Priced || c.MicroUSD != 0 || c.Usage != "input=7 cache_read=0 cache_write=0 output=1 reasoning=0 model=deepseek/deepseek-flash requests=1" {
		t.Fatalf("tokens with no opencode price: the tokens go, no charged figure: %+v %v", c, ok)
	}
	h.messages = func(string) ([]string, error) { return nil, nil }
	if c, ok := h.Usage(Job{JobDir: t.TempDir()}); ok {
		t.Fatalf("a run with no message is nothing to report, never a priced zero: %+v", c)
	}
	h.messages = func(string) ([]string, error) { return nil, errors.New("no sqlite3") }
	if _, ok := h.Usage(Job{JobDir: t.TempDir()}); ok {
		t.Fatal("an unreadable record is unpriced")
	}
}

// usage.json wins over opencode's record, and may carry the tokens: as a
// tokens object with a model, or as package usage's words whole.
func TestUsageFileCarriesTokens(t *testing.T) {
	t.Parallel()
	h := &OpenCodeHarness{messages: func(string) ([]string, error) { t.Fatal("usage.json is read first"); return nil, nil }}
	for body, want := range map[string]Cost{
		`{"micro_usd":950,"priced":true}`: {MicroUSD: 950, Priced: true},
		`{"micro_usd":950,"priced":true,"tokens":{"input":10,"cache_read":4,"output":2},"model":"deepseek/deepseek-flash"}`: {MicroUSD: 950, Priced: true, Usage: "input=10 cache_read=4 output=2 model=deepseek/deepseek-flash"},
		`{"usage":"input=3 output=1 model=inception/mercury-3"}`:                                                            {Usage: "input=3 output=1 model=inception/mercury-3"},
		`{"micro_usd":5,"priced":true,"usage":"input=lots"}`:                                                                {MicroUSD: 5, Priced: true},
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "usage.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		c, ok := h.Usage(Job{JobDir: dir})
		if !ok || c != want {
			t.Fatalf("%s: %+v %v, want %+v", body, c, ok, want)
		}
	}
}
