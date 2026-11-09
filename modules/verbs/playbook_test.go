package verbs

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The docs a new AI is told to trust: every `nova-sprint <verb> ...` they
// show is held to the verb table, so the text cannot drift from the binary.
var taughtDocs = []string{"../../docs/README.md", "../playbooks/PLAYBOOK-WORKER.md", "../playbooks/PLAYBOOK-COORDINATOR.md", "../../docs/CORRECT-USE.md", "../../docs/SETUP.md", "../../docs/CARD-CONTRACT.md"}

var (
	codeSpan  = regexp.MustCompile("`([^`\n]+)`")
	flagToken = regexp.MustCompile(`^--([a-z][a-z0-9-]*)`)
	listItem  = regexp.MustCompile(`^(- |[0-9]+\. )`)
)

// taughtCommands is every nova-sprint command in text: each inline code
// span, and each line of a fenced block, from "nova-sprint " to the end of
// the command (a shell separator, or the next nova-sprint). A span that is
// a flag alone ("`--expect <n>`") is the flag of the paragraph's last
// command before it, and is given as that command with the flag.
func taughtCommands(text string) []string {
	var out []string
	last, fenced := "", false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			fenced = !fenced
			continue
		case trimmed == "" && !fenced:
			last = "" // a new paragraph
			continue
		case listItem.MatchString(trimmed):
			last = "" // a new list item
		}
		spans := []string{line}
		if !fenced {
			spans = nil
			for _, m := range codeSpan.FindAllStringSubmatch(line, -1) {
				spans = append(spans, m[1])
			}
		}
		for _, s := range spans {
			if strings.HasPrefix(s, "--") && last != "" {
				out = append(out, strings.Fields(last)[0]+" "+s)
				continue
			}
			last = "" // a flag after another tool's command or a quoted output is not ours
			for i, p := range strings.Split(s, "nova-sprint ")[1:] {
				if cmd := strings.TrimSpace(cutShell(p)); cmd != "" {
					out = append(out, cmd)
					if i == 0 && strings.HasPrefix(strings.TrimSpace(s), "nova-sprint ") {
						last = cmd
					}
				}
			}
		}
	}
	return out
}

// cutShell is a command up to the first shell separator or redirection.
func cutShell(s string) string {
	for _, sep := range []string{" && ", " || ", "; ", " > ", " | ", "'", ")"} {
		if i := strings.Index(s, sep); i >= 0 {
			s = s[:i]
		}
	}
	return s
}

// checkTaught is why cmd (the words after nova-sprint) is not a command the
// binary takes, or "": its verb is in the table and every flag is the
// verb's.
func checkTaught(cmd string) string {
	words := strings.Fields(cmd)
	first := strings.Trim(words[0], "[]().,:")
	if first == "" || strings.ContainsAny(first[:1], "<.-") || first != words[0] && !isVerbWord(first) {
		return "" // a placeholder (<verb>, ...) or prose about the binary
	}
	v := find(first)
	rest := words[1:]
	if len(words) > 1 {
		if two := find(first + " " + words[1]); two != nil {
			v, rest = two, words[2:]
		}
	}
	if v == nil {
		if len(sub(first)) > 0 {
			return "no subcommand " + strings.Join(words[:min(2, len(words))], " ")
		}
		return "no verb " + first
	}
	spec := parseSpec(v.flags + " as=")
	if v.name == "help" { // help's words are verbs or a playbook
		return ""
	}
	for _, w := range rest {
		w = strings.TrimLeft(w, "[(")
		if m := flagToken.FindStringSubmatch(w); m != nil {
			if _, ok := spec[m[1]]; !ok && sprintFlag(m[1]) {
				return v.name + " has no flag --" + m[1]
			}
		}
	}
	return ""
}

// sprintFlag is whether some verb takes --name: a flag no verb takes is
// another tool's (git's --force-with-lease) in the same paragraph.
func sprintFlag(name string) bool {
	for _, v := range table {
		if _, ok := parseSpec(v.flags + " as=")[name]; ok {
			return true
		}
	}
	return false
}

func isVerbWord(w string) bool { return find(w) != nil || len(sub(w)) > 0 }

// TestPlaybookCommandsParse holds every nova-sprint command the taught docs
// show to the verb table.
func TestPlaybookCommandsParse(t *testing.T) {
	t.Parallel()
	for _, name := range taughtDocs {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		cmds := taughtCommands(string(b))
		if len(cmds) == 0 {
			t.Errorf("%s: no nova-sprint command found; the extractor is broken", name)
		}
		for _, cmd := range cmds {
			if why := checkTaught(cmd); why != "" {
				t.Errorf("%s: `nova-sprint %s`: %s", name, cmd, why)
			}
		}
	}
}

// A conflict goes back as a merge, never a rebase: the next head must
// descend from the prior head, so both playbooks say so.
func TestPlaybooksMergeNeverRebase(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"../playbooks/PLAYBOOK-WORKER.md":      "merge it into your branch and resolve the conflicts; never rebase",
		"../playbooks/PLAYBOOK-COORDINATOR.md": "merge current <base> into the card branch and resolve the conflicts; never rebase",
	} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(strings.Fields(string(b)), " "), want) {
			t.Errorf("%s lacks %q", name, want)
		}
	}
}

// The checker refuses what the playbooks once taught (edit, drop --expect,
// a bare subcommand) and passes what the binary takes.
func TestCheckTaughtRefusesDrift(t *testing.T) {
	t.Parallel()
	for cmd, want := range map[string]string{
		"edit <card> --reason <why>":                      "no verb edit",
		"drop <card> --expect 2 --reason <why>":           "drop has no flag --expect",
		"finish <id>@<gen> --force-with-lease":            "",
		"policy frob <v>":                                 "no subcommand policy frob",
		"finish <id>@<gen> --head <sha> --as <you>":       "",
		"read <id>@<gen> yes [--cost <usd>] --as <you>":   "",
		"drop <c> --reason r --relink <dependant>=<card>": "",
		"<verb> ...":           "",
		"help playbook worker": "",
	} {
		if got := checkTaught(cmd); got != want {
			t.Errorf("checkTaught(%q) = %q, want %q", cmd, got, want)
		}
	}
	got := taughtCommands("run `nova-sprint take a@1 --as x` then\n```\nnova-sprint next --as x && nova-sprint show a\n```\nadd `--expect <n>`\n\nthen `--json`\n")
	if strings.Join(got, "|") != "take a@1 --as x|next --as x|show a|next --expect <n>" {
		t.Errorf("taughtCommands = %q", got)
	}
}
