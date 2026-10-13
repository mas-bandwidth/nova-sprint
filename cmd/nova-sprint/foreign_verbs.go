package main

// foreignVerbs is the verbs of the tools nova-sprint does not build that the runbook
// (docs/SPRINT-COORDINATOR.md) and docs/COORDINATOR-TOOLS.md name (nova-swarm,
// nova-friend, nova-config, nova-bus, nova-secrets), each read once from those docs. The
// lander's E6 check (calledVerbs, registeredVerbs) holds a line that runs one of these
// tools to its list here (docs/SPEC-SPRINT.md section 7, the lander's checks), and the
// coordinator-rule tests hold the runbook's own lines to it. Their flags are not checked
// because their FlagSets live in nova-tools.
var foreignVerbs = map[string]map[string]bool{
	"nova-swarm":   {"lint": true, "template": true, "disk-guard": true, "member": true, "mirror": true, "install": true},
	"nova-friend":  {"check": true, "ping": true, "ping-install": true, "wait-pong": true, "status": true, "install": true, "run": true},
	"nova-config":  {"machine": true, "friend": true, "loop": true, "apply": true, "route": true, "provider": true, "migrate": true, "sprint": true, "tier": true},
	"nova-bus":     {"log": true, "peek": true, "send": true, "recv": true},
	"nova-secrets": {"exec": true},
}
