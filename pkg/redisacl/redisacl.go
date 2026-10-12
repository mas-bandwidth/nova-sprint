// Package redisacl renders the fleet store's ACL users from the function
// library a build embeds and the key families its tools use, and compares a
// rendering with a store's live ACL. It is what `nova-redis acl` runs and
// what fleet/redis.yml converges a store with (docs/FLEET.md, "The store's
// ACL").
//
// A role is declared once, here: its user, the key families it reads or
// writes, the command categories it holds and the library files whose
// functions it may call. The function names are never listed: they are read
// out of the library (redisfn.Library.Registered) at render time, FCALL for
// each and FCALL_RO for each the library flags no-writes, so a function added
// to a file reaches every role of that file at the next render, and a file
// added to the library is refused until a role names it or it is declared the
// coordinator's alone (TestEveryLibraryFileHasItsRoles).
package redisacl

import (
	"fmt"
	"maps"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/pkg/redisfn"
)

// The roles, by the name the CLI prints.
const (
	Coordinator = "coordinator"
	Member      = "member"
	Table       = "table"
	Friend      = "friend"
)

// Family is one family of keys the tools use, with the code that names
// them; render prints each with its source (ACL FAMILY lines).
type Family struct {
	Name     string
	Patterns []string
	From     string
}

// Families are the key families of the fleet store, each held to its
// owner's key functions by TestFamiliesAreTheOwnersKeys. A key a table cell
// is bound to (a binding may name any key outside table:) is in none of
// them unless it falls in one: only the coordinator reads such a key.
var Families = []Family{
	{"tables", []string{"table:*", "tables"}, "pkg/ntable DefKey, ChangesKey, RowsKeyAt, Registry"},
	{"views", []string{"view:*", "views"}, "pkg/ntable: a view's key and the views registry"},
	{"sprint", []string{"sprint:*"}, "internal/sprint Names.Key: the epoch, the beats, the tick's keys"},
	{"machines", []string{"machine:*", "machines"}, "pkg/config MachineKey, MachineCeilingKey, MachinesKey"},
	{"beats", []string{"bench:*"}, "pkg/config BeatKey: a machine's measured facts"},
	{"friends", []string{"friend:*", "friends", "friends:*"}, "pkg/config FriendBeatKey, FriendsKey: a friend's beat, desired slots, width, mode and roles"},
	{"fleet", []string{"fleet:*"}, "pkg/config FleetKey"},
	{"loops", []string{"loops", "loop:*"}, "pkg/config LoopsKey, LoopKey"},
	{"routes", []string{"routes", "route:*"}, "pkg/config RoutesKey, RouteKey"},
	{"config", []string{"config:decl"}, "pkg/config DeclKey"},
	{"tokens", []string{"tokens:ledger:*"}, "internal/record LedgerPrefix: nova-tokens ledger and report, logged in as the seat's user"},
	{"events", []string{"ev:github"}, "internal/ghevent/wire Stream: the CI run receipt (nova-ci github receipt, tools/ci/reportrun.go) XADDs it as the bench user"},
}

// keys is the key rules for families by access: "rw" families as ~<pattern>,
// "r" families as %R~<pattern>, in the order of Families.
func keys(access map[string]string) []string {
	var out []string
	for _, f := range Families {
		for _, p := range f.Patterns {
			switch access[f.Name] {
			case "rw":
				out = append(out, "~"+p)
			case "r":
				out = append(out, "%R~"+p)
			}
		}
	}
	return out
}

// The command rules, applied in order. A writer holds the data categories
// and loses every dangerous command and all of scripting (EVAL, SCRIPT,
// FUNCTION, FCALL) before its functions are granted one by one, so no role
// but the coordinator can load, delete or flush the library. A reader holds
// the read category only.
var (
	writerCommands = []string{"-@all", "+@read", "+@write", "+@keyspace", "+@connection", "+@transaction", "-@dangerous", "-@scripting"}
	readerCommands = []string{"-@all", "+@read", "+@connection", "-@dangerous", "-@scripting"}
)

// CoordinatorOnly are the library files no role but the coordinator calls:
// nova-config's apply writes the capacity and the friends' roles through
// them.
var CoordinatorOnly = []string{"lua/capacity.lua", "lua/friend_roles.lua"}

