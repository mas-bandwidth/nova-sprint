package run

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// EventNotices derives recipient messages only from committed event images.
type EventNotices struct{ Coordinator string }

func (n EventNotices) ForEvent(event store.Event, afterView store.Snapshot, recipient string) ([]Notice, error) {
	if !event.Accepted || event.After == nil {
		return nil, nil
	}
	var out []Notice
	stitched := map[string]bool{}
	for _, id := range sortedCardIDs(event.After.Cards) {
		after := event.After.Cards[id]
		var before store.Card
		exists := false
		if event.Before != nil {
			before, exists = event.Before.Cards[id]
		}
		if (after.Kind == "work" || after.Kind == "read") && after.Holder == recipient {
			if after.State == "ready" && (!exists || before.State != "ready") {
				out = append(out, dealNotice(after, event.Epoch))
			}
			if after.State == "fail" && exists && (before.State == "ready" || before.State == "working") {
				out = append(out, withdrawNotice(after))
			}
		}
		if recipient == n.Coordinator && after.Kind == "producer" {
			if after.State == "landed" && (!exists || before.State != "landed") {
				out = append(out, Notice{Subject: "LANDED", Line: fmt.Sprintf("LANDED card=%s stream=%s head=%s; run: nova-sprint show %s", after.ID, after.Stream, after.Head, shellWord(after.ID))})
			}
			terminal := after.State == "landed" || after.State == "fail"
			wasTerminal := exists && (before.State == "landed" || before.State == "fail")
			if terminal && !wasTerminal && !stitched[after.Stream] {
				if notice, ok := terminalNotice(afterView, after.Stream); ok {
					out = append(out, notice)
					stitched[after.Stream] = true
				}
			}
		}
	}
	if recipient == n.Coordinator {
		out = append(out, judgementNotices(event, n.Coordinator)...)
	}
	return out, nil
}
func sortedCardIDs(cards map[string]store.Card) []string {
	keys := make([]string, 0, len(cards))
	for id := range cards {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	return keys
}
func dealNotice(c store.Card, epoch int) Notice {
	line := fmt.Sprintf("card=%s attempt=%d kind=%s stream=%s tier=%s epoch=%d", c.ID, c.Attempt, c.Kind, c.Stream, c.Tier, epoch)
	if c.Kind == "read" {
		line += " head=" + c.Head
	}
	if c.Prior != "" {
		line += " prior=" + c.Prior
	}
	reason := c.Reason
	if reason == "" {
		reason = c.Finding
	}
	if reason != "" {
		line += " reason=" + clipText(reason, c.ID)
	}
	line += fmt.Sprintf("; run: nova-sprint take %s --epoch %d --as %s", shellWord(fmt.Sprintf("%s@%d", c.ID, c.Gen)), epoch, shellWord(c.Holder))
	return Notice{Subject: "DEAL", Line: line}
}
func withdrawNotice(c store.Card) Notice {
	return Notice{Subject: "WITHDRAWN", Line: fmt.Sprintf("WITHDRAWN card=%s reason=%s; stop the runner and push its work", c.ID, clipText(c.Reason, c.ID))}
}
func judgementNotices(e store.Event, coordinator string) []Notice {
	keys := make([]string, 0, len(e.After.Judgements))
	for id := range e.After.Judgements {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	var out []Notice
	for _, id := range keys {
		j := e.After.Judgements[id]
		var before store.Judgement
		existed := false
		if e.Before != nil {
			before, existed = e.Before.Judgements[id]
		}
		if !existed && j.State == "open" {
			group := j.Group
			if group == "" {
				group = j.ID
			}
			line := fmt.Sprintf("NEEDS YOU id=%s: %s; run: %s", j.ID, clipJudgement(j.Question), safeCommand(j.Command))
			out = append(out, Notice{Subject: "NEEDS YOU", Group: group, Line: line})
		} else if existed && before.State == "open" && j.State == "expired" {
			out = append(out, Notice{Subject: "EXPIRED", Line: fmt.Sprintf("EXPIRED judgement=%s: %s", j.ID, clipJudgement(j.Answer))})
		} else if existed && before.State == "open" && j.State == "answered" && j.Actor != coordinator {
			out = append(out, Notice{Subject: "ANSWERED", Line: fmt.Sprintf("ANSWERED judgement=%s by=%s choice=%s", j.ID, j.Actor, clipJudgement(j.Answer))})
		}
	}
	return out
}

// clipText caps free text in bytes without splitting UTF-8; the full value stays in the store.
func clipText(s, id string) string  { return clipWith(s, "nova-sprint show "+shellWord(id)) }
func clipJudgement(s string) string { return clipWith(s, "nova-sprint judge") }
func clipWith(s, command string) string {
	s = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(s)
	if len(s) <= 300 {
		return s
	}
	s = s[:300]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "… full text: " + command
}

func shellWord(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._:@/-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func safeCommand(command string) string {
	if command == "" || len(command) > 300 || strings.ContainsAny(command, "\r\n") {
		return "nova-sprint judge"
	}
	return command
}

func terminalNotice(s store.Snapshot, stream string) (Notice, bool) {
	landed, failed := 0, []string{}
	for _, c := range s.Cards {
		if c.Stream != stream || c.Kind != "producer" {
			continue
		}
		switch c.State {
		case "landed":
			landed++
		case "fail":
			failed = append(failed, c.ID)
		default:
			return Notice{}, false
		}
	}
	if landed+len(failed) == 0 {
		return Notice{}, false
	}
	subject := "STREAM LANDED"
	line := fmt.Sprintf("STREAM LANDED %s %d landed", stream, landed)
	if len(failed) > 0 {
		slices.Sort(failed)
		subject = "STREAM DONE WITH FAILURES"
		shown := failed
		if len(shown) > 20 {
			shown = shown[:20]
		}
		line = fmt.Sprintf("%s %s %d landed %d failed ids=%s", subject, stream, landed, len(failed), strings.Join(shown, ","))
		if len(failed) > len(shown) {
			line += fmt.Sprintf(" (+%d more)", len(failed)-len(shown))
		}
	}
	line += "; run: nova-sprint show " + shellWord(stream) + " --stitch"
	return Notice{Subject: subject, Line: line}, true
}
