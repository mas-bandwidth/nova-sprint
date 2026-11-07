// Package docs exposes the role playbooks embedded in the binary.
package docs

import _ "embed"

//go:embed PLAYBOOK-WORKER.md
var worker string

//go:embed PLAYBOOK-COORDINATOR.md
var coordinator string

// Playbook returns the worker playbook first for coordinators.
func Playbook(role string) (string, bool) {
	switch role {
	case "worker":
		return worker, true
	case "coordinator":
		return worker + "\n" + coordinator, true
	default:
		return "", false
	}
}
