package store

import (
	"context"
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// TestOOMRefusalNamesTheStoreFull: an EXECABORT whose first queued command
// carries Redis's OOM refusal is unwrapped and said as the store being full,
// never as the record's size (tla/StoreRetention.tla Bounded).
func TestOOMRefusalNamesTheStoreFull(t *testing.T) {
	t.Parallel()
	cmd := redis.NewStatusCmd(context.Background())
	cmd.SetErr(errors.New("OOM command not allowed when used memory > 'maxmemory'."))
	execErr := errors.New("EXECABORT Transaction discarded because of previous errors")

	why := storeRefusal(OpRecord{}, execErr, cmd)
	require.Contains(t, why, "the store is full")
	require.Contains(t, why, "OOM")
	require.Contains(t, why, "maxmemory")
	require.NotContains(t, why, "bytes")
	require.NotContains(t, why, "EXECABORT")

	direct := storeRefusal(OpRecord{}, errors.New("OOM command not allowed when used memory > 'maxmemory'."))
	require.Contains(t, direct, "the store is full")
	require.NotContains(t, direct, "bytes")
}
