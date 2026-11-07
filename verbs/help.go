package verbs

import (
	"context"
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/docs"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// runHelp is `help`: the index (every verb by group, with who may run it),
// one verb's table (usage, what it does, who, what refuses it and the fix),
// a verb's subcommands, or a role's playbook. It is built from the verb
// table, so it lists only real verbs.
func runHelp(_ *Engine, _ context.Context, in *input, _ store.Snapshot) (string, error) {
	name := strings.Join(in.args, " ")
	switch {
	case name == "":
		return helpIndex(), nil
	case in.arg(0) == "playbook":
		text, ok := docs.Playbook(in.arg(1))
		if !ok {
			return "", misuse("help", "the playbooks are worker and coordinator: nova-sprint help playbook worker")
		}
		return done("nova-sprint next --as <you>", text), nil
	}
	if v := find(name); v != nil {
		return helpVerb(v), nil
	}
	if subs := sub(name); len(subs) > 0 {
		lines := make([]string, len(subs))
		for i, s := range subs {
			lines[i] = helpLine(find(name + " " + s))
		}
		return done("nova-sprint help "+name+" "+subs[0], lines...), nil
	}
	return "", misuse("help", "no verb "+name)
}

func helpIndex() string {
	lines := []string{"nova-sprint <verb> [flags]; every verb takes --as <name> (or NOVA_SPRINT_AS).",
		"Exit 0: done, the last line says next. Exit 1: refused, one line naming the fix. Exit 2: bad usage, or the store, bus or an edge failed."}
	for _, g := range groups {
		lines = append(lines, "", g+":")
		for _, v := range table {
			if v.group == g {
				lines = append(lines, helpLine(v))
			}
		}
	}
	return done("nova-sprint next --as <you>", append(lines, "", "nova-sprint help <verb> for its table; nova-sprint help playbook worker|coordinator for a role's playbook")...)
}

func helpLine(v *verb) string {
	return fmt.Sprintf("  %-16s %s [%s]", v.name, v.about, v.role)
}

func helpVerb(v *verb) string {
	lines := append([]string{"nova-sprint " + strings.TrimSpace(v.name+" "+v.usage), v.about}, v.more...)
	lines = append(lines, "who: "+v.role.String())
	if len(v.needs) > 0 {
		lines = append(lines, "refuses when (and the fix it names):")
		for _, n := range v.needs {
			lines = append(lines, "  "+n)
		}
	}
	return done("nova-sprint "+v.name+" ...", lines...)
}
