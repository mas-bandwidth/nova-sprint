package store

import (
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storelessOnce keeps -count=2 from registering the storeless codec twice; the
// process-wide registry is a package global, so one registration per run.
var storelessOnce sync.Once

// TestRowFormsListEveryRecordAtZero pins that a fresh store that has read
// nothing still lists every registered dual-read record in its CheckReport
// RowForms, each with every count zero. S4: the per-form counter is the
// evidence a later card reads before that record's writer switches, so a
// record a store has not yet read must still show on check at zero.
func TestRowFormsListEveryRecordAtZero(t *testing.T) {
	t.Parallel()

	// The test clock starts at 1000.0 seconds, never 0.
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "row-zero-"}, Actor: "tester",
		Now:   func() time.Time { return now },
		Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(t.Context()))

	rep, _, err := st.Check(t.Context(), 1)
	require.NoError(t, err)

	for _, name := range []string{
		doneRowFns().Name,
		friendEntryRowFns().Name,
		friendStatusRowFns().Name,
		beatRowFns().Name,
		strangerRowFns().Name,
	} {
		counts, ok := rep.RowForms[name]
		require.True(t, ok, "RowForms lists %s", name)
		for form, n := range counts {
			assert.Zero(t, n, "%s %s", name, form)
		}
	}
}

// TestRowFormsCountStorelessReads pins that a reader registered process-wide
// with no Store (a snapshot and restore read) has its reads merged into every
// store's RowForms. S4.
func TestRowFormsCountStorelessReads(t *testing.T) {
	t.Parallel()

	storelessOnce.Do(func() {
		c := NewRowCodec(rowCodecNoteFns())
		c.name = "STORELESSFIXTURE"
		c.RegisterProcess()

		bin, err := c.Write(&rowNote{Text: "hi"}, schemaread.FormBinary)
		require.NoError(t, err)
		_, _, err = c.Read(bin)
		require.NoError(t, err)

		_, _, err = c.Read([]byte(`{"text":"hi"}`))
		require.NoError(t, err)
	})

	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "row-sl-"}, Actor: "tester",
		Now:   func() time.Time { return now },
		Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(t.Context()))

	rep, _, err := st.Check(t.Context(), 1)
	require.NoError(t, err)

	counts, ok := rep.RowForms["STORELESSFIXTURE"]
	require.True(t, ok, "RowForms lists STORELESSFIXTURE")
	assert.Equal(t, uint64(1), counts["binary"])
	assert.Equal(t, uint64(1), counts["legacy"])

	_, ok = rep.RowForms[doneRowFns().Name]
	require.True(t, ok, "RowForms still lists DONERECORD")
}
