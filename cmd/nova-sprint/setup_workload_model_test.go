package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

// tla/Setup.tla's world[7] and world[8], bound to the workload adapter
// over every state of a two-bench fleet (each bench on the bus or not, up
// in the store or not, with cores or not): world[7] is every worker bench
// on the bus and up in the store; world[8] is world[7] and every worker
// bench with cores. WorkersReady and SmokeReady are exactly those, and a
// step that is not ready is always a *verbs.WorkloadNotReady (the model's
// repair phase), never another error.
func TestWorkloadStepsMatchTheSetupModel(t *testing.T) {
	t.Parallel()
	benches := []string{"amd-1", "amd-2"}
	for mask := 0; mask < 1<<(3*len(benches)); mask++ {
		bit := func(i int) bool { return mask&(1<<i) != 0 }
		machines := []string{`{"kind":"machine","fields":{"name":"studio","roles":"services","cores":8}}`}
		rows, present := map[string]store.Row{}, map[string]bool{}
		world7, world8 := true, true
		for i, b := range benches {
			hooked, up, cores := bit(3*i), bit(3*i+1), bit(3*i+2)
			present[b] = hooked
			if up {
				rows[b] = store.Row{Name: b, State: "up"}
			}
			fields := `"name":"` + b + `","roles":"bench"`
			if cores {
				fields += `,"cores":4`
			}
			machines = append(machines, `{"kind":"machine","fields":{`+fields+`}}`)
			world7 = world7 && hooked && up
			world8 = world8 && hooked && up && cores
		}
		f := &fleetFake{machines: strings.Join(machines, ","), rows: len(machines)}
		w := testWorkload(f, rows, present)
		for step, want := range map[int]bool{7: world7, 8: world8} {
			ready := w.WorkersReady
			if step == 8 {
				ready = w.SmokeReady
			}
			ok, err := ready(context.Background())
			var nr *verbs.WorkloadNotReady
			if ok != want || (want && err != nil) || (!want && !errors.As(err, &nr)) {
				t.Fatalf("%s step %d: ready %t, err %v; the model says world[%d] = %t", fmt.Sprintf("mask %06b", mask), step, ok, err, step, want)
			}
		}
	}
}
