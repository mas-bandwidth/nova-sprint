package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

// workerHarness is the harness a worker service runs cards with:
// nova-config's machines carry no harness field, so this one default.
const workerHarness = "opencode"

// workerProviderKeys are the provider keys a worker service opens from
// its bench's own seat, beside the sprint and bus credentials.
func workerProviderKeys() []string {
	return []string{"DEEPSEEK_API_KEY", "INCEPTION_API_KEY"}
}

// workerArgv is the one place the worker service's command line is built.
// The directories are under the login's home on the bench (the worker
// resolves a relative directory there, services.Worker); a worker that
// stages one clone per stream repo changes --work-clone here only.
func workerArgv(machine, _ string) []string {
	return []string{"nova-sprint", "worker", "--as", machine,
		"--work-root", ".local/share/nova-sprint/work",
		"--harness", workerHarness,
		"--providers", workerProviders,
		"--root", ".local/state/nova-worker/" + machine,
		"--state-dir", ".local/state"}
}

// workerProviders is the worker's key-proxy routes (edge/worker
// ParseProviders; an upstream is given without /v1): DeepSeek and
// Inception (Mercury). Paper Instruments (Ultramarine) is left out until
// its upstream is known; adding a route is this string and its key in
// workerProviderKeys.
const workerProviders = "deepseek=https://api.deepseek.com@DEEPSEEK_API_KEY,inception=https://api.inceptionlabs.ai@INCEPTION_API_KEY"

// workerService is the nova-config service record of the worker on a
// bench: its seat is the bench's own, its keys the sprint credential, the
// github token its git clones, fetches and pushes take as their one
// credential (forge.TokenEnv; the worker refuses to start without it), the
// bus password when the bus has a user (as sprint-run's), and the provider
// keys; its environment names the same store and bus as sprint-run's.
func workerService(machine string, run verbs.FleetService, runAddress string) verbs.FleetService {
	env := map[string]string{"NOVA_PG_DSN": benchReachable(run.Env["NOVA_PG_DSN"], runAddress), "NOVA_BUS_REDIS": benchReachable(run.Env["NOVA_BUS_REDIS"], runAddress)}
	keys := append([]string{"NOVA_SPRINT_PG_PASSWORD", forge.TokenEnv}, workerProviderKeys()...)
	if user := run.Env["NOVA_BUS_USER"]; user != "" {
		env["NOVA_BUS_USER"] = user
		keys = append(keys, "NOVA_BUS_PASSWORD")
	}
	return verbs.FleetService{Name: "sprint-worker-" + machine, Machine: machine, Seat: machine, Argv: workerArgv(machine, run.Machine), Keys: keys, Env: env}
}

