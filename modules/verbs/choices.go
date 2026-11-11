package verbs

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Choices persists non-secret setup answers as inactive policy data versions.
// It never activates data, so the active script stays active. An explicit
// flag is the operator answering again and is saved as a new version; the
// newest version is the answer in force. See tla/Setup.tla.
type Choices struct {
	Store store.Store
	Actor string
	Now   func() time.Time
}

func choiceName(question string) (string, error) {
	if !slices.Contains([]string{"coordinator", "dashboard", "services", "run-on"}, question) {
		return "", fmt.Errorf("invalid non-secret question")
	}
	return "setup:" + question, nil
}

// Get returns the newest durable global answer.
func (c Choices) Get(ctx context.Context, question string) (string, bool, error) {
	name, err := choiceName(question)
	if err != nil {
		return "", false, err
	}
	return c.newest(ctx, name)
}

// newest is the body of name's newest data version, read by its name
// (store.PoliciesNamed), never from the log.
func (c Choices) newest(ctx context.Context, name string) (string, bool, error) {
	versions, err := c.Store.PoliciesNamed(ctx, "data", name, 1)
	if err != nil || len(versions) == 0 {
		return "", false, err
	}
	return versions[0].Body, true, nil
}

// Effective is the answer in force for a question: the explicit flag when
// one is given, else the saved answer. stored says the value is already the
// newest saved version, so nothing needs saving.
func (c Choices) Effective(ctx context.Context, question, flag string) (string, bool, error) {
	saved, found, err := c.Get(ctx, question)
	if err != nil {
		return "", false, err
	}
	if flag != "" {
		return flag, found && saved == flag, nil
	}
	return saved, found, nil
}

// Save writes one immutable inactive data version unless the newest already
// holds value; a stale snapshot is refused so a concurrent writer must
// re-read the durable answer before asking again.
func (c Choices) Save(ctx context.Context, question, value string) error {
	name, err := choiceName(question)
	if err != nil {
		return err
	}
	if value == "" || c.Actor == "" || c.Now == nil {
		return fmt.Errorf("invalid setup answer")
	}
	snap, err := c.Store.Snapshot(ctx)
	if err != nil {
		return err
	}
	existing, found, err := c.newest(ctx, name)
	if err != nil {
		return err
	}
	if found && existing == value {
		return nil
	}
	id := consumerOpID("choice:"+c.Actor, snap.Epoch, name+"\x00"+value, int(snap.Seq))
	op := store.Op{ID: id, Epoch: snap.Epoch, Actor: c.Actor, Kind: "setup answer", At: c.Now(), Expect: []store.Expect{store.UnchangedSince{Seq: snap.Seq, Policies: true}}, Writes: []store.Write{store.PutPolicy{Kind: "data", Name: name, Body: value}}}
	r, err := applyOnce(ctx, c.Store, op, c.Store.Apply)
	if err != nil {
		return err
	}
	if !r.Accepted {
		return fmt.Errorf("setup answer refused: %s", r.Reason)
	}
	return nil
}
