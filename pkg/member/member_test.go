package member

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestARateLimitedRunIsAProviderFailure checks that a rate-limited EndProvider with
// no pushed head is reported as `provider failure:` whatever its shape (tla/CardContract.tla,
// Judge). A shaped result with a pushed head stays the card's own.
func TestARateLimitedRunIsAProviderFailure(t *testing.T) {
	t.Parallel()

	// Rate-limited with no pushed head, no shape: provider failure
	r := Result{
		End:      EndProvider,
		Provider: "provider: class=rate-limited status=429 msg=Too many requests",
		Shaped:   false,
	}
	fin, why := Judge(r, Push{})
	require.Equal(t, FinishFailed, fin)
	assert.Equal(t, EndProvider+": provider: class=rate-limited status=429 msg=Too many requests", why)

	// Rate-limited with no pushed head, with shape: provider failure (push not accepted)
	r = Result{
		End:      EndProvider,
		Provider: "provider: class=rate-limited status=429 msg=Too many requests",
		Shaped:   true,
	}
	fin, why = Judge(r, Push{})
	require.Equal(t, FinishFailed, fin)
	assert.Equal(t, EndProvider+": provider: class=rate-limited status=429 msg=Too many requests", why)

	// Rate-limited with pushed head, shaped, verdict ok: stays the card's own
	r = Result{
		End:      EndProvider,
		Provider: "provider: class=rate-limited status=429 msg=Too many requests",
		Shaped:   true,
		Verdict:  "ok",
	}
	fin, why = Judge(r, Push{Sha: "abc123"})
	require.Equal(t, FinishOK, fin)
	assert.Equal(t, "", why)

	// Non-rate-limited with no pushed head, no shape: provider failure
	r = Result{
		End:      EndProvider,
		Provider: "provider: class=provider-5xx status=503 msg=Service unavailable",
		Shaped:   false,
	}
	fin, why = Judge(r, Push{})
	require.Equal(t, FinishFailed, fin)
	assert.Equal(t, EndProvider+": provider: class=provider-5xx status=503 msg=Service unavailable", why)

	// Non-rate-limited with pushed head, shaped, verdict ok: stays the card's own
	r = Result{
		End:      EndProvider,
		Provider: "provider: class=provider-5xx status=503 msg=Service unavailable",
		Shaped:   true,
		Verdict:  "ok",
	}
	fin, why = Judge(r, Push{Sha: "abc123"})
	require.Equal(t, FinishOK, fin)
	assert.Equal(t, "", why)

	// Non-rate-limited with no pushed head, shaped: provider failure (push not accepted)
	r = Result{
		End:      EndProvider,
		Provider: "provider: class=provider-5xx status=503 msg=Service unavailable",
		Shaped:   true,
		Verdict:  "ok",
	}
	fin, why = Judge(r, Push{})
	require.Equal(t, FinishFailed, fin)
	assert.Equal(t, EndProvider+": provider: class=provider-5xx status=503 msg=Service unavailable", why)
}
