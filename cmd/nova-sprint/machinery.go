package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/subproc"
)

// The seat check (docs/SPEC-SPRINT.md, "The seat check"; the owner, 2026-10-04 1:52 PM:
// "what is manual needs a verb: nova-sprint seat check"): `nova-sprint seat check`
// measures the machinery under the sprint and prints one line per check, up or
// DOWN with the remedy; exit 1 on any red/DOWN check.
// The judging is internal/sprint; this file is the transport: what is measured,
// and how. Every reach outside the store goes through outside, so a test runs
// the check with no socket, no process and no clock of its own.

// DashboardEnv names the local dashboard the check reads; DashboardListen is
// the default.
const DashboardEnv = "NOVA_SPRINT_DASHBOARD"

// DashboardListenDefault is the default dashboard listen address.
const DashboardListenDefault = "127.0.0.1:7390"

// BusEnv names nova-bus2's Redis; unset is "not configured", never DOWN. It
// is dialed as the store is (openConn): the one fleet Redis seat every tool
// dials with, pkg/nsprint/redisauth, never a password variable of its own.
const BusEnv = "NOVA_BUS_REDIS"

// outside is every reach of the check past the store: each a function a test
// replaces. A served check (self: the server answering in its single-threaded
// step) runs none of httpGet, ping: the dashboard, the bus and unmeasured
// probes say not measured.
type outside struct {
	// serverAddr is the sprint's server as this process knows it: the address
	// NOVA_SPRINT_SERVER names, or self when this process is the server.
	serverAddr func() (addr string, self bool)
	// roundTrip sends the server one verb and says how long it took.
	roundTrip func(ctx context.Context, addr string) (time.Duration, error)
	// listenerPID is the pid listening on a local port, 0 unknown.
	listenerPID func(port string) int
	// dbsize is the store's DBSIZE, -1 when the store keeps none (the twin).
	dbsize func(ctx context.Context, addr string) int64
	// httpGet is one GET: the status and the body.
	httpGet func(ctx context.Context, url string) (int, []byte, error)
	// ping pings a Redis by address.
	ping func(ctx context.Context, addr string) error
	// hostname is this machine's short name.
	hostname func() string
	// devMergeQueue is the dev merge queue as measured (fp-mach-01).
	devMergeQueue func(ctx context.Context) (sprint.QueueM, error)
	// machineVersions is each machine's installed version against dev (fp-mach-01).
	machineVersions func(ctx context.Context, machines []string) (sprint.VersionsM, error)
	novaTools       func(bin string) sprint.NovaToolsProbe
	// hostProbe dials a member's host, named by its machine row (the tailnet host:
	// `ssh <name>` reaches it, pkg/config/kind.go); nil probes none, and no member is then
	// excused as offline. probeBound is the one bound over every probe of a check, run in
	// parallel; 0 is hostProbeBound.
	hostProbe  func(ctx context.Context, host string) hostAnswer
	probeBound time.Duration
}

// hostProbeBound is the overall bound of a check's host probes, all run at once.
const hostProbeBound = 3 * time.Second

// hostAnswer is what a probe of a host says: it answered, it gave a clear no-answer
// (offline), or neither (Err names why, and the member is not excused).
type hostAnswer struct {
	Answered bool
	Offline  bool
	Err      string
}

// dialHost is the real hostProbe: a TCP dial of the host's ssh port. A connection, or a
// refusal (the host's own stack answered), is an answer. Only a clear no-answer is
// offline: the dial timing out, or no route to the resolved address. A name that does
// not resolve is evidence of nothing about the host, and is said as an error.
func dialHost(ctx context.Context, host string) hostAnswer {
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, "22"))
	if err == nil {
		c.Close() // ignored: a probe connection
		return hostAnswer{Answered: true}
	}
	return classifyDial(err)
}

// classifyDial is the answer a failed dial gives.
func classifyDial(err error) hostAnswer {
	var dnsErr *net.DNSError
	var netErr net.Error
	switch {
	case errors.As(err, &dnsErr):
		return hostAnswer{Err: "its name does not resolve: " + dnsErr.Error()}
	case errors.Is(err, syscall.ECONNREFUSED):
		return hostAnswer{Answered: true}
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH),
		errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return hostAnswer{Offline: true}
	}
	return hostAnswer{Err: err.Error()}
}

