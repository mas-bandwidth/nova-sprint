REPO: mas-bandwidth/nova-sprint
BASE: sprint/v17
PATHS: cmd/nova-sprint/landgo.go,cmd/nova-sprint/land.go,cmd/nova-sprint/landpass.go,internal/sprint/state.go

THE TASK.
1. In internal/sprint/state.go add `func (s *Snapshot) GateMembers() []string`: the fleet members in row order whose status is up, or that are held by the coordinator (held set, held_by empty, no fault_since). A member that is down and not held, or held by a machine mark (fault, sync, probe, adopt: any non-empty held_by), is left out. Give it a comment naming the deal filter model NeverDealtToDraining as the deal filter it is not. UpMembers is unchanged.
2. In landgo.go `gateBenches` use `s.GateMembers()` instead of `s.UpMembers()`; everything else there (this machine dropped, bench.

STEP 1. (the rest of the brief is cut from this fixture)
