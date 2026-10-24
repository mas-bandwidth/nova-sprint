// Package gatefact is the one vocabulary of gate facts: the words a brief's GATE-NEEDS, a
// member's beat and its gates_only name a machine by.
package gatefact

import "strings"

// A member's beat carries its facts as Go names its machine (darwin, arm64, amd64), and a
// brief's GATE-NEEDS line is written by a person, who says macos. On 2026-10-24 every card
// whose GATE-NEEDS said macos raised "no member up satisfies GATE-NEEDS macos" while the
// one macOS member (gates_only darwin, facts darwin,arm64,...) beat with width 16: the deal
// compared the words, not the facts, and the gates-only filter dropped the member before the
// judgment could even name it. Every word that says a gate fact (a GATE-NEEDS need, a beat's
// fact, a member's gates_only gate, the card lint's toolchain term) is read through GateFact,
// so the two sides of the match share one vocabulary.

// gateFactAliases are the other names of a fact, each to the name a beat carries.
var gateFactAliases = map[string]string{
	"macos": "darwin", "mac": "darwin", "osx": "darwin", "macosx": "darwin",
	"win": "windows", "win32": "windows", "win64": "windows",
	"x86_64": "amd64", "x86-64": "amd64", "x64": "amd64",
	"aarch64": "arm64",
}

// GateFact is word as the fact a beat carries: trimmed, lowercased, an alias read as its fact
// (macos is darwin, x86_64 amd64, aarch64 arm64).
func GateFact(word string) string {
	w := strings.ToLower(strings.TrimSpace(word))
	if f, ok := gateFactAliases[w]; ok {
		return f
	}
	return w
}

// GateFacts is GateFact of each word, in order, duplicates dropped; nil for no word.
func GateFacts(words []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range words {
		f := GateFact(w)
		if f != "" && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}
