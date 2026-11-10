package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A provider's rate limit is the bench's fault, never the card's fail: the
// card goes back to the queue without spending an attempt, and the reason
// names the model so the deal rests it.
func TestRateLimitIsFaultNotFail(t *testing.T) {
	t.Parallel()
	h := &ClaudeHarness{}
	const model = "inception/mercury-3"
	tail := []byte("Error: Rate limit reached: input token limit exceeded")

	j := Job{Consumer: "p1.w1", Model: model, JobDir: t.TempDir()}
	if out := ClassifyJob(h, j, 1, time.Second, 0, tail); out.Result != "fault" || !strings.Contains(out.Reason, model) {
		t.Fatalf("a rate-limited exit with no result.json must be a fault naming %s, got %+v", model, out)
	}

	jf := Job{Consumer: "p1.w2", Model: model, JobDir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(jf.JobDir, "result.json"), []byte(`{"result":"fail","reason":"429 Too Many Requests from the provider"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := ClassifyJob(h, jf, 1, time.Second, 0, nil); out.Result != "fault" || !strings.Contains(out.Reason, model) {
		t.Fatalf("a fail whose reason is a rate limit must be a fault naming %s, got %+v", model, out)
	}

	jo := Job{Consumer: "p1.w3", Model: model, JobDir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(jo.JobDir, "result.json"), []byte(`{"result":"fail","reason":"tests fail"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := ClassifyJob(h, jo, 1, time.Second, 0, tail); out.Result != "fail" {
		t.Fatalf("a fail whose reason is not a rate limit stays the card's fail, got %+v", out)
	}
}