// loopbackAddr is true when addr (a URL such as the store's DSN, or a
// host:port such as the bus's) names a loopback host: localhost in any
// case, 127.0.0.0/8 or ::1.
func loopbackAddr(addr string) bool {
	host := ""
	if u, err := url.Parse(addr); err == nil && u.Host != "" {
		host = u.Hostname()
	} else if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

// benchReachable is addr as a bench reaches it: a loopback host is the run
// machine's, so it becomes runAddress, the run machine's fleet address,
// with user, port, database and query kept. Any other host, an empty
// runAddress or an address it cannot parse is kept as it is.
func benchReachable(addr, runAddress string) string {
	if runAddress == "" || !loopbackAddr(addr) {
		return addr
	}
	if u, err := url.Parse(addr); err == nil && u.Host != "" {
		host := runAddress
		if port := u.Port(); port != "" {
			host = net.JoinHostPort(runAddress, port)
		}
		u.Host = host
		return u.String()
	}
	_, port, _ := net.SplitHostPort(addr)
	return net.JoinHostPort(runAddress, port)
}

// serviceLogPath is where nova-fleet's unit for service appends its output
// (the record's default log), relative to the login's home.
func serviceLogPath(service string) string {
	return filepath.Join(".local", "state", "nova", "services", service+".log")
}

// productionWorkload is up's steps 6 to 8 on a real fleet
// (verbs.SetupWorkload): services writes as up's actor; run is the
// sprint-run record the workers share the store and bus with; store and
// present observe the rows; lastLine reads a unit's last log line.
type productionWorkload struct {
	services verbs.FleetServices
	run      verbs.FleetService
	store    verbs.Reader
	present  func(context.Context) (map[string]bool, error)
	lastLine func(ctx context.Context, machine, service string) string
	version  func(ctx context.Context, machine string) error // nil: this bench runs this binary's version
}

// ServicesReady is true: step 6's keys are proven by their model probe
// (verbs.ServiceKey) and "none" needs nothing; the worker records that
// open the provider keys are step 7's, proven by the workers coming up.
func (productionWorkload) ServicesReady(context.Context, []string) (bool, error) { return true, nil }

// InstallServices has nothing past the keys to install (ServicesReady).
func (productionWorkload) InstallServices(context.Context, []string) error { return nil }

// InstallWorkers writes one worker service record per worker bench (the
// seats were granted the sprint credential first, verbs.RuntimeSetup) and
// applies them all in one nova-fleet play limited to those benches.
func (w productionWorkload) InstallWorkers(ctx context.Context) error {
	machines, err := w.services.Machines(ctx)
	if err != nil {
		return serviceConfigFailure("cannot inspect machine inventory: ", err)
	}
	benches := verbs.WorkerBenches(machines)
	runAddress := ""
	for _, m := range machines {
		if m.Name == w.run.Machine {
			runAddress = m.Address
		}
	}
	if w.services.Command == nil {
		return fmt.Errorf("worker setup unavailable: no fleet command is wired")
	}
	if len(benches) == 0 {
		return nil
	}
	if runAddress == "" && (loopbackAddr(w.run.Env["NOVA_PG_DSN"]) || loopbackAddr(w.run.Env["NOVA_BUS_REDIS"])) {
		return &verbs.OperationError{Problem: "the store or bus is on loopback on run machine " + w.run.Machine + ", which has no fleet address in the machine inventory, so no bench could reach it", Fix: "nova-config machine set " + w.run.Machine + " --address <MagicDNS name> --actor <you> --reason <why>, then nova-sprint up again", Exit: 1}
	}
	names := make([]string, 0, len(benches))
	for _, bench := range benches {
		s := workerService(bench, w.run, runAddress)
		if _, err = w.services.Ensure(ctx, s); err != nil {
			return err
		}
		names = append(names, s.Name)
	}
	_, err = w.services.Command(ctx, "nova-fleet", "services", "--", "-l", strings.Join(benches, ","), "-e", "nova_service_only="+strings.Join(names, ","))
	if err != nil {
		return verbs.RefusalOr(err, &verbs.OperationError{Problem: edge.RedactN("nova-fleet services for the workers on "+strings.Join(benches, ",")+" failed: "+err.Error(), edge.FailureMax+400), Fix: "fix what it says for each machine it names (nova-fleet check), or take the bench role off a machine that is gone, then nova-sprint up again", Exit: 1})
	}
	return nil
}

// benchState is one worker bench as steps 7 and 8 see it.
type benchState struct {
	name, row string // row: the store row's state, "no row" when absent
	hooked    bool   // its name is on the bus
	cores     *int
}

func (b benchState) up() bool { return b.hooked && b.row == "up" }

// benchStates reads every worker bench's bus presence, store row and cores
// in one inventory read, one snapshot and one nova-bus who.
func (w productionWorkload) benchStates(ctx context.Context) ([]benchState, error) {
	machines, err := w.services.Machines(ctx)
	if err != nil {
		return nil, serviceConfigFailure("cannot inspect machine inventory: ", err)
	}
	snap, err := w.store.Snapshot(ctx)
	if err != nil {
		return nil, edge.Cause("cannot read the store", err)
	}
	present, err := w.present(ctx)
	if err != nil {
		return nil, err
	}
	cores := map[string]*int{}
	for _, m := range machines {
		cores[m.Name] = m.Cores
	}
	var states []benchState
	for _, bench := range verbs.WorkerBenches(machines) {
		row := "no row"
		if r, ok := snap.Rows[bench]; ok {
			row = r.State
		}
		states = append(states, benchState{name: bench, row: row, hooked: present[bench], cores: cores[bench]})
	}
	return states, nil
}

// WorkersReady is every worker bench hooked on the bus and up in the
// store, and running this binary's version; else the benches that are not,
// each with its unit's last log line. A worker probes its harness with the
// policy's models, its seat's provider keys in hand, before it hooks
// (worker.Runner.ServeRow), so hooked and up is probed (admission, intent
// rule 34).
func (w productionWorkload) WorkersReady(ctx context.Context) (bool, error) {
	states, err := w.benchStates(ctx)
	if err != nil {
		return false, err
	}
	var down []benchState
	for _, b := range states {
		if !b.up() {
			down = append(down, b)
		}
	}
	if len(down) > 0 {
		return false, &verbs.WorkloadNotReady{Why: w.downWhy(ctx, down), Fix: "read the unit's log on each bench named (~/" + serviceLogPath("sprint-worker-<bench>") + ") and fix what it says (a probe refusal names the model or key it lacks), or take the bench role off a machine that is gone; then nova-sprint up again"}
	}
	return w.versionsReady(ctx, states)
}

// versionsReady is every bench's nova-sprint at this binary's version, the
// benches read at once (step 5's check, over ssh).
func (w productionWorkload) versionsReady(ctx context.Context, states []benchState) (bool, error) {
	if w.version == nil {
		return false, fmt.Errorf("worker setup unavailable: no version check is wired")
	}
	errs := make([]error, len(states))
	var wg sync.WaitGroup
	for i, b := range states {
		wg.Go(func() { errs[i] = w.version(ctx, b.name) })
	}
	wg.Wait()
	var why []string
	var first error
	for _, err := range errs {
		if err != nil {
			why, first = append(why, err.Error()), cmp.Or(first, err)
		}
	}
	if len(why) == 0 {
		return true, nil
	}
	fix := "nova-fleet install --repo <org/repo> --tag <this version> <bench> for each bench named, then nova-sprint up again"
	var op *verbs.OperationError
	if errors.As(first, &op) {
		fix = op.Fix
	}
	return false, &verbs.WorkloadNotReady{Why: strings.Join(why, "; "), Fix: fix}
}

// downWhy names each bench that is not up and its unit's last log line,
// the logs read concurrently.
func (w productionWorkload) downWhy(ctx context.Context, down []benchState) string {
	lines := make([]string, len(down))
	var wg sync.WaitGroup
	for i, b := range down {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lines[i] = w.lastLine(ctx, b.name, "sprint-worker-"+b.name)
		}()
	}
	wg.Wait()
	parts := make([]string, len(down))
	for i, b := range down {
		bus := "not on the bus"
		if b.hooked {
			bus = "on the bus"
		}
		parts[i] = fmt.Sprintf("%s (%s, store row %s; last log line: %s)", b.name, bus, b.row, lines[i])
	}
	return fmt.Sprintf("%d of the worker benches are not hooked and up: %s", len(down), strings.Join(parts, "; "))
}

