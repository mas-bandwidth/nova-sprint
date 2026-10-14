package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// hookOverdueDefault is the default --overdue: a card-holding judgment the server's
// own list still holds unanswered this long is named by one OVERDUE line, again
// every interval until it is answered. (docs/CLI.md, `nova-sprint hook --compact`;
// docs/SPEC-SPRINT.md, "The hook".)
const hookOverdueDefault = 5 * time.Minute

// compactSay is one line of the compact subscription: only the HOOK lines the seat
// acts on (connected, challenge, bye, refused) and each JUDGMENT push cut to its
// group line plus its first detail line. A judgment's group line may be prefixed
// CRITICAL <n> behind: when its cards wait on it (reads.go, groupLine), so a push
// is a judgment unless its first line is a HAPPENED one; HAPPENED pushes and the
// verb menus are not printed. Every line is one write, so a pipe sees it at once.
// (docs/CLI.md, `nova-sprint hook --compact`; docs/SPEC-SPRINT.md, "The hook".)
func compactSay(w io.Writer, name, addr, from string, m hookMsg) {
	switch m.T {
	case "hello":
		fmt.Fprintf(w, "HOOK CONNECTED name=%s session=%s server=%s from=%s\n", oneline.Field(m.Name), oneline.Field(m.Session), oneline.Field(addr), oneline.Field(from))
	case "challenge":
		fmt.Fprintf(w, "HOOK CHALLENGE %s: answer now: nova-sprint hook --prove %s --actor %s\n", m.Challenge, m.Challenge, oneline.Field(name))
	case "push":
		lines := strings.Split(strings.TrimRight(m.Text, "\n"), "\n")
		if len(lines) == 0 || strings.HasPrefix(lines[0], "HAPPENED") {
			return // a HAPPENED push: not printed
		}
		fmt.Fprintf(w, "PUSH %d %s\n", m.ID, lines[0])
		if len(lines) > 1 {
			fmt.Fprintf(w, "  %s\n", lines[1])
		}
	case "bye":
		fmt.Fprintf(w, "HOOK BYE name=%s: %s\n", oneline.Field(name), oneline.Escape(m.Why))
	case "refused":
		fmt.Fprintf(w, "HOOK REFUSED name=%s: %s\n", oneline.Field(name), oneline.Escape(m.Why))
	}
}

// compactOverdue is one overdue look of the compact subscription: one OVERDUE line
// for every card-holding judgment (sprint.HoldsACard) the server's own judgment list
// still holds unanswered past overdue. A judgment that is answered leaves the list
// and is not named again; the source is the store's judgment list, never a poll of
// the inbox verb. (docs/CLI.md, `nova-sprint hook --compact`; docs/SPEC-SPRINT.md,
// "The hook".)
func (a *app) compactOverdue(ctx context.Context, redis, actor string, overdue time.Duration, out io.Writer) {
	st, err := a.storeCtx(ctx, common{redis: redis, actor: actor})
	if err != nil {
		return
	}
	v, err := st.Inbox(ctx, defaultDeadline, defaultStale, 10000)
	if err != nil {
		return
	}
	now := st.Now()
	for _, g := range v.Groups {
		if g.Kind != sprint.Judgment || !sprint.HoldsACard(g.Type) {
			continue // an alarm, a status, or a happened note holds no card
		}
		age := now.Sub(g.Oldest)
		if age < overdue {
			continue
		}
		fmt.Fprintf(out, "OVERDUE %s %s %s :: %s\n", orDashStr(g.Alias, g.ID), age.Round(time.Second), g.Type, strings.Join(g.Primaries, ","))
	}
}
