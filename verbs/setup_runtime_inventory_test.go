package verbs

import (
	"context"
	"testing"
)

func TestFleetMachinesReadsActualRoles(t *testing.T) {
	t.Parallel()
	raw := `{"result":{"verb":"machine list","status":"ok","exit":0},"facts":{"kind":"machine","rows":2},"items":[{"kind":"machine","fields":{"name":"b","address":"b.example","roles":"bench,runner","cores":16}},{"kind":"machine","fields":{"name":"a","address":"","roles":"services"}}]}`
	f := FleetServices{Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "nova-config" || len(args) != 3 || args[0] != "machine" || args[1] != "list" || args[2] != "--json" {
			t.Fatalf("wrong command %s %v", name, args)
		}
		return []byte(raw), nil
	}}
	got, err := f.Machines(context.Background())
	if err != nil || len(got) != 2 || got[0].Name != "a" || got[1].Roles[0] != "bench" || got[1].Cores == nil || *got[1].Cores != 16 {
		t.Fatal(got, err)
	}
}
func TestFleetMachinesRejectsInvalidResponse(t *testing.T) {
	t.Parallel()
	f := FleetServices{Command: func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{"result":{"verb":"machine list","status":"ok"},"facts":{"kind":"machine","rows":2},"items":[]}`), nil
	}}
	if _, err := f.Machines(context.Background()); err == nil {
		t.Fatal("accepted inconsistent result")
	}
}
func TestSprintSeatsUseServiceSeatAndKeepPublicBench(t *testing.T) {
	t.Parallel()
	machines := []FleetMachine{{Name: "studio", Roles: []string{"services"}}, {Name: "amd-1", Roles: []string{"bench"}}, {Name: "public", Roles: []string{"services"}}}
	seats, err := SprintSeats(machines, FleetService{Machine: "studio", Seat: "studio"}, "public")
	if err != nil || len(seats) != 2 || seats[0] != (SprintSeat{Seat: "studio", Machine: "studio"}) || seats[1] != (SprintSeat{Seat: "amd-1", Machine: "amd-1"}) {
		t.Fatal(seats, err)
	}
	if _, err = SprintSeats(machines, FleetService{Machine: "public", Seat: "public"}, "public"); err == nil {
		t.Fatal("public host accepted")
	}
	// A bench that serves the public board works cards too: it keeps its seat.
	machines[2].Roles = []string{"bench"}
	seats, err = SprintSeats(machines, FleetService{Machine: "studio", Seat: "studio"}, "public")
	if err != nil || len(seats) != 3 || seats[2] != (SprintSeat{Seat: "public", Machine: "public"}) {
		t.Fatal(seats, err)
	}
}
func TestPublicDashboardRejectsCredentials(t *testing.T) {
	t.Parallel()
	service := &FleetService{Name: "sprint-public", Machine: "public", Argv: []string{"/bin/nova-sprint", "dash", "--pull", "http://private:8080", "--listen", ":443"}}
	if !validPublicTarget(service) {
		t.Fatal("valid pull service refused")
	}
	service.Keys = []string{sprintPasswordName}
	service.Seat = "swarm-public"
	if validPublicTarget(service) {
		t.Fatal("public service received a credential")
	}
	service.Keys = nil
	service.Seat = ""
	service.Env = map[string]string{"NOVA_PG_DSN": "postgres://private/db"}
	if validPublicTarget(service) {
		t.Fatal("public service received store configuration")
	}
}

func TestPublicDashboardEnvHasExactProgram(t *testing.T) {
	t.Parallel()
	service := &FleetService{Name: "sprint-public", Machine: "public", Argv: []string{"/usr/bin/env", "nova-sprint", "dash", "--pull", "http://private:8080", "--listen", ":8080"}}
	if !validPublicTarget(service) {
		t.Fatal("env-backed pull service refused")
	}
	service.Argv[1] = "other-program"
	if validPublicTarget(service) {
		t.Fatal("generic env wrapper accepted")
	}
}
