package verbs

// BusAnswer is what the sprint does with a message sent to its name on the
// bus (run's hook execs `nova-sprint bus-reply` for each one; directive 61
// item 45): nothing but one log line. The sprint's pushes are receipts and
// the verbs are the answers, so no message is answered on the bus (answering
// a REPLY with a REPLY made a loop). log is "" for its own name or no sender.
func BusAnswer(from string) (log string) {
	if from == "" || from == "sprint" {
		return ""
	}
	return "ignored a message from " + clip(from) + ": the sprint answers nothing on the bus; the verbs are the answers"
}
