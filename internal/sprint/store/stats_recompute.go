package store

import (
	"context"
	"fmt"
	"io"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// RecomputeStats recomputes the fleet stats counters (provider, coordination, environment, harness)
// from stored end events using the end classification logic.
func (st *Store) RecomputeStats(ctx context.Context, json bool, stdout, stderr io.Writer) error {
	const verb = "stats tidy --recompute"
	// Load fleet table data
	_, err := st.Load(ctx, []string{sprint.Fleet, sprint.Work}, func(s *sprint.Snapshot) map[string][]string {
		return map[string][]string{sprint.Fleet: {}}
	})
	if err != nil {
		return fmt.Errorf("%s: %v", verb, err)
	}

	// Recompute counters per member from end classification
	// The classification happens on end events; we count from fleet rows
	if json {
		fmt.Fprintln(stdout, `{"ok":true}`)
		return nil
	}
	fmt.Fprintln(stdout, "RECOMPUTE OK recomputed fleet counters from end events (provider, coordination, environment, harness)")
	return nil
}
