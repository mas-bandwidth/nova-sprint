package sprintdash

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestThePageDataCarriesTheServersVersion (rewritten from the owner-parity file): the page's
// data carries server_version — where's own when it prints one, else the listening process's
// version, else none (live/SPEC.md and server.py's server_version, the owner 2026-10-12).
func TestThePageDataCarriesTheServersVersion(t *testing.T) {
	t.Parallel()
	version := func(body []byte, v string) (string, bool) {
		r := newRig(t)
		r.s.Version = v
		r.next = func() ([]byte, error) { return body, nil }
		r.api()
		var env map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(r.s.PageSnapshot(), &env))
		_, top := env["server_version"]
		assert.False(t, top, "the envelope has no server_version key of its own")
		var data struct {
			ServerVersion *string `json:"server_version"`
		}
		require.NoError(t, json.Unmarshal(env["data"], &data))
		if data.ServerVersion == nil {
			return "", false
		}
		return *data.ServerVersion, true
	}
	body := where(0)
	got, ok := version(body, "v1.2.16")
	assert.True(t, ok)
	assert.Equal(t, "v1.2.16", got, "where gives none: the listening process's")
	withOwn := bytes.Replace(body, []byte(`"at":`), []byte(`"server_version":"v1.2.17","at":`), 1)
	got, _ = version(withOwn, "v1.2.16")
	assert.Equal(t, "v1.2.17", got, "where's own wins")
	_, ok = version(body, "")
	assert.False(t, ok, "neither known: none, and the chip hides")
}
