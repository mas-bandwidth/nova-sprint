package verbs

import (
	"strings"
	"testing"
)

// TestBusAnswer: the sprint's pushes are receipts. A reply is never
// answered (no REPLY-to-REPLY loop), a message
// that is not a verb is ignored with one log line, and a verb is pointed
// at once; its own name and no sender are nothing.
func TestBusAnswer(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ from, re, subject, reply, log string }{
		{from: "alice", re: "m-1", subject: "thanks", log: "ignored a reply from alice: the sprint's pushes are receipts"},
		{from: "alice", re: "-", subject: "REPLY", log: "ignored a reply from alice"},
		{from: "alice", subject: "reply: ok", log: "ignored a reply from alice"},
		{from: "alice", subject: "DEAL received, working on it", log: "ignored a message from alice that is not a verb (DEAL"},
		{from: "alice", subject: "", log: "ignored a message from alice that is not a verb"},
		{from: "alice", subject: "-h", log: "that is not a verb (-h)"},
		{from: "alice", subject: "nova-sprint take p1@2", reply: "the sprint takes no verbs over the bus: run nova-sprint take p1@2 --as alice yourself"},
		{from: "alice", subject: "finish p1@2 --head x", reply: "run nova-sprint finish p1@2 --head x --as alice yourself"},
		{from: "sprint", subject: "take p1@2"},
		{from: "", subject: "take p1@2"},
	} {
		reply, log := BusAnswer(c.from, c.re, c.subject)
		if (c.reply == "") != (reply == "") || !strings.Contains(reply, c.reply) || (c.log == "") != (log == "") || !strings.Contains(log, c.log) {
			t.Fatalf("BusAnswer(%q, %q, %q) = %q, %q", c.from, c.re, c.subject, reply, log)
		}
	}
}