// SmokeReady is the first deal made ready: every worker row hooked, up and
// dealable, a width from its cores (a bench with none is held at width 0,
// roster.NoCores). A fresh sprint is stopped until start and moves
// nothing, so up deals no card itself; the run deals once started.
func (w productionWorkload) SmokeReady(ctx context.Context) (bool, error) {
	states, err := w.benchStates(ctx)
	if err != nil {
		return false, err
	}
	var why []string
	fix := "nova-sprint up again"
	for _, b := range states {
		switch {
		case !b.up():
			why = append(why, fmt.Sprintf("%s is not hooked and up (store row %s)", b.name, b.row))
		case b.cores == nil || *b.cores < 1:
			why = append(why, b.name+" is held at width 0: "+roster.NoCores)
			fix = "nova-config machine set " + b.name + " --cores <n> --actor <you> --reason <why> (each bench named), then nova-sprint up again"
		}
	}
	if len(why) == 0 {
		return true, nil
	}
	return false, &verbs.WorkloadNotReady{Why: "not every worker row is dealable: " + strings.Join(why, "; "), Fix: fix}
}

// RunSmoke has nothing to act: what SmokeReady needs is a worker coming up
// (step 7) or a core count in nova-config, which its refusal names.
func (productionWorkload) RunSmoke(context.Context) error { return nil }

// workerUnit is machine's worker service as check and show read it
// (verbs.Doctor.Workers): its unit's last log line, or why it is unreadable.
func (p productionSetup) workerUnit(ctx context.Context, machine string) verbs.WorkerUnit {
	u := verbs.WorkerUnit{Machine: machine, Service: "sprint-worker-" + machine}
	u.LastLine, u.Unread = p.serviceLogLine(ctx, machine, u.Service)
	return u
}

// lastServiceLogLine is the last line of service's unit log on machine,
// quoted; "(the log is empty)"; or what failed when it cannot be read.
func (p productionSetup) lastServiceLogLine(ctx context.Context, machine, service string) string {
	line, unread := p.serviceLogLine(ctx, machine, service)
	switch {
	case unread != "":
		return unread
	case line == "":
		return "(the log is empty)"
	}
	return fmt.Sprintf("%q", line)
}

// serviceLogLine is the last line of service's unit log on machine, over
// ssh to the inventory's address ("" when the log is empty), or, second,
// why it cannot be read.
func (p productionSetup) serviceLogLine(ctx context.Context, machine, service string) (line, unread string) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	destination, err := p.sshDestination(ctx, machine)
	if err != nil {
		return "", "unreadable (" + edge.RedactN(err.Error(), 200) + ")"
	}
	out, err := p.smallCommand(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=yes", "--", destination, "tail", "-n", "1", serviceLogPath(service))
	if err != nil {
		return "", "unreadable over ssh to " + destination + " (" + edge.RedactN(err.Error(), 200) + ")"
	}
	text := strings.TrimSpace(string(out))
	return edge.RedactN(text[strings.LastIndex(text, "\n")+1:], 300), ""
}
