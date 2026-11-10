package run

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
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
			if !exists && (after.State == "working" || after.State == "ready") { // cut: in the row's hand at the deal
				out = append(out, dealNotice(after, afterView.Streams[after.Stream], event.Epoch))
			}
			if after.State == "fail" && exists && (before.State == "ready" || before.State == "working") {
				out = append(out, withdrawNotice(after))
			}
		}
		if recipient == n.Coordinator && (after.Kind == "work" || after.Kind == "read") && after.State == "fail" && exists &&
			(before.State == "ready" || before.State == "working") && strings.HasPrefix(after.Reason, card.ReturnedPrefix) {
			out = append(out, Notice{Subject: "RETURNED", Line: fmt.Sprintf("RETURNED card=%s from=%s: %s", after.ID, after.Holder, clipText(after.Reason, after.ID))})
		}
		if recipient == n.Coordinator && after.Kind == "producer" {
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

// dealNotice tells the row what it now holds (defect 52: the deal is the
// take): the card, the one model its deal named (defect 57; "none" for a
// card dealt before models were named), where to work (repo, base, branch;
// a read's head), the brief clipped, the finish or read command, and take,
// the acknowledgement that stops the nudges and prints the whole brief.
func dealNotice(c store.Card, s store.Stream, epoch int) Notice {
	ref, row := steps.ShellWord(fmt.Sprintf("%s@%d", c.ID, c.Gen)), steps.ShellWord(c.Holder)
	line := fmt.Sprintf("DEAL card=%s attempt=%d kind=%s stream=%s tier=%s model=%s epoch=%d repo=%s", c.ID, c.Attempt, c.Kind, c.Stream, c.Tier, steps.ShellWord(cmp.Or(c.Model, "none")), epoch, s.Repo)
	finish := fmt.Sprintf("nova-sprint finish %s --head <sha> --as %s --epoch %d", ref, row, epoch)
	if c.Kind == "read" {
		line += " head=" + c.Head
		finish = fmt.Sprintf("nova-sprint read %s yes|fix --as %s --epoch %d", ref, row, epoch)
	} else {
		line += " base=" + c.BaseSHA + " branch=" + c.Branch
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
	line += " brief=" + clipWith(c.Brief, "nova-sprint take "+ref)
	line += fmt.Sprintf("; in your hand now: run nova-sprint take %s --epoch %d --as %s (acknowledges, prints the whole brief), then do it and run: %s", ref, epoch, row, finish)
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
func clipText(s, id string) string  { return clipWith(s, "nova-sprint show "+steps.ShellWord(id)) }
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
	line += "; run: nova-sprint show " + steps.ShellWord(stream) + " --stitch"
	return Notice{Subject: subject, Line: line}, true
}
