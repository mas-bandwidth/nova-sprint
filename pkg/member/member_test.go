package member

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestARateLimitedRunIsAProviderFailure(t *testing.T) {
	t.Parallel()

	t.Run("shaped with rate-limited is provider failure", func(t *testing.T) {
		t.Parallel()
		r := Result{
			End:    cardhdr.EndProvider,
			Provider: "provider: class=rate-limited status=429 msg=Too many requests",
			Shaped: true,
			Head:   "",
		}
		fin, why := Judge(r, Push{})
		require.Equal(t, FinishFailed, fin)
		assert.Contains(t, why, cardhdr.EndProvider)
		assert.Contains(t, why, "rate-limited")
	})

	t.Run("unshaped with rate-limited is provider failure", func(t *testing.T) {
		t.Parallel()
		r := Result{
			End:    cardhdr.EndProvider,
			Provider: "provider: class=rate-limited status=429 msg=Too many requests",
			Shaped: false,
		}
		fin, why := Judge(r, Push{})
		require.Equal(t, FinishFailed, fin)
		assert.Contains(t, why, cardhdr.EndProvider)
		assert.Contains(t, why, "rate-limited")
	})

	t.Run("rate-limited takes precedence over shape", func(t *testing.T) {
		t.Parallel()
		r := Result{
			End:    cardhdr.EndProvider,
			Provider: "provider: class=rate-limited status=429 msg=Too many requests",
			Shaped: false,
			Head:   "abc123",
		}
		fin, why := Judge(r, Push{})
		require.Equal(t, FinishFailed, fin)
		assert.Contains(t, why, cardhdr.EndProvider)
		assert.Contains(t, why, "rate-limited")
	})
}
