package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDropMembersMachineDualRead is the machine record as DropMembers reads it (fleet_rows.go),
// Aspect 8 of the schema-table adoption: each form of the record — the variable table's binary
// wire, the padded standard base64 of that wire, the schema JSON text and the legacy JSON every
// writer writes today — reads the one value through the DropMembers read site, a damaged record
// is refused by name (never read as absent, empty or zero), and the reads are counted per form
// on the store's machine reader (S1, S2, S4). The store is the in-memory backend.
func TestDropMembersMachineDualRead(t *testing.T) {
	t.Parallel()

	want := Machine{State: "RUNNING", Since: time.Unix(1000, 0).UTC(), Who: "tester", RunSeq: 2}
	forms := []struct {
		name string
		form schemaread.Form
	}{{"binary", schemaread.FormBinary}, {"base64", schemaread.FormBase64}, {"json", schemaread.FormJSON}, {"legacy", schemaread.FormLegacy}}

	for _, fm := range forms {
		fm := fm
		t.Run(fm.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			mustDropSetup(t, h)
			mc := h.st.viewRows().machine
			kv, err := h.st.kv()
			require.NoError(t, err)
			require.NoError(t, kv.SetKey(h.ctx, keyMachine, string(writeMachineForm(t, mc, want, fm.form))))
			before := mc.Counts().Snapshot()
			dropped, err := h.st.DropMembers(h.ctx, []string{"m2"})
			require.NoError(t, err, "%s: a valid machine record drops the row", fm.name)
			require.Equal(t, []string{"m1"}, dropped, "%s", fm.name)
			after := mc.Counts().Snapshot()
			assert.Positive(t, after[fm.name]-before[fm.name], "%s: reads counted on their form", fm.name)
		})
	}

	t.Run("refused by name", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		mustDropSetup(t, h)
		mc := h.st.viewRows().machine
		kv, err := h.st.kv()
		require.NoError(t, err)
		// the legacy JSON every writer writes, cut short: named, never absent or zero
		require.NoError(t, kv.SetKey(h.ctx, keyMachine, `{"state":`))
		before := mc.Counts().Refused("legacy_json_unreadable")
		_, err = h.st.DropMembers(h.ctx, []string{"m2"})
		require.ErrorContains(t, err, "MACHINE REFUSED: legacy_json_unreadable")
		require.Equal(t, before+1, mc.Counts().Refused("legacy_json_unreadable"))
	})
}

// mustDropSetup puts m1 and m2 on the fleet so DropMembers keeps m2 and drops m1, reaching the
// machine read site with non-empty guards.
func mustDropSetup(t *testing.T, h *harness) {
	t.Helper()
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 1}))
	h.must(FleetStep(sprint.FleetReq{
		Op: "sync", Who: "tester", Sync: []sprint.SyncMember{{Name: "m2", Width: 1}}, Machines: []string{"m2"},
	}))
}

// writeMachineForm writes want in form: the legacy JSON the writers write today, else the
// codec's form.
func writeMachineForm(t *testing.T, mc *RowCodec[Machine], want Machine, form schemaread.Form) []byte {
	t.Helper()
	if form == schemaread.FormLegacy {
		b, err := json.Marshal(want)
		require.NoError(t, err)
		return b
	}
	b, err := mc.Write(&want, form)
	require.NoError(t, err)
	return b
}
