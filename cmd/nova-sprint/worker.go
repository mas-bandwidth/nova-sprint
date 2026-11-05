package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/worker"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/hostload"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
	"github.com/mas-bandwidth/nova-tools/pkg/safepath"
	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
)

// The fleet worker (docs: WORKER-INVENTORY.md): one process per machine works both its
// rows of the sprint, the machine's work row and its reader row reader-<machine>, by
// passes against the sprint's server. It replaces nova-swarm member and its --reader.
func init() {
	verbClasses["worker"] = classMachine
	notServed = append(notServed, "worker")
	verbExit["worker"] = "exit codes: 0 stopped (SIGTERM drained, or an interrupt), 1 refused at start (no server named, no harness, no nova-sandbox, no pool identity), 2 usage, 3 its binary was replaced (its supervisor starts the new one)"
	verbEffect["worker"] = "runs until stopped: every pass it beats the machine's row, reads both rows' queues from the sprint's server, takes up to their width, stages each card from the bench mirror into <root>/<work|read>/slots, runs the harness under nova-sandbox to the card's deadline and token budget, pushes a work card's commit to origin's branch from <root>/work/push.git, and finishes or reads each card through the server; it opens no store"
	w := verb{"worker", "--as <machine> [--harness <path>] [--root <dir>]", "worker --as hetzner2", (*app).cmdWorker}
	verbs = slices.Insert(verbs, len(verbs)-1, w) // before coordinator, whose example moves the seat
}

// refuseStart is a refusal of what the machine lacks, not of the words: exit 1.
func refuseStart(stderr io.Writer, verb, what string) int {
	refuse(stderr, verb, what)
	return 1
}

// workerEvery is the time between a worker's passes and beats.
const workerEvery = 3 * time.Second

// workerDiskFloor is the free space below which a worker starts no card.
const workerDiskFloor = 10 << 30

func (a *app) cmdWorker(args []string, stdout, stderr io.Writer) int {
	const name = "worker"
	fs, _ := a.verbSetup(name)
	as := fs.String("as", "", "required: this machine's name, its row in the fleet table; its reader row is reader-<name>")
	harness := fs.String("harness", "", "the opencode binary each card runs under (default: opencode on PATH)")
	root := fs.String("root", "", "the directory the worker's launches, push repository and catalog live under (default: ~/nova-bench/run/worker-<as>); its identity.tsv is the pool identity every child commits under")
	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, name, argErr("takes no words ", err, pos...))
	}
	if *as == "" || !safepath.NameOK(*as) {
		return refuse(stderr, name, "wants --as <machine>, its row in the fleet table (letters, digits, - _ .)")
	}
	addr, _ := a.namedServer()
	if addr == "" {
		return refuseStart(stderr, name, a.noServerHint())
	}
	home := a.getenv("HOME")
	if *root == "" {
		*root = filepath.Join(home, "nova-bench", "run", "worker-"+*as)
	}
	look := func(bin string) (string, error) {
		p, err := exec.LookPath(bin)
		if err != nil {
			return "", err
		}
		return filepath.Abs(p)
	}
	if *harness == "" {
		*harness = "opencode"
	}
	hp, err := look(*harness)
	if err != nil {
		return refuseStart(stderr, name, "the harness "+*harness+" cannot be run: "+err.Error())
	}
	wall := a.workerWall
	if wall == "" {
		if wall, err = look("nova-sandbox"); err != nil {
			return refuseStart(stderr, name, "no nova-sandbox on PATH: a card never runs without its wall; run: nova-update install nova-sandbox")
		}
	}
	id, err := swarm.LoadPoolIdentity(*root)
	if err != nil {
		return refuseStart(stderr, name, err.Error())
	}
	send := a.workerSend
	if send == nil {
		send = (&sprintwire.Client{Addr: addr}).Do
	}
	catalog := refreshCatalog(hp, *root)
	fmt.Fprintf(stdout, "WORKER as=%s server=%s harness=%s root=%s catalog=%s\n", oneline.Field(*as), oneline.Field(addr), oneline.Field(hp), oneline.Field(*root), oneline.Field(catalog))
	if catalog == "" {
		fmt.Fprintln(stdout, "NOTE catalog: the harness gave no model list; each launch reads its own")
	}
	if _, err := exec.LookPath("sqlite3"); err != nil {
		fmt.Fprintln(stdout, "NOTE budget: no sqlite3 on PATH, so no card's token spend can be read: each runs to its deadline alone, and reports no usage")
	}
	env := os.Environ()
	usage := worker.OpenCodeUsage(worker.SQLiteQuery)
	launch := func(sub string, pusher *worker.Pusher) *worker.Launch {
		l := &worker.Launch{Bench: *as, Root: filepath.Join(*root, sub), Home: home, Harness: hp, Wall: wall, Env: env,
			Identity: id, Catalog: catalog, Pusher: pusher, Usage: usage, Spawn: worker.ExecProc, SampleEvery: 15 * time.Second}
		if n := l.Sweep(); n > 0 {
			fmt.Fprintf(stdout, "NOTE sweep: removed %d launch directories under %s\n", n, oneline.Field(l.Root))
		}
		return l
	}
	room := func() (bool, string) {
		free, err := worker.FreeBytes(*root)
		switch {
		case err != nil:
			return false, "the free space under " + *root + " cannot be read: " + err.Error() + "; run: df -h " + *root
		case free < workerDiskFloor:
			return false, fmt.Sprintf("%d GiB free under %s, below the %d GiB floor; run: df -h %s", free>>30, *root, workerDiskFloor>>30, *root)
		}
		return true, ""
	}
	var prev hostload.State
	load := func(now time.Time) float64 {
		pct, _, next, ok := hostload.Measure(a.meter, prev, now)
		prev = next
		if !ok {
			return 0
		}
		return pct
	}
	work := worker.New(worker.Config{As: *as, Launcher: launch("work", &worker.Pusher{Repo: filepath.Join(*root, "work", "push.git"), Now: time.Now}),
		Load: load, Facts: machineFacts(home), Room: room, Out: stdout}, send)
	read := worker.New(worker.Config{As: "reader-" + *as, Reader: true, Launcher: launch("read", nil), Room: room, Out: stdout}, send)
	return a.workerLoop([]*worker.Worker{work, read}, stdout, stderr)
}

