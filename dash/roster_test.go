package dash

import (
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/roster"
)

// testRoster is the roster the tests' rows come from (a row's kind and
// width are the roster's, never the store's).
var testRoster = func() roster.Roster {
	r := roster.Roster{Workers: map[string]roster.Worker{}, Kin: map[string]roster.Kin{}}
	worker := func(name string, width int) { r.Workers[name] = roster.Worker{Name: name, Width: width} }
	kin := func(name string, width int) { r.Kin[name] = roster.Kin{Name: name, Width: width} }
	worker("amd-1", 4000)
	kin("friendG", 4000)
	kin("friendA", 2)
	kin("kin", 2)
	worker("r", 8)
	worker("w", 2)
	for i := 0; i < 12; i++ {
		worker(fmt.Sprintf("m%02d", i), 16)
	}
	for i := 0; i < 4; i++ {
		kin(fmt.Sprintf("k%d", i), 32)
	}
	return r
}()