// excuseOffline marks each member that does not beat (sprint.NeedsHostProbe) and whose
// host gives a clear no-answer as HostOffline, so the check says it down, not DOWN. A
// member is probed only when its machine row is in the inventory (machines). The seat's
// own network is proved by a control: a beating member of the inventory, probed in the
// same batch; when there is none, or it does not answer, no member is excused, since N
// offline hosts and the seat's own network down look the same from here. Every probe
// runs at once, under one bound.
func excuseOffline(ctx context.Context, probe func(context.Context, string) hostAnswer, bound time.Duration, fleet []sprint.MemberM, machines map[string]bool, inventoryErr error) {
	var targets []int
	control := ""
	for i, mm := range fleet {
		switch {
		case !sprint.NeedsHostProbe(mm):
			if control == "" && mm.Status != "held" && machines[mm.Name] {
				control = mm.Name
			}
		case inventoryErr != nil:
			fleet[i].HostErr = "not probed: the inventory was not read (" + inventoryErr.Error() + ")"
		case !machines[mm.Name]:
			fleet[i].HostErr = "not probed: no machine row names it"
		default:
			targets = append(targets, i)
		}
	}
	if len(targets) == 0 || probe == nil {
		return
	}
	if bound <= 0 {
		bound = hostProbeBound
	}
	ctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	hosts := make([]string, 0, len(targets)+1)
	for _, i := range targets {
		hosts = append(hosts, fleet[i].Name)
	}
	if control != "" {
		hosts = append(hosts, control)
	}
	answers := make([]hostAnswer, len(hosts))
	var wg sync.WaitGroup
	for j, h := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers[j] = probe(ctx, h)
		}()
	}
	wg.Wait()
	seatNet := ""
	switch {
	case control == "":
		seatNet = "not excused: no beating member to prove the seat's own network"
	case !answers[len(hosts)-1].Answered:
		seatNet = "not excused: control " + control + " did not answer either (the seat's own network?)"
	}
	for j, i := range targets {
		a := answers[j]
		switch {
		case a.Offline && seatNet == "":
			fleet[i].HostOffline = true
		case a.Offline:
			fleet[i].HostErr = seatNet
		case a.Answered:
			// the host answers and the member does not beat: a fault, said as before
		default:
			fleet[i].HostErr = a.Err
		}
	}
}

// realOutside is the check as it runs on a machine.
func (a *app) realOutside() outside {
	return outside{
		serverAddr: func() (string, bool) {
			if a.serveAddr != "" {
				return a.serveAddr, true
			}
			return a.getenv(ServerEnv), false
		},
		roundTrip: func(ctx context.Context, addr string) (time.Duration, error) {
			t := a.now()
			res, err := a.ask(ctx, addr, []string{"routes"}, nil)
			if err != nil {
				return 0, err
			}
			if res.Code != 0 {
				return 0, fmt.Errorf("routes answered exit %d: %s", res.Code, strings.TrimSpace(res.Stderr))
			}
			return a.now().Sub(t), nil
		},
		listenerPID: func(port string) int {
			cmd, cancel := subproc.Command(context.Background(), subproc.Tool, "lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN", "-t")
			defer cancel()
			out, err := cmd.Output()
			if err != nil {
				return 0
			}
			pid, _ := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]))
			return pid
		},
		dbsize: func(ctx context.Context, addr string) int64 {
			conn, ok := a.conns[addr]
			if !ok {
				return -1
			}
			n, err := conn.Client().DBSize(ctx).Result()
			if err != nil {
				return -1
			}
			return n
		},
		httpGet: func(ctx context.Context, url string) (int, []byte, error) {
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return 0, nil, err
			}
			resp, err := (&http.Client{Transport: a.transport}).Do(req)
			if err != nil {
				return 0, nil, err
			}
			defer resp.Body.Close() // ignored: a read response body
			body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			return resp.StatusCode, body, err
		},
		ping: func(ctx context.Context, addr string) error {
			conn, err := a.openConn(ctx, addr)
			if err != nil {
				return err
			}
			return conn.Close()
		},
		hostProbe: dialHost,
		hostname: func() string {
			h, _ := os.Hostname()
			return strings.SplitN(h, ".", 2)[0]
		},
	}
}

