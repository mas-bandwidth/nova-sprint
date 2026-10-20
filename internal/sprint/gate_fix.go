package sprint

import (
	"fmt"
	"sort"
	"strings"
)

// A gate artifact and its fix are one shape (docs/SPEC-SPRINT.md section 2,
// the cards; tla/WaitFor.tla, the gate-artifact edge and NoWaitCycle): a card
// G whose DONE-WHEN runs a command, and a card F that needs G, directly or
// through other open cards, whose DONE-WHEN or TEST runs the same command. G
// cannot be done before F's work, and F waits for G, so neither can finish.
// It is refused at add, at a brief edit that writes DONE-WHEN, TEST or
// DEPENDS-ON, and named by brief check as gate-fix-cycle. The split lands G's
// infrastructure red and turns it green in a twin that needs F.

// GateFixCard is one card the check reads: its id, brief and needs. The needs
// are the ones the add or the brief edit writes, never the text's stale list.
type GateFixCard struct {
	ID    string
	Brief string
	Needs []string
}

// GateFixFinding is one gate-fix cycle: the gate G, the dependent F and the
// command key they share.
type GateFixFinding struct {
	Gate, Fix, Key string
	Why            string
}

// gateFixKeyEqual is the key comparison's test-only seam: the reversed witness
// turns it off and TestGateFixCycleRefusedAtAdd adds the pair it should refuse.
var gateFixKeyEqual = func(a, b string) bool { return a == b }

// GateFixCycleCheck is every gate-fix cycle among the open primaries and the
// proposed cards (an add's own, or a brief edit's new text and needs): a card
// F and a card G reachable through F's needs with a command key of G's
// DONE-WHEN equal to a key of F's DONE-WHEN or TEST. Landed and dropped cards
// are out; sentinels are not primaries.
func GateFixCycleCheck(s *Snapshot, proposed []GateFixCard) []GateFixFinding {
	type state struct {
		id       string
		needs    []string
		done     []string
		test     []string
		commands map[string]string
	}
	byID := map[string]*state{}
	if s != nil && s.Work != nil {
		for _, c := range s.Work.Cards() {
			if !c.Placed() || IsSentinel(c) || !IsOpen(c.Col) {
				continue
			}
			done, test, commands := gateFixBriefCommands(c.F("brief"))
			byID[c.ID] = &state{id: c.ID, needs: Split(c.F("needs")), done: done, test: test, commands: commands}
		}
	}
	for _, c := range proposed {
		st := byID[c.ID]
		if st == nil {
			st = &state{id: c.ID}
			byID[c.ID] = st
		}
		st.needs = append([]string(nil), c.Needs...)
		st.done, st.test, st.commands = gateFixBriefCommands(c.Brief)
	}
	var ids []string
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []GateFixFinding
	for _, fixID := range ids {
		f := byID[fixID]
		seen := map[string]bool{fixID: true}
		queue := []string{fixID}
		for len(queue) > 0 {
			x := queue[0]
			queue = queue[1:]
			for _, need := range byID[x].needs {
				if seen[need] {
					continue
				}
				gate := byID[need]
				if gate == nil {
					continue
				}
				seen[need] = true
				queue = append(queue, need)
				for _, gateCmd := range gate.done {
					dk := gateFixCommandKey(gateCmd)
					if dk == "" {
						continue
					}
					matched := false
					for _, fcmd := range append(append([]string(nil), f.done...), f.test...) {
						fk := gateFixCommandKey(fcmd)
						if fk != "" && gateFixKeyEqual(dk, fk) {
							matched = true
							break
						}
					}
					if !matched {
						continue
					}
					command := gate.commands[dk]
					if command == "" {
						command = gateCmd
					}
					out = append(out, GateFixFinding{Gate: gate.id, Fix: f.id, Key: dk,
						Why: gateFixWhy(gate.id, f.id, dk, command)})
					break
				}
			}
		}
	}
	return out
}

// gateFixWhy is the refusal, naming both ids, the command key and the split.
func gateFixWhy(gate, fix, key, command string) string {
	return fmt.Sprintf("gate-fix cycle: %s is in %s's needs (directly or through open cards), and %s's DONE-WHEN runs the same command as %s's DONE-WHEN or TEST (key %q): %s cannot be done before %s's work, and %s waits for %s; nothing was changed; split %s: land its infrastructure with `%s` allowed red (its DONE-WHEN says so), and turn it green in a card that needs %s: run: nova-sprint twin %s --needs %s --instruction 'make `%s` green'",
		gate, fix, gate, fix, key, gate, fix, fix, gate, gate, command, fix, gate, fix, key)
}

// gateFixEditWhy is the brief edit's refusal when the edit named by id is one
// side of a cycle: every proposed edit of the batch is in the check, since two
// edits that each pass alone may close one.
func gateFixEditWhy(s *Snapshot, edits map[string]GateFixCard, id string) string {
	cards := make([]GateFixCard, 0, len(edits))
	for _, e := range edits {
		cards = append(cards, e)
	}
	for _, f := range GateFixCycleCheck(s, cards) {
		if f.Gate == id || f.Fix == id {
			return f.Why
		}
	}
	return ""
}

// gateFixBriefKeys is the keys of a brief's DONE-WHEN commands and its TEST
// commands, the comparison the cycle makes.
func gateFixBriefKeys(brief string) (done, test []string) {
	doneCmds, testCmds, _ := gateFixBriefCommands(brief)
	for _, c := range doneCmds {
		if k := gateFixCommandKey(c); k != "" {
			done = append(done, k)
		}
	}
	for _, c := range testCmds {
		if k := gateFixCommandKey(c); k != "" {
			test = append(test, k)
		}
	}
	return done, test
}

