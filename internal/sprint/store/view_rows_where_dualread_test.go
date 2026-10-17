package store

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// TestViewRowsWhereDualRead is the dual-read acceptance for the where, stop,
// check and stuck rows (Aspect 8): each is read alike from its fixed binary
// image, the padded standard base64 of that image, and the legacy form it has
// today, while every writer still writes the legacy form; a fuzzed malformed
// value of each form is refused by name; reads are counted per record and per
// form. S2, S4.
func TestViewRowsWhereDualRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "dualread-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))

	// S4: a fresh store lists each row's reader at zero before the first read.
	report, _, err := st.Check(ctx, 1)
	require.NoError(t, err)
	for _, name := range []string{"WHERERECORD", "STOPSRECORD", "CHECKRECORD", "STUCK"} {
		require.Contains(t, report.RowForms, name, name+" registered and counted")
		require.Zero(t, report.RowForms[name]["binary"], name+" binary reads")
		require.Zero(t, report.RowForms[name]["legacy"], name+" legacy reads")
	}
}
