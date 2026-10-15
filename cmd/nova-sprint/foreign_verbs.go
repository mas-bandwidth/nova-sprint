package main

// foreignVerbs is the verbs of the tools nova-sprint does not build that the runbook
// (docs/SPRINT-COORDINATOR.md) and docs/COORDINATOR-TOOLS.md name (nova-swarm,
// nova-friend, nova-hook, nova-config, nova-bus, nova-secrets), each read once from those docs. The
// lander's E6 check (calledVerbs, registeredVerbs) holds a line that runs one of these
// tools to its list here (docs/SPEC-SPRINT.md section 7, the lander's checks), and the
// coordinator-rule tests hold the runbook's own lines to it. Their flags are not checked
// because their FlagSets live in nova-tools.
//
// The lander does not hold a landing head to this compiled table but to the one in the
// candidate tree's copy of this file, read with go/parser (foreignVerbsFromSource,
// lander-checks-docs-against-the-candidate-verbs), so it stays a plain composite literal
// of string keys and bool values: no helper call, no computed key, no spread.
var foreignVerbs = map[string]map[string]bool{
	"nova-swarm":   {"lint": true, "template": true, "disk-guard": true, "member": true, "mirror": true, "install": true},
	"nova-friend":  {"check": true, "reach": true, "pong": true, "status": true, "install": true, "run": true},
	"nova-hook":    {"hook": true, "prove": true, "unhook": true, "friends": true, "status": true, "serve": true},
	"nova-config":  {"machine": true, "friend": true, "loop": true, "apply": true, "route": true, "provider": true, "migrate": true, "sprint": true, "tier": true},
	"nova-bus":     {"log": true, "peek": true, "send": true, "recv": true},
	"nova-secrets": {"exec": true},
}