// workerLoop runs passes until stopped. A SIGTERM drains (nothing taken, each lane
// reported as it ends) for at most the longest deadline held, capped by DrainMost, then
// ends what is left; an interrupt ends every lane at once; a replaced binary drains and
// exits 3.
func (a *app) workerLoop(rows []*worker.Worker, stdout, stderr io.Writer) int {
	term, intr := make(chan os.Signal, 1), make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	signal.Notify(intr, os.Interrupt)
	defer signal.Stop(term)
	defer signal.Stop(intr)
	began := a.binaryStamp()
	running := func() (n int) {
		for _, w := range rows {
			n += w.Running()
		}
		return n
	}
	stopAll := func() (n int) {
		for _, w := range rows {
			n += w.StopAll()
		}
		return n
	}
	drain := func() {
		for _, w := range rows {
			w.Drain()
		}
	}
	draining, exit := "", 0
	var until time.Time
	for n := 1; ; n++ {
		select {
		case <-intr:
			fmt.Fprintf(stdout, "WORKER STOP interrupt: ended %d lane(s)\n", stopAll())
			return 0
		case <-term:
			if draining == "" {
				draining = "SIGTERM"
				drain()
			}
		default:
		}
		now := a.now()
		if draining == "" && began != "" && a.binaryStamp() != began {
			draining, exit = "replaced", exitReplaced
			drain()
		}
		if draining != "" && until.IsZero() {
			var longest time.Duration
			for _, w := range rows {
				longest = max(longest, w.LongestDeadline())
			}
			until = now.Add(worker.DrainBound(longest))
			fmt.Fprintf(stdout, "WORKER DRAIN %s: taking no new card, %d running, ending what is left by %s\n", draining, running(), until.Format("15:04:05"))
		}
		if draining != "" && running() == 0 {
			fmt.Fprintf(stdout, "WORKER STOP %s: nothing running\n", draining)
			return exit
		}
		if !until.IsZero() && !now.Before(until) {
			fmt.Fprintf(stdout, "WORKER STOP %s: the drain's bound passed; ended %d lane(s), which the sprint deals again\n", draining, stopAll())
			return exit
		}
		rows[0].Beat(now)
		for _, w := range rows {
			acted, err := w.Tick(now)
			if err != nil {
				fmt.Fprintf(stderr, "%s worker: pass %d: %s\n", prog, n, oneline.Escape(err.Error()))
			}
			if acted > 0 {
				fmt.Fprintf(stdout, "pass %d acted=%d running=%d %s\n", n, acted, running(), now.Format("15:04:05"))
			}
		}
		if a.workerTicks > 0 && n >= a.workerTicks {
			fmt.Fprintf(stdout, "WORKER STOP after %d passes: ended %d lane(s)\n", n, stopAll())
			return 0
		}
		select {
		case <-a.after(workerEvery):
		case <-term:
			if draining == "" {
				draining = "SIGTERM"
				drain()
			}
		case <-intr:
			fmt.Fprintf(stdout, "WORKER STOP interrupt: ended %d lane(s)\n", stopAll())
			return 0
		}
	}
}

// refreshCatalog runs the harness's model list once into <root>/catalog/models.json,
// for every launch to read in place of fetching its own inside the wall; it returns the
// file, or "" when the harness gave none and none is kept from before.
func refreshCatalog(harness, root string) string {
	dst := filepath.Join(root, "catalog", "models.json")
	home := filepath.Join(root, "catalog", "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, harness, "models")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "XDG_CACHE_HOME=" + filepath.Join(home, ".cache")}
	_ = cmd.Run()
	var m map[string]json.RawMessage
	if b, err := os.ReadFile(filepath.Join(home, ".cache", "opencode", "models.json")); err == nil && json.Unmarshal(b, &m) == nil && len(m) > 0 {
		tmp := dst + ".tmp"
		if os.WriteFile(tmp, b, 0o644) == nil && os.Rename(tmp, dst) == nil {
			return dst
		}
	}
	if _, err := os.Stat(dst); err == nil {
		return dst
	}
	return ""
}

// machineFacts are what the machine offers a card's gate, sent on every beat: its OS and
// architecture, landlock, kvm, a TLC bench, and the toolchains on its PATH.
func machineFacts(home string) []string {
	facts := []string{runtime.GOOS, runtime.GOARCH}
	if runtime.GOOS == "linux" {
		if b, err := os.ReadFile("/sys/kernel/security/lsm"); err == nil && slices.Contains(strings.Split(strings.TrimSpace(string(b)), ","), "landlock") {
			facts = append(facts, "landlock")
		}
		if f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0); err == nil {
			_ = f.Close()
			facts = append(facts, "kvm")
		}
	}
	if _, err := os.Stat("/opt/tla/tla2tools.jar"); err == nil {
		if _, err := exec.LookPath("tlacheck"); err == nil {
			facts = append(facts, "tla")
		} else if _, err := os.Stat(filepath.Join(home, ".local", "bin", "tlacheck")); err == nil {
			facts = append(facts, "tla")
		}
	}
	for _, tool := range []string{"go", "dotnet", "java"} {
		if _, err := exec.LookPath(tool); err == nil {
			facts = append(facts, tool)
		}
	}
	return facts
}
