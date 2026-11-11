package verbs

import (
	"context"
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// WorkerUnit is one worker bench's service as the doctor reads it: the
// bench, its unit's name, the unit's last log line, or why the log could
// not be read.
type WorkerUnit struct {
	Machine  string `json:"machine"`
	Service  string `json:"service"`
	LastLine string `json:"last_line"`        // "" when the log is empty
	Unread   string `json:"unread,omitempty"` // why the log could not be read; "" when it was
}

// workerFix is how a worker's service record is brought to this build's
// command line: up writes every bench's record (cmd/nova-sprint workerArgv)
// and applies them.
const workerFix = "nova-sprint up --as <coordinator> (it rewrites every bench's worker service record and applies it)"

// checkWorkers is each worker bench's service, read from the fleet (the
// doctor: the benches in the inventory, each unit's last log line over
// ssh): red when the last line is a refusal (the worker exits on it and its
// unit restarts it, forever: D14, 2026-11-07, every bench's record carrying
// flags an earlier worker took, for days, while the rows read as hooked and
// up), red when the bench is not hooked and up on the bus (up's workers step) or
// the inventory cannot be read (no bench was checked), a WARN for a log that
// cannot be read from here.
func (e *Engine) checkWorkers(ctx context.Context, r *checkReport, snap store.Snapshot, who map[string]bus.Presence) {
	units, err := e.Doctor.Workers(ctx)
	if err != nil {
		r.red(RefusalOr(err, broken("workers: the benches cannot be read ("+errText(err)+")", "nova-config machine list --json on the control machine")))
		return
	}
	if len(units) == 0 {
		r.ok("workers: no bench in nova-config's inventory")
		return
	}
	flagged := 0
	for _, u := range units {
		switch f := workerFact(u, snap, who); {
		case f != nil:
			flagged++
			r.red(f)
		case u.Unread != "":
			flagged++
			r.warn("worker "+u.Machine+": its unit's last log line is unreadable ("+u.Unread+")", workerLogFix(u))
		}
	}
	if flagged == 0 {
		r.ok("workers: %d benches, each hooked, up and on the bus, its unit's last log line no refusal", len(units))
	}
}

// workerFact is what check and show say of one worker bench: nil when its
// worker serves (its unit's last log line, when read, is no refusal, and its
// row is hooked, up and on the bus) or its row is held on purpose, else the
// refusal with its fix. A log that cannot be read still has its row and bus
// checked (a powered-off bench is unreachable and off the bus), the reason
// standing in for the last line.
func workerFact(u WorkerUnit, snap store.Snapshot, who map[string]bus.Presence) *Fail {
	last := "the log is empty"
	switch {
	case u.Unread != "":
		last = "its log " + u.Unread
	case u.LastLine != "":
		last = fmt.Sprintf("last log line %q", u.LastLine)
	}
	row, has := snap.Rows[u.Machine]
	on, known := present(who, u.Machine)
	switch {
	case u.Unread == "" && strings.HasPrefix(u.LastLine, "SPRINT REFUSED"):
		return refuse(fmt.Sprintf("worker %s: %q (its unit exits on it and is restarted, forever)", u.Machine, u.LastLine), workerFix)
	case !has:
		return refuse(fmt.Sprintf("worker %s: no row in the store (%s)", u.Machine, last), workerLogFix(u))
	case row.Held:
		return nil
	case row.Session == "":
		return refuse(fmt.Sprintf("worker %s: not hooked in the store (%s)", u.Machine, last), workerLogFix(u))
	case row.State != "up":
		return refuse(fmt.Sprintf("worker %s: the store's row state is %s (%s)", u.Machine, row.State, last), workerLogFix(u))
	case known && !on:
		return refuse(fmt.Sprintf("worker %s: not on the bus (%s)", u.Machine, last), workerLogFix(u))
	}
	return nil
}

// workerLogFix is up's workers-step fix for one bench: its unit's log, or the
// bench role off a machine that is gone, then up.
func workerLogFix(u WorkerUnit) string {
	return "read ~/.local/state/nova/services/" + u.Service + ".log on " + u.Machine + " and fix what it says, or take the bench role off a machine that is gone; then nova-sprint up --as <coordinator>"
}

// rowView is show's row: the store's row and, for a worker bench, its
// service as check reads it, so the row carries the fact check prints.
type rowView struct {
	store.Row
	Worker *workerView `json:"worker,omitempty"`
}

// workerView is a bench's unit with check's red line for it, when any.
type workerView struct {
	WorkerUnit
	Problem string `json:"problem,omitempty"`
	Fix     string `json:"fix,omitempty"`
}

// showRow is row as show prints it: the row alone when it is no bench or
// the doctor is not configured; the bus read for the fact, unknown when it
// does not answer (check's bus line says so).
func (e *Engine) showRow(ctx context.Context, row store.Row, snap store.Snapshot) any {
	if e.Doctor == nil || e.isFriend(ctx, row.Name) {
		return row // bench-only checks never judge a friend row (docs/README.md "Hard requirements" 3)
	}
	u, isBench, err := e.Doctor.Worker(ctx, row.Name)
	switch {
	case err != nil:
		return rowView{Row: row, Worker: &workerView{WorkerUnit: WorkerUnit{Machine: row.Name, Unread: errText(err)}}}
	case !isBench:
		return row
	}
	view := &workerView{WorkerUnit: u}
	var who map[string]bus.Presence
	if e.Bus != nil {
		who, _ = e.Bus.Who(ctx) // nil when it does not answer: present then says unknown
	}
	if f := workerFact(u, snap, who); f != nil {
		view.Problem, view.Fix = f.What, f.Fix
	}
	return rowView{Row: row, Worker: view}
}

// isFriend says whether nova-config's roster names row a friend: the bench
// list is never read for one, so a bench-only failure (the control seat's
// machine inventory unreadable) never lands on a friend's row. An
// unreadable roster says no, and the bench read goes on as before.
func (e *Engine) isFriend(ctx context.Context, row string) bool {
	if e.Roster == nil {
		return false
	}
	ro, err := e.Roster.ReadRosterAt(ctx, 1) // the width cap moves no name in or out of Kin
	_, kin := ro.Kin[row]
	return err == nil && kin
}
