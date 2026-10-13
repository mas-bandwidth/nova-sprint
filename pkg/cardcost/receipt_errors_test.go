package cardcost

import (
	"encoding/base64"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestCheckedReceiptErrorsDoNotEchoUntrustedFields(t *testing.T) {
	t.Parallel()
	const marker = "synthetic-secret-marker"
	for _, parse := range []struct {
		name   string
		call   func(string) (Usage, error)
		prefix string
	}{
		{"usage-version", ParseUsageChecked, "receipt_version="},
		{"spend-version", ParseSpendChecked, "receipt_version:"},
	} {
		t.Run(parse.name, func(t *testing.T) {
			_, err := parse.call(parse.prefix + marker)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), marker)
		})
	}
	rawObject := &RawUsage{Version: RawVersion, Records: []RawRecord{{Harness: "muse", Execution: "fleet", SourceSchema: "muse-jsonl-v1", ProducerVersion: "fixture", Billing: BillingSubscription, Tokens: map[string]int64{marker: 0}}}}
	_, err := EncodeRaw(rawObject)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), marker)
	valid := `{"version":1,"records":[{"harness":"muse","execution":"fleet","source_schema":"muse-jsonl-v1","producer_version":"fixture","billing":"subscription","tokens":{"input_tokens":0}}]}`
	for name, raw := range map[string]string{
		"unknown-counter": strings.Replace(valid, "input_tokens", marker, 1),
		"unknown-field":   strings.Replace(valid, "source_schema", marker, 1),
		"wrong-type":      strings.Replace(valid, `"version":1`, `"version":"`+marker+`"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeRaw(base64.RawURLEncoding.EncodeToString([]byte(raw)))
			require.Error(t, err)
			assert.NotContains(t, err.Error(), marker)
		})
	}
}
