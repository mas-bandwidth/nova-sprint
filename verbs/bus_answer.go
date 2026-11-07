package verbs

import (
	"strings"
)

// BusAnswer is what the sprint does with a message sent to its name on the
// bus (run's hook execs `nova-sprint bus-reply` for each one). The sprint's pushes are receipts, never answered on the bus:
// the verbs are the answers. So a reply (a message that answers another, or
// one whose subject is REPLY) is never answered, which would otherwise loop
// when the sprint answered a REPLY with a REPLY; a message that is
// not a verb is ignored; a message whose subject is a verb is answered once
// with where verbs run (a REPLY, which the sender's answer to will not
// get). log is the one line the hook's log keeps for an ignored message,
// "" when none is ignored; reply is the REPLY body, "" for none.
func BusAnswer(from, re, subject string) (reply, log string) {
	if from == "" || from == "sprint" {
		return "", ""
	}
	words := strings.Fields(subject)
	switch {
	case re != "" && re != "-", len(words) > 0 && strings.EqualFold(strings.TrimSuffix(words[0], ":"), "REPLY"):
		return "", "ignored a reply from " + clip(from) + ": the sprint's pushes are receipts; the verbs are the answers"
	case len(words) > 0 && words[0] == "nova-sprint":
		words = words[1:]
	}
	if len(words) == 0 {
		return "", "ignored a message from " + clip(from) + " that is not a verb"
	}
	if v, _ := lookup(words); v == nil || v.name == "help" && words[0] != "help" {
		return "", "ignored a message from " + clip(from) + " that is not a verb (" + clip(words[0]) + ")"
	}
	return "the sprint takes no verbs over the bus: run nova-sprint " + clip(strings.Join(words, " ")) + " --as " + clip(from) + " yourself (nova-sprint next --as " + clip(from) + " says what to do now)", ""
}
