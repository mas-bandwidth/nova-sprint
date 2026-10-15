package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
)

// Bench grants are coordinator argv on the existing loopback line, never a
// worker verb. Their expiry uses this server's run-loop wake (tla/BenchGrant.tla).
func (a *app) cmdGrantAdd(args []string, out, errs io.Writer) int {
	return a.cmdGrant("add", args, out, errs)
}
func (a *app) cmdGrantExtend(args []string, out, errs io.Writer) int {
	return a.cmdGrant("extend", args, out, errs)
}
func (a *app) cmdGrantEnd(args []string, out, errs io.Writer) int {
	return a.cmdGrant("end", args, out, errs)
}
func (a *app) cmdGrantList(args []string, out, errs io.Writer) int {
	return a.cmdGrant("list", args, out, errs)
}

func (a *app) cmdGrant(op string, args []string, stdout, stderr io.Writer) int {
	name := "grant " + op
	fs, c := a.verbSetup(name)
	bench, to := fs.String("bench", "", "bench machine name"), fs.String("to", "", "fleet member or friend row")
	duration := fs.Duration("for", 0, "duration from server now: 1m through 720h")
	pos, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	r := sprint.GrantRequest{Op: op, Bench: *bench, Grantee: *to, For: *duration}
	want := 0
	if op == "extend" || op == "end" {
		want = 1
	}
	if len(pos) != want {
		return refuse(stderr, name, fmt.Sprintf("wants %d positional arguments; run: nova-sprint %s --help", want, name))
	}
	if want == 1 {
		r.ID = pos[0]
	}
	if op != "add" && (*bench != "" || *to != "") {
		return refuse(stderr, name, "--bench and --to belong to grant add")
	}
	if (op == "end" || op == "list") && *duration != 0 {
		return refuse(stderr, name, "--for belongs to grant add or extend")
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	ctx := context.Background()
	if op == "list" {
		gs, err := st.Grants(ctx)
		if err != nil {
			return a.readFailed(name, err, stderr)
		}
		a.outcome, a.changed = "accepted", "no"
		if gs.Items == nil {
			gs.Items = []sprint.Grant{}
		}
		if c.json {
			b, _ := json.Marshal(struct {
				Items   []sprint.Grant `json:"items"`
				Outcome string         `json:"outcome"`
				Changed string         `json:"changed"`
			}{gs.Items, a.outcome, a.changed})
			fmt.Fprintln(stdout, string(b))
		} else {
			for _, g := range gs.Items {
				b, _ := json.Marshal(g)
				fmt.Fprintln(stdout, string(b))
			}
		}
		return 0
	}
	g, res, err := st.ChangeGrant(ctx, r, c.op)
	if err != nil {
		if errors.Is(err, store.ErrUnknown) {
			a.outcome, a.changed = "unknown", "unknown"
			fmt.Fprintln(stderr, err)
			return 2
		}
		return refuse(stderr, name, err.Error())
	}
	if len(res.Refused) > 0 {
		return refuse(stderr, name, res.Refused[0].Why)
	}
	a.outcome, a.changed = "accepted", "yes"
	if res.Replay || len(res.Moved) == 0 {
		a.changed = "no"
	}
	b, _ := json.Marshal(struct {
		sprint.Grant
		Outcome string `json:"outcome"`
		Changed string `json:"changed"`
	}{g, a.outcome, a.changed})
	fmt.Fprintln(stdout, string(b))
	return 0
}

// An empty record needs no bus configuration or sender resolution. The ordinary
// run loop remains usable before any bench grant exists.
func (a *app) prepareGrantDeliveries(ctx context.Context, st *store.Store) ([]sprint.Grant, string, error) {
	gs, err := st.Grants(ctx)
	if err != nil || len(gs.Items) == 0 {
		return nil, "", err
	}
	login, loginErr := a.coordinatorBusLogin()
	// A broken current bus configuration blocks new sender freezing and sends,
	// but an old pending receipt still reaches its judgment at RetryBefore.
	pending, err := st.PrepareGrantDeliveries(ctx, login, loginErr == nil, bus.DefaultTokenLife, bus.DefaultTokenCleanup)
	if err != nil {
		return nil, login, err
	}
	if loginErr != nil {
		return nil, login, loginErr
	}
	return pending, login, nil
}

// sendGrantDeliveries is the off-line half of the existing run-loop wake.
// Preparation ran under its bounded line; receipts take it only after bus I/O.
func (a *app) sendGrantDeliveries(ctx context.Context, st *store.Store, pending []sprint.Grant, login string) error {
	for _, g := range pending {
		d := g.Delivery
		if !a.now().Before(d.RetryBefore) {
			continue
		}
		if why := coordinatorSenderRefusal(d.From, login); why != "" {
			a.serveSay("grant %s: %s", g.ID, why)
			continue
		}
		m := bus.Message{From: d.From, To: strings.Split(d.To, ","), Token: d.Token, Kind: bus.KindStatus, Subject: d.Subject}
		if err := a.bus(ctx, m, func(s string) { a.serveSay("%s", s) }); err != nil {
			a.serveSay("grant %s: %s", g.ID, err)
			continue
		}
		receiptCtx, cancel := context.WithTimeout(ctx, hookReadBound)
		if err := a.serial.LockCtx(receiptCtx); err != nil {
			cancel()
			return err
		}
		err := st.MarkGrantSent(receiptCtx, g.ID, d.Token)
		a.serial.Unlock()
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}