// sameGateFixCommands says the two briefs' DONE-WHEN and TEST command keys are
// the same: an edit that changes no key cannot close a gate-fix cycle.
func sameGateFixCommands(a, b string) bool {
	ad, at := gateFixBriefKeys(a)
	bd, bt := gateFixBriefKeys(b)
	if len(ad) != len(bd) || len(at) != len(bt) {
		return false
	}
	for i := range ad {
		if ad[i] != bd[i] {
			return false
		}
	}
	for i := range at {
		if at[i] != bt[i] {
			return false
		}
	}
	return true
}

// gateFixBriefCommands reads the backticked commands on a brief's DONE-WHEN
// and TEST lines, with each DONE-WHEN command's key and text.
func gateFixBriefCommands(brief string) (done, test []string, commands map[string]string) {
	commands = map[string]string{}
	for _, line := range strings.Split(brief, "\n") {
		key, value, ok := gateFixHeaderValue(line)
		if !ok {
			continue
		}
		switch {
		case strings.EqualFold(key, "DONE-WHEN"):
			for _, cmd := range gateFixBackticks(value) {
				done = append(done, cmd)
				if k := gateFixCommandKey(cmd); k != "" {
					if _, seen := commands[k]; !seen {
						commands[k] = cmd
					}
				}
			}
		case strings.EqualFold(key, "TEST"):
			test = append(test, gateFixBackticks(value)...)
		}
	}
	return done, test, commands
}

// gateFixHeaderValue is a header-like line's key and value: a bullet and bold
// marks around the key are allowed, as the card lint allows them.
func gateFixHeaderValue(line string) (key, value string, ok bool) {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "- ")
	s = strings.TrimPrefix(s, "* ")
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "**")
	key, value, ok = strings.Cut(s, ":")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSuffix(strings.TrimSpace(key), "**")
	return strings.TrimSpace(key), strings.TrimSpace(strings.TrimPrefix(value, "**")), true
}

// gateFixBackticks is every backticked command in s.
func gateFixBackticks(s string) []string {
	var out []string
	for {
		i := strings.IndexByte(s, '`')
		if i < 0 {
			break
		}
		s = s[i+1:]
		j := strings.IndexByte(s, '`')
		if j < 0 {
			break
		}
		if cmd := strings.TrimSpace(s[:j]); cmd != "" {
			out = append(out, cmd)
		}
		s = s[j+1:]
	}
	return out
}

// gateFixCommandKey is the key two commands are the same by: make's target
// words (arguments that are neither VAR=value nor a flag), go test's package
// paths and -run pattern, and any other command's words with whitespace
// collapsed.
func gateFixCommandKey(cmd string) string {
	words := strings.Fields(cmd)
	i := 0
	for i < len(words) && gateFixAssign(words[i]) {
		i++
	}
	if i >= len(words) {
		return ""
	}
	switch words[i] {
	case "make":
		var targets []string
		for _, w := range words[i+1:] {
			if gateFixAssign(w) || strings.HasPrefix(w, "-") {
				continue
			}
			targets = append(targets, w)
		}
		if len(targets) == 0 {
			return "make"
		}
		return strings.Join(targets, " ")
	case "go":
		if i+1 < len(words) && words[i+1] == "test" {
			var pkgs []string
			run := ""
			rest := words[i+2:]
			for j := 0; j < len(rest); j++ {
				w := rest[j]
				if w == "-run" || w == "--run" {
					if j+1 < len(rest) {
						run = gateFixUnquote(rest[j+1])
						j++
					}
					continue
				}
				if v, ok := strings.CutPrefix(w, "-run="); ok {
					run = gateFixUnquote(v)
					continue
				}
				if v, ok := strings.CutPrefix(w, "--run="); ok {
					run = gateFixUnquote(v)
					continue
				}
				if strings.HasPrefix(w, "-") {
					if !strings.Contains(w, "=") && gateFixGoTestValueFlag(w) && j+1 < len(rest) {
						j++
					}
					continue
				}
				pkgs = append(pkgs, gateFixUnquote(w))
			}
			key := strings.Join(pkgs, " ")
			if run != "" {
				key = strings.TrimSpace(key + " -run " + run)
			}
			return key
		}
	}
	return strings.Join(words[i:], " ")
}

// gateFixUnquote removes the shell quotes around one word.
func gateFixUnquote(w string) string {
	if len(w) >= 2 && ((w[0] == '\'' && w[len(w)-1] == '\'') || (w[0] == '"' && w[len(w)-1] == '"')) {
		return w[1 : len(w)-1]
	}
	return w
}

// gateFixGoTestValueFlag is a go test flag whose value is its next word.
func gateFixGoTestValueFlag(w string) bool {
	switch w {
	case "-timeout", "--timeout", "-tags", "--tags", "-count", "--count", "-parallel", "--parallel",
		"-benchtime", "--benchtime", "-cpu", "--cpu", "-coverprofile", "--coverprofile", "-covermode", "--covermode",
		"-memprofile", "--memprofile", "-cpuprofile", "--cpuprofile", "-blockprofile", "--blockprofile",
		"-mutexprofile", "--mutexprofile", "-o", "--o", "-p", "--p", "-exec", "--exec", "-bench", "--bench",
		"-fuzz", "--fuzz", "-fuzztime", "--fuzztime", "-shuffle", "--shuffle", "-vet", "--vet", "-skip", "--skip":
		return true
	}
	return false
}

// gateFixAssign says the word is a shell VAR=value word.
func gateFixAssign(w string) bool {
	i := strings.IndexByte(w, '=')
	if i <= 0 {
		return false
	}
	for j, r := range w[:i] {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
		case j > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}