// seatCheck runs every probe against the store and the outside and judges the
// measures: the check as seat check and machinery print it.
func (a *app) seatCheck(ctx context.Context, st *store.Store, redisAddr string) sprint.SeatCheckReport {
	o := a.outside
	if o.serverAddr == nil {
		o = a.realOutside()
	}
	now := a.now()
	var host string
	if o.hostname != nil {
		host = o.hostname()
	}
	m := sprint.SeatCheckMeasures{
		Fleet:   []sprint.MemberM{},
		Friends: []sprint.FriendM{},
		Errs:    map[string]string{},
		Host:    host,
	}

	// 1. the server
	var addr string
	var self bool
	if o.serverAddr != nil {
		addr, self = o.serverAddr()
	}
	if addr == "" && !self {
		addr = a.seatServer() // the server seat install recorded, when the environment names none
	}
	m.Server = sprint.ServerM{Addr: addr, Self: self}
	switch {
	case self:
		m.Server.PID = os.Getpid()
	case addr != "":
		if o.roundTrip != nil {
			d, err := o.roundTrip(ctx, addr)
			if err != nil {
				m.Server.Err = err.Error()
			} else {
				m.Server.Millis = d.Milliseconds()
				if host, port, err := net.SplitHostPort(addr); err == nil && (host == "127.0.0.1" || host == "localhost" || host == "::1") {
					if o.listenerPID != nil {
						m.Server.PID = o.listenerPID(port)
					}
				}
			}
		}
	}

	// 2. the store, 3. the loop: the records the run loop writes
	m.Store = sprint.StoreM{Addr: redisAddr, DBSize: -1}
	mach, hb, err := st.Machine(ctx)
	if err != nil {
		m.Store.Err = err.Error()
	} else {
		if o.dbsize != nil {
			m.Store.DBSize = o.dbsize(ctx, redisAddr)
		}
		m.Store.Machine = strings.ToLower(mach.StateWord())
		if ep, err := st.EpochNow(ctx); err == nil {
			m.Store.Epoch = ep.N
		}
		if seen := hb.Alive(); !seen.IsZero() {
			m.Store.Tick = sprint.TickM{Ticked: true, Age: now.Sub(seen), Ticks: hb.Ticks, Failures: hb.Failures, Error: hb.Error}
		}
	}

	// 4. the fleet, 6. the readers: the tables as where shows them
	v, _, err := a.where(ctx, st, defaultStale, false)
	if err != nil {
		m.Errs[sprint.SeatCheckFleet], m.Errs[sprint.SeatCheckReaders] = err.Error(), err.Error()
	} else {
		members := sortedKeys(v.Tables[sprint.Fleet])
		beats, err := st.Beats(ctx, members)
		if err != nil {
			m.Errs[sprint.SeatCheckFleet] = err.Error()
		}
		for _, name := range members {
			b := beats[name]
			m.Fleet = append(m.Fleet, sprint.MemberM{Name: name, Status: cellText(v.Tables[sprint.Fleet][name][sprint.Status]), Beaten: b.Beaten(), Age: now.Sub(b.At)})
		}
		// a member that does not beat on a host that is offline is down, not a fault (the
		// owner, 2026-10-10: "Down is not an error, when they are actually down")
		if o.hostProbe != nil && a.inventory != nil && slices.ContainsFunc(m.Fleet, sprint.NeedsHostProbe) {
			machines := map[string]bool{}
			ictx, cancel := context.WithTimeout(ctx, 5*time.Second)
			inv, err := a.inventory(ictx, "")
			cancel()
			for _, w := range inv {
				machines[w.Machine] = true
			}
			excuseOffline(ctx, o.hostProbe, o.probeBound, m.Fleet, machines, err)
		}
		readers := sortedKeys(v.Tables[sprint.Readers])
		states, err := st.ReaderStates(ctx, readers, now)
		if err != nil {
			m.Errs[sprint.SeatCheckReaders] = err.Error()
		}
		m.Readers.Total = len(readers)
		for _, r := range readers {
			if states[r] == sprint.ReaderUp {
				m.Readers.Up++
			}
			n, _ := strconv.Atoi(cellText(v.Tables[sprint.Readers][r][sprint.Reading])) // ignored: a cell that is no number counts 0
			m.Readers.Reading += n
		}
	}

	// 5. the friends: the roster's statuses and beats, and their agents here
	rows, err := st.FriendRows(ctx, now)
	if err != nil {
		m.Errs[sprint.SeatCheckFriends] = err.Error()
	}
	beats, err := st.FriendBeats(ctx)
	if err != nil {
		m.Errs[sprint.SeatCheckFriends] = err.Error()
	}
	for _, r := range rows {
		b := beats[r.Name]
		m.Friends = append(m.Friends, sprint.FriendM{Name: r.Name, Status: r.Status, Beaten: b.Beaten(), Age: now.Sub(b.At)})
	}

	// 7. the dashboard
	dash := a.getenv(DashboardEnv)
	if dash == "" {
		dash = DashboardListenDefault
	}
	m.Dashboard = sprint.DashM{Addr: dash}
	if !self && o.httpGet != nil {
		status, body, err := o.httpGet(ctx, "http://"+dash+"/api/sprint")
		m.Dashboard.Status = status
		if err != nil {
			m.Dashboard.Err = err.Error()
		} else {
			var snap struct {
				Build string `json:"build"`
			}
			_ = json.Unmarshal(body, &snap) // ignored: a body that is no snapshot carries no build
			m.Dashboard.Build = snap.Build
		}
	}

	// 8. the bus
	m.Bus.Addr = a.getenv(BusEnv)
	if !self && m.Bus.Addr != "" && o.ping != nil {
		if err := o.ping(ctx, m.Bus.Addr); err != nil {
			m.Bus.Err = err.Error()
		}
	}

	// 9. the inbox
	in, err := st.Inbox(ctx, defaultDeadline, defaultStale, 10000)
	if err != nil {
		m.Errs[sprint.SeatCheckInbox] = err.Error()
	}
	for _, g := range in.Groups {
		if g.Kind != sprint.Judgment {
			continue
		}
		m.Inbox.Open++
		if w := now.Sub(g.Oldest); w > m.Inbox.Oldest {
			m.Inbox.Oldest = w
		}
	}

	// 10. the dev merge queue (fp-mach-01)
	if !self && o.devMergeQueue != nil {
		q, err := o.devMergeQueue(ctx)
		if err != nil {
			m.Errs[sprint.SeatCheckQueue] = err.Error()
		} else {
			q.Measured = true
			m.Queue = q
		}
	}

	// 11. each machine's installed version (fp-mach-01)
	if !self && o.machineVersions != nil {
		var machines []string
		for _, mm := range m.Fleet {
			machines = append(machines, mm.Name)
		}
		vers, err := o.machineVersions(ctx, machines)
		if err != nil {
			m.Errs[sprint.SeatCheckVersions] = err.Error()
		} else {
			vers.Measured = true
			m.Versions = vers
		}
	}

	// 12. the push proof: the holder's push record (pushproof.go)
	if holder, err := st.B.Coordinator(ctx); err != nil {
		m.Errs[sprint.SeatCheckPush] = err.Error()
	} else if holder != "" && pushArmed(holder) {
		rec, ok, err := readPush(ctx, st, holder)
		if err != nil {
			m.Errs[sprint.SeatCheckPush] = err.Error()
		}
		m.Push = sprint.PushM{Measured: true, Holder: holder, Record: rec, Recorded: ok}
	}

	return sprint.JudgeSeatCheck(m, now)
}

func (a *app) runSeatCheckVerb(verb string, args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup(verb)
	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, verb, argErr("takes no words ", err, pos...))
	}
	if code := a.refuseNovaTools(verb, stderr); code != 0 {
		return code
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, verb, err.Error())
	}
	r := a.withConfigSeat(a.seatCheck(context.Background(), st, c.redis))
	if c.json {
		fmt.Fprintln(stdout, r.JSON())
	} else {
		fmt.Fprint(stdout, r.Text())
	}
	return r.ExitCode
}

// cmdSeatCheck is `nova-sprint seat check`: the seat check on demand, read-only;
// exit 1 when anything is DOWN.
func (a *app) cmdSeatCheck(args []string, stdout, stderr io.Writer) int {
	return a.runSeatCheckVerb("seat check", args, stdout, stderr)
}

// cmdMachinery is `nova-sprint machinery`: an alias of `nova-sprint seat check`.
func (a *app) cmdMachinery(args []string, stdout, stderr io.Writer) int {
	return a.runSeatCheckVerb("machinery", args, stdout, stderr)
}

func init() {
	verbClasses["seat check"] = classRead
	verbClasses["machinery"] = classRead
}
