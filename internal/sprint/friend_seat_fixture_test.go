package sprint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// workerSeatFixture explicitly supplies the worker evidence intended by legacy
// presence fixtures. Production seats obtain this fact from the hook or member
// heartbeat; Status alone never supplies it outside this test constructor.
func workerSeatFixture(f FriendSeat) FriendSeat {
	f.WorkerLive = f.Status == Up
	return f
}

func workerSeatFixtures(seats []FriendSeat) []FriendSeat {
	for i := range seats {
		seats[i] = workerSeatFixture(seats[i])
	}
	return seats
}

// rememberWorkerSeats records supplied evidence without forgetting fixture friends
// omitted from a later partial request. Repeated names replace the old observation.
func rememberWorkerSeats(s *Snapshot, seats []FriendSeat) {
	for _, seat := range seats {
		found := false
		for i := range s.Friends {
			if s.Friends[i].Name == seat.Name {
				s.Friends[i] = seat
				found = true
				break
			}
		}
		if !found {
			s.Friends = append(s.Friends, seat)
		}
	}
}

func tickDealFixture(s *Snapshot, r TickReq) (Plan, int) {
	rememberWorkerSeats(s, r.Friends)
	return TickDeal(s, r)
}

func friendLevelFixture(s *Snapshot, r FriendLevelReq) Plan {
	rememberWorkerSeats(s, r.Seats)
	return FriendLevel(s, r)
}

// New fixtures must name their worker assumption. Negative proof fixtures set
// WorkerLive false explicitly, so the guard has no per-file exemptions.
func TestFriendSeatFixturesDeclareWorkerEvidence(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		fs := token.NewFileSet()
		file, err := parser.ParseFile(fs, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && (id.Name == "workerSeatFixture" || id.Name == "workerSeatFixtures") {
					return false
				}
			}
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			typ := lit.Type
			if a, ok := typ.(*ast.ArrayType); ok {
				typ = a.Elt
			}
			id, ok := typ.(*ast.Ident)
			if !ok || id.Name != "FriendSeat" {
				return true
			}
			// Reject literals with a status but without an explicit worker declaration,
			// including a variable status that can later be Up.
			var inspect func(*ast.CompositeLit)
			inspect = func(l *ast.CompositeLit) {
				status, proof := false, false
				for _, e := range l.Elts {
					if inner, ok := e.(*ast.CompositeLit); ok {
						inspect(inner)
						continue
					}
					kv, ok := e.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok {
						continue
					}
					status = status || key.Name == "Status"
					proof = proof || key.Name == "WorkerLive"
				}
				if status && !proof {
					t.Errorf("%s: FriendSeat status fixture needs workerSeatFixture or explicit WorkerLive evidence", fs.Position(l.Pos()))
				}
			}
			inspect(lit)
			return false
		})
	}
}
