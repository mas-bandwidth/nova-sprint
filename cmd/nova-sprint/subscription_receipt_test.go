package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// The identical argv is used locally and by the server's forwarding dispatcher.
// On the previous tree receipt-contract is an unknown flag; on this tree its
// invalid envelope is refused by the checked codec before store access.
func TestSubscriptionReceiptCompatibilityFenceBeforeStore(t *testing.T) {
	t.Parallel()
	for _, verb := range []string{"finish", "read"} {
		t.Run(verb, func(t *testing.T) {
			t.Parallel()
			args := []string{verb, "--as", "m1", "--receipt-contract", "2", "--usage", "receipt_version=2 billing=subscription raw_usage=bad"}
			if verb == "read" {
				args = append(args, "--ok")
			}
			args = append(args, "p1.w1@1")
			a := newApp(func(string) string { return "" })
			backendCalls := 0
			a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) {
				backendCalls++
				return nil, fmt.Errorf("unexpected store access")
			}
			a.now = func() time.Time { return time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC) }
			var out, err bytes.Buffer
			require.Equal(t, 2, a.run(args, &out, &err))
			require.Contains(t, err.String(), "receipt")
			require.NotContains(t, err.String(), "dial")
			forwarded := serveOne(a, false, args...)
			require.Equal(t, 2, forwarded.Code)
			require.Contains(t, forwarded.Stderr, "receipt")
			require.NotContains(t, forwarded.Stderr, "dial")
			require.Zero(t, backendCalls, "both parser paths refuse before opening the fake store")
			t.Logf("local refusal: %s; forwarded refusal: %s", err.String(), forwarded.Stderr)
		})
	}
}
