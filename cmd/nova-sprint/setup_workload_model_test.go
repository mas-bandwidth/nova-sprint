package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

// tla/Setup.tla's world[9] and world[10], bound to the workload adapter
// over every state of a two-bench fleet (each bench on the bus or not, up
// in the store or not, with cores or not, its worker process beaten or not
// and, beaten, this build or an older one): world[9] is every worker bench
// on the bus, up in the store and its process this build; world[10] is
// world[9] and every worker bench with cores. WorkersReady and SmokeReady
// are exactly those, and a step that is not ready is always a
// *verbs.WorkloadNotReady (the model's repair phase), never another error.
func TestWorkloadStepsMatchTheSetupModel(t *testing.T) {
	t.Parallel()
	benches := []string{"amd-1", "amd-2"}
	for mask := 0; mask < 1<<(5*len(benches)); mask++ {
		bit := func(i int) bool { return mask&(1<<i) != 0 }
		machines := []string{`{"kind":"machine","fields":{"name":"studio","roles":"services","cores":8}}`}
		rows, present := map[string]store.Row{}, map[string]bool{}
		world9, world10 := true, true
		for i, b := range benches {
			hooked, up, cores, beaten, current := bit(5*i), bit(5*i+1), bit(5*i+2), bit(5*i+3), bit(5*i+4)
			present[b] = hooked
			if up {
				rows[b] = store.Row{Name: b, State: "up"}
				if beaten {
					rows[b] = store.Row{Name: b, State: "up", Version: map[bool]string{true: "v2.0.37", false: "v2.0.36"}[current]}
				}
			}
			fields := `"name":"` + b + `","roles":"bench"`
			if cores {
				fields += `,"cores":4`
			}
			machines = append(machines, `{"kind":"machine","fields":{`+fields+`}}`)
			world9 = world9 && hooked && up && beaten && current
			world10 = world10 && hooked && up && beaten && current && cores
		}
		f := &fleetFake{machines: strings.Join(machines, ","), rows: len(machines)}
		w := testWorkload(f, rows, present)
		w.release = "v2.0.37"
		w.pause = func(context.Context, time.Duration) error { return nil }
		for step, want := range map[int]bool{verbs.StepWorkers: world9, verbs.StepSmoke: world10} {
			ready := w.WorkersReady
			if step == verbs.StepSmoke {
				ready = w.SmokeReady
			}
			ok, err := ready(context.Background())
			var nr *verbs.WorkloadNotReady
			if ok != want || (want && err != nil) || (!want && !errors.As(err, &nr)) {
				t.Fatalf("%s step %d: ready %t, err %v; the model says world[%d] = %t", fmt.Sprintf("mask %010b", mask), step, ok, err, step, want)
			}
		}
	}
}
