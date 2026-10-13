package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The check record (v1.2.17, start-refuses-until-a-clean-check; docs/SPEC-SPRINT.md section
// 14, "The card check"): every run of nova-sprint check cards writes one, the epoch's, beside
// the machine's own record (keyMachine). It says when and by whom the cards were checked,
// clean or the findings by kind, and every card checked (sprint.CheckedCard: its id, stream,
// tier, priority, needs and the sha256 of its brief) with one digest over them. A failed run
// overwrites a clean one. start reads it before the machine changes and refuses with none, a
// failed one, or a clean one whose digest is not the cards' now, recomputed from the live table
// (tla/MachineReady.tla Check, StartRefusedCheck, StartOnlyChecked). It is keyed by epoch as
// the machine's records are: one of another epoch reads as none, so a clear leaves no record
// in the new epoch (tla/MachineReady.tla ClearAdvance, recFresh).

const keyCheckRecord = "cardcheck" // STRING, the check record (JSON)

// CheckRecord is the check record.
type CheckRecord struct {
	Epoch uint64    `json:"epoch"`
	At    time.Time `json:"at"`
	By    string    `json:"by"`
	Clean bool      `json:"clean"`
	// Streams is the streams a check limited by --stream covered; none is every stream. A
	// limited check never opens the start gate (it did not see every card).
	Streams []string `json:"streams,omitempty"`
	// Kinds is the findings by kind (sprint.CardCheckKinds), "" when clean; Findings their
	// lines, at most MaxCheckFindings of them.
	Kinds    string               `json:"kinds,omitempty"`
	Findings []string             `json:"findings,omitempty"`
	Cards    []sprint.CheckedCard `json:"cards"`
	Digest   string               `json:"digest"`
}

// MaxCheckFindings bounds the finding lines a check record keeps (Kinds counts them all).
const MaxCheckFindings = 200

// CheckRecordOf is the epoch's check record of a stored value, and whether there is one: none
// when absent, unreadable, or another epoch's.
func CheckRecordOf(raw string, ok bool, epoch uint64) (CheckRecord, bool) {
	var r CheckRecord
	if !ok || json.Unmarshal([]byte(raw), &r) != nil || r.Epoch != epoch {
		return CheckRecord{}, false
	}
	return r, true
}

// CardCheck reads the epoch's check record: false when there is none for this epoch.
func (st *Store) CardCheck(ctx context.Context) (CheckRecord, bool, error) {
	st, err := st.pin(ctx)
	if err != nil {
		return CheckRecord{}, false, err
	}
	kv, err := st.kv()
	if err != nil {
		return CheckRecord{}, false, err
	}
	raw, ok, err := kv.GetKey(ctx, keyCheckRecord)
	if err != nil {
		return CheckRecord{}, false, err
	}
	r, ok := CheckRecordOf(raw, ok, st.epoch)
	return r, ok, nil
}

// PutCardCheck writes the epoch's check record, over any before it: r is stamped with the
// pinned epoch, and its findings cut to MaxCheckFindings.
func (st *Store) PutCardCheck(ctx context.Context, r CheckRecord) (CheckRecord, error) {
	st, err := st.pin(ctx)
	if err != nil {
		return r, err
	}
	r.Epoch = st.epoch
	if len(r.Findings) > MaxCheckFindings {
		r.Findings = r.Findings[:MaxCheckFindings]
	}
	return r, st.putJSON(ctx, keyCheckRecord, r)
}
