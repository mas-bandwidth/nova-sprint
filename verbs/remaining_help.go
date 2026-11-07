package verbs

import (
	"context"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/docs"
)

func (e *Engine) help(_ context.Context, args []string) Result {
	groups := map[string]string{
		"getting-started": "next, check, up, hook, unhook, stream, add, row",
		"work":            "run, worker, take, finish, read, report, release",
		"coordinate":      "show, judge, priority, edit, drop, hold, unhold, policy",
		"operate":         "stop, start, dash, backup, restore, clear, version",
	}
	index := "nova-sprint commands:\n  getting-started: " + groups["getting-started"] + "\n  work: " + groups["work"] + "\n  coordinate: " + groups["coordinate"] + "\n  operate: " + groups["operate"] + "\nnext: nova-sprint\n"
	if len(args) == 0 || len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		return Result{index, 0}
	}
	if len(args) == 2 && args[0] == "playbook" {
		if text, ok := docs.Playbook(args[1]); ok {
			return Result{text + "\nnext: nova-sprint\n", 0}
		}
		return refused("help", "unknown playbook "+args[1], "nova-sprint help playbook coordinator|worker")
	}
	if len(args) > 2 {
		return refused("help", "invalid arguments", "nova-sprint help")
	}
	key := strings.Join(args, " ")
	if len(args) == 1 {
		if group, ok := groups[key]; ok {
			return Result{key + ": " + group + "\nnext: nova-sprint\n", 0}
		}
	}
	usage := helpUsage()
	if u, ok := usage[key]; ok {
		return Result{strings.TrimSpace("nova-sprint "+key+" "+u) + "\nnext: nova-sprint\n", 0}
	}
	return Result{index, 0}
}
func helpUsage() map[string]string {
	return map[string]string{
		"stop": "--reason <why>", "start": "", "hook": "--as <name> --role coordinator|worker", "unhook": "[<name>] --as <you>", "next": "[--as <name>]", "check": "[--history | --integrity]", "up": "[--as <coordinator>]", "stream": "add <name> --repo <owner/name> | set <name> --mode auto|group|hand", "stream add": "<name> --repo <owner/name> [--base main] [--pr]", "stream set": "<name> --mode auto|group|hand", "row": "configuration is in nova-config; hook records a session", "row add": "configuration is in nova-config; hook records a session",
		"add": "--stream <name> --brief-file <file> [--dry-run]", "run": "[--listen <addr>] [--shadow]", "worker": "--as <row>", "take": "<card>@<gen> --as <row>", "finish": "<card>@<gen> --head <sha> --as <row>", "read": "<card>@<gen> yes|fix --as <row>", "report": "<card>@<gen> --result ok|fail|fault --reason <why> --as <row>", "release": "<card>@<gen> --as <row> --reason <why> [--epoch <n>] | --row <row> --all --reason <why>", "show": "[<card>|<stream>|<row>|run] [--stitch | --ci]", "judge": "[<id> <choice> --reason <why>]", "priority": "<card> <level> --reason <why>", "edit": "<card> --brief-file <file> --reason <why>", "drop": "<id> --reason <why> [--waive | --relink <other>] [--expect <dependant-ids>] [--dry-run]", "hold": "<row|stream>... --reason <why> [--dry-run]", "unhold": "<row|stream>... --reason <why> [--dry-run]", "policy": "set|activate|show|diff|check", "policy set": "<file> --reason <why> [--as <coordinator>]", "policy activate": "<version> --reason <why> [--as <coordinator>]", "policy show": "[<version> | --list] [--as <coordinator>]", "policy diff": "<version|default> [<version|default>] [--as <coordinator>]", "policy check": "<file|version> [--as <coordinator>]", "dash": "--listen <addr> [--pull <tailnet addr>]", "backup": "[--out <dir>]", "restore": "<file>", "clear": "--confirm <epoch> [--dry-run]", "help": "[<verb>|<group>]", "version": "",
	}
}
