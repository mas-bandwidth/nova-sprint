package member

import (
	"github.com/mas-bandwidth/nova-sprint/pkg/cardcost"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubscriptionDurableReceiptPreservesOriginalRawRuns(t *testing.T) {
	t.Parallel()
	raw := &cardcost.RawUsage{Version: 1, Records: []cardcost.RawRecord{{Harness: "muse", Execution: "fleet", SourceSchema: "muse-jsonl-v1", ProducerVersion: "paired-fake-source", Billing: "subscription", Tokens: map[string]int64{"input_tokens": 12, "output_tokens": 7, "cached_tokens": 5, "cache_write_tokens": 3, "cache_read_tokens": 5, "reasoning_tokens": 2}}}}
	word, err := cardcost.EncodeRaw(raw)
	require.NoError(t, err)
	header := []string{"provider", "model", "tokens_in", "tokens_out", "cache_write", "cache_read", "reasoning", "usd", "receipt_version", "billing", "raw_usage"}
	row := []string{"p", "m", "12", "7", "3", "5", "2", "0", "2", "subscription", word}
	job := t.TempDir()
	text := strings.Join(header, "\t") + "\n" + strings.Join(row, "\t") + "\n" + strings.Join(row, "\t") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(job, ReceiptName), []byte(text), 0600))
	u, found, err := ReceiptUsage(job)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "0", u.Actual, "reported zero remains present")
	require.Equal(t, "harness", u.ActualBy)
	require.Equal(t, int64(24), u.Tokens.Input)
	require.Len(t, u.Raw.Records, 2, "one original raw record per run, never a sum")
	for _, r := range u.Raw.Records {
		require.Equal(t, int64(5), r.Tokens["cached_tokens"])
	}
	require.Contains(t, usageArgs(Result{Usage: u.String()}), "--receipt-contract")
	encoded, err := cardcost.EncodeSpend(u)
	require.NoError(t, err)
	replayed, err := cardcost.ParseSpendChecked(encoded)
	require.NoError(t, err)
	require.Equal(t, u.Raw, replayed.Raw)
	require.Equal(t, "0", replayed.MarginalUSD())
	require.Contains(t, usageArgs(Result{Usage: "receipt_version=999 raw_usage=bad"}), "--receipt-contract", "malformed typed receipts cannot silently fall back")
	mixedRow := append([]string(nil), row...)
	mixedRow[8], mixedRow[9], mixedRow[10] = "-", "-", "-"
	mixed := strings.Join(header, "\t") + "\n" + strings.Join(row, "\t") + "\n" + strings.Join(mixedRow, "\t") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(job, ReceiptName), []byte(mixed), 0600))
	_, found, err = ReceiptUsage(job)
	require.Error(t, err)
	require.False(t, found)
	require.Contains(t, err.Error(), "mixed legacy and typed")

	// Old rows stay legacy: a projection cannot invent original raw counters.
	legacy := strings.Join(header[:8], "\t") + "\n" + strings.Join(row[:8], "\t") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(job, ReceiptName), []byte(legacy), 0600))
	u, found, err = ReceiptUsage(job)
	require.NoError(t, err)
	require.True(t, found)
	require.Nil(t, u.Raw)
	require.Zero(t, u.ReceiptVersion)
	require.NotContains(t, usageArgs(Result{Usage: u.String()}), "--receipt-contract")
}

func TestSubscriptionDamagedTypedHeaderRetainsContract(t *testing.T) {
	t.Parallel()
	job := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(job, ReceiptName), []byte("provider\tmodel\traw_usage\n"), 0600))
	u, found, err := ReceiptUsage(job)
	require.Error(t, err)
	require.False(t, found)
	require.Equal(t, 2, u.ReceiptVersion)
	require.Nil(t, u.Raw)
	require.Equal(t, cardcost.Unreported, u.Tokens.Input)
	require.Contains(t, usageArgs(Result{Usage: u.String()}), "--receipt-contract", "damaged capable receipt cannot downgrade into legacy")
}