// Role is one ACL user's declaration.
type Role struct {
	// Name is the role; User the ACL user that holds it.
	Name, User string
	// Keys are the key rules: ~<pattern> to read and write, %R~<pattern> to
	// read.
	Keys []string
	// Commands are the command rules before the functions.
	Commands []string
	// Files are the library files whose functions it may call; every file
	// when All.
	Files []string
	All   bool
	// ReadOnly grants only the no-writes functions, through FCALL_RO.
	ReadOnly bool
	// Extra are command rules after the functions.
	Extra []string
}

// Roles are the four roles, in the order they render. The users are the
// tools' own defaults: the member's is the bench user every member loop
// logs in as (redisauth.NoUserHint), the table reader's and the friend's the
// ns- users the tools already name.
func Roles() []Role {
	member := []string{"lua/00_ping.lua", "lua/table.lua"}
	// Every store verb lists the library before it calls it (fn.LoadMissing,
	// the sprint store's open): FUNCTION LIST is every role's.
	list := []string{"+function|list"}
	read := map[string]string{"tables": "r", "views": "r", "sprint": "r", "machines": "r", "beats": "r", "friends": "r", "fleet": "r", "loops": "r", "routes": "r", "config": "r", "tokens": "r", "events": "r"}
	with := func(over map[string]string) map[string]string {
		out := maps.Clone(read)
		maps.Copy(out, over)
		return out
	}
	return []Role{
		{Name: Coordinator, User: "coordinator", Keys: []string{"~*"}, Commands: writerCommands, All: true,
			Extra: []string{"+function|load", "+function|list"}},
		{Name: Member, User: "bench", Keys: keys(with(map[string]string{"tables": "rw", "views": "rw", "sprint": "rw", "beats": "rw", "tokens": "rw", "events": "rw"})),
			Commands: writerCommands, Files: member, Extra: list},
		{Name: Table, User: "ns-table", Keys: keys(read), Commands: readerCommands, Files: []string{"lua/table.lua"}, ReadOnly: true, Extra: list},
		{Name: Friend, User: "ns-friend", Keys: keys(with(map[string]string{"tables": "rw", "views": "rw", "sprint": "rw", "friends": "rw", "tokens": "rw"})),
			Commands: writerCommands, Files: member, Extra: list},
	}
}

// User is one rendered ACL user: the arguments of ACL SETUSER after its
// name. A rendering never touches a password.
type User struct {
	Role, Name string
	Rules      []string
	// Functions is how many library functions it may call.
	Functions int
}

// Render is every role's user for the library. A role naming a file the
// library does not have is refused: a renamed file must not leave a role
// silently without its functions.
func Render(lib redisfn.Library) ([]User, error) {
	fns, err := lib.Registered()
	if err != nil {
		return nil, err
	}
	luaCommands, err := LuaCommands(lib)
	if err != nil {
		return nil, err
	}
	files := map[string]bool{}
	for _, f := range fns {
		files[f.File] = true
	}
	var out []User
	for _, r := range Roles() {
		wanted := map[string]bool{}
		for _, f := range r.Files {
			if !files[f] {
				return nil, fmt.Errorf("role %s names library file %s, which registers no function in this build", r.Name, f)
			}
			wanted[f] = true
		}
		rules := append([]string{"on", "clearselectors", "resetkeys", "resetchannels"}, r.Keys...)
		rules = append(rules, r.Commands...)
		n := 0
		for _, f := range fns {
			if !r.All && !wanted[f.File] {
				continue
			}
			switch {
			case r.ReadOnly && !f.NoWrites:
				continue
			case r.ReadOnly:
				rules = append(rules, "+fcall_ro|"+f.Name)
			case f.NoWrites:
				rules = append(rules, "+fcall|"+f.Name, "+fcall_ro|"+f.Name)
			default:
				rules = append(rules, "+fcall|"+f.Name)
			}
			n++
		}
		// The commands the role's functions run, under its ACL: each named
		// after the categories so no removal above takes it back.
		called := map[string]bool{}
		for f, cmds := range luaCommands {
			if r.All || wanted[f] {
				for _, c := range cmds {
					called[c] = true
				}
			}
		}
		for _, c := range slices.Sorted(maps.Keys(called)) {
			rules = append(rules, "+"+c)
		}
		rules = append(rules, r.Extra...)
		out = append(out, User{Role: r.Name, Name: r.User, Rules: rules, Functions: n})
	}
	return out, nil
}


