package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// cmdBlocked lists the blocked primaries (sprint.BlockedCards): each with its flaps by
// kind, its last flap and how long it has been blocked, one line a card.
func (a *app) cmdBlocked(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("blocked")
	stream := fs.String("stream", "", "the blocked cards of one stream (default: every stream)")
	s, code := a.workSnapshot("blocked", fs, args, c, stderr)
	if code != 0 {
		return code
	}
	s.Now = a.now()
	cards := []sprint.BlockedCard{}
	for _, b := range sprint.BlockedCards(s) {
		if *stream == "" || b.Stream == *stream {
			cards = append(cards, b)
		}
	}
	if c.json {
		b, _ := json.Marshal(struct {
			Cards []sprint.BlockedCard `json:"cards"`
		}{cards})
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	for _, b := range cards {
		fmt.Fprintf(stdout, "BLOCKED %s stream=%s from=%s flaps=%d %s for=%s last=%s\n", oneline.Escape(b.ID), oneline.Escape(b.Stream), b.From,
			b.Flaps.Total, b.Flaps.Kinds(), b.For, oneline.Field(b.Flaps.Last))
	}
	fmt.Fprintf(stdout, "BLOCKED OK cards=%d; unblock: nova-sprint unblock <id> --reason '<what was fixed>'\n", len(cards))
	return 0
}

// cmdUnblock is the coordinator's unblock of one blocked primary, to waiting
// (sprint.Unblock): --reason says what was fixed; --dry-run says what would move.
func (a *app) cmdUnblock(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("unblock")
	reason := fs.String("reason", "", "what was fixed: recorded on the card (required)")
	ans := fs.String("answers", "", answersWords)
	dry := fs.Bool("dry-run", false, "say what would move and what would be refused; write nothing")
	ids, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, "unblock", err.Error())
	}
	if len(ids) != 1 || strings.TrimSpace(*reason) == "" {
		return refuse(stderr, "unblock", "wants one <id> and --reason <what was fixed>; run: nova-sprint help unblock")
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "unblock", err.Error())
	}
	req := sprint.UnblockReq{ID: ids[0], Reason: *reason, Answers: answers(*ans), Who: c.actor}
	if *dry {
		_, snap, err := st.Check(context.Background(), 1)
		if err != nil {
			return a.readFailed("unblock", err, stderr)
		}
		plan := sprint.Unblock(snap, req)
		var lines []string
		for _, u := range plan.Units {
			lines = append(lines, u.Moved)
		}
		for _, r := range plan.Refused {
			lines = append(lines, "REFUSED "+r.Key+": "+r.Why)
		}
		code, status := 0, "ok"
		if len(plan.Refused) > 0 {
			code, status = 1, "refused"
		}
		sayOK(stdout, c.json, "unblock", fmt.Sprintf("UNBLOCK %s DRY-RUN changes=%d refused=%d\n%s", strings.ToUpper(status), len(plan.Units), len(plan.Refused), strings.Join(lines, "\n")),
			map[string]any{"status": status, "exit": code, "dry_run": true, "changes": len(plan.Units), "lines": lines, "refused": plan.Refused})
		return code
	}
	return a.runStep("unblock", *c, st, store.UnblockStep(req), stdout, stderr)
}
