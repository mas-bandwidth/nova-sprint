// Package oprowswire is the sprint store's operation rows as fixed tables, generated
// by the pinned schema compiler from schemas/oprows/oprows.schema: the operation
// journal header and its parts, the held lock, the notes and note lines, the timers,
// the cells and moved members of a change event, the engine's request digest and
// step result and the teardown member list. Every other .go file here is generated:
// never edit them; edit the schema and run go generate ./internal/sprint/store/oprowswire.
// The compiler is pinned by exact commit (mas-bandwidth/schema 4d9941fbc7fd, main on
// 2026-10-15); go run fetches it, so generating needs the network and the build does not.
package oprowswire

//go:generate go run github.com/mas-bandwidth/schema/v2/cmd/schema@v2.5.1-0.20261014163537-4d9941fbc7fd generate --lang go --out . ../../../../schemas/oprows
