package config

// The Redis keys apply writes, per kind. The routes and tiers sets are read
// by the deal (internal/sprint/store, the routes read); a route's row is the
// hash route:<name> and a tier's the hash tier:<name>, each name in its set.
const (
	RoutesKey = "routes"
	TiersKey  = "tiers"
)

// FriendBeatKey is a friend's own heartbeat (nova-friend): its host field
// is the machine she runs on now, what her desired slots are charged to.
func FriendBeatKey(f string) string { return "friend:" + f + ":beat" }

// RouteKey is a route's hash: its fields, name, rev and at. The deal reads
// every route of the set RoutesKey (internal/sprint/store, the routes read).
func RouteKey(name string) string { return "route:" + name }

// TierKey is a tier's hash: its route array (routes), name, rev and at. The deal
// reads its routes field with the routes (internal/sprint/store, the routes read).
func TierKey(name string) string { return "tier:" + name }

// SprintKey is the plain key one sprint field is written to:
// sprint:coordinator.
func SprintKey(field string) string { return "sprint:" + field }
