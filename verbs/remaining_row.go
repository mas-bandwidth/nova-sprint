package verbs

import "context"

// Row configuration lives in nova-config; hooks create sprint state rows.
func (e *Engine) row(_ context.Context, args []string) Result {
	if len(args) == 0 || args[0] != "add" {
		return refused("row", "invalid arguments", "nova-sprint help row")
	}
	return refused("row add", "row configuration is managed by nova-config", "set it in nova-config, then nova-sprint hook --as <name> --role worker")
}
