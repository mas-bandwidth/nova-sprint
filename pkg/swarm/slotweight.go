package swarm

import (
	"strings"
)

// Slot admission weight (nova-tools#2033).
//
// Capacity is refused at take, before any child starts. A schema card spawns
// make/cargo/dotnet and is far heavier than a read-only card; charging the
// kind's weight against the store's share is the ceiling. A load reading after
// launch is a brake that arrives too late: a machine reached load 172 on
// 64 cores and fell off the network.

// CardKindFromText reads a card's kind from its typed KIND: header or a :kind
// pull field. An untyped card is "".
func CardKindFromText(text string) string {
	if k := cardFields(text)["kind"]; k != "" {
		return k
	}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == ":kind" {
				return strings.ToLower(fields[i+1])
			}
		}
	}
	return ""
}

// cardHeaders are the field lines a card may state its own evidence with. The
// set is closed: a line the card carries that is not one of these is the
// card's business and is never read as evidence.
var cardHeaders = []string{"kind", "files", "packages", "lanes", "lane", "platform", "touches", "deadline"}

// cardFields reads the stated evidence lines, first occurrence winning.
func cardFields(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		for _, h := range cardHeaders {
			if name != h || out[h] != "" {
				continue
			}
			out[h] = strings.ToLower(strings.TrimSpace(value))
		}
	}
	return out
}
