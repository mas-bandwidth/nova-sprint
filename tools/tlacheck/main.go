// Command tlacheck is the runner of the TLA+ models under tla/: it names the
// groups to run and whether they are stale (groups), lists what a case reads
// (inputs), runs a selection of cases with TLC and writes their records (run),
// and joins records into tla/RUNS.tsv (merge).
//
// It is built from this tree's pkg/tlc, so the fingerprints it computes are the
// checkout's own: CheckRunner refuses a binary built from other runner files.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/tlc"
)

const usage = `tlacheck runs and reads the TLA+ models under tla/.

usage: tlacheck <verb> [flags] [args]

verbs:
  groups   [--root .] [--stale]
           print the groups of the case plan as a JSON list; with --stale,
           print the groups that hold a case with no current record.
  inputs   [--root .] <config>
           print what a case reads, one path per line.
  run      [--root .] --dir <dir> [--jar <jar>] [--java <java>]
           [--group <group>] [--shards <n>] [--shard <i>] [--timeout <dur>]
           [--workers <n>] [--parallel <n>] [--manual]
           run the selected cases with TLC and write <dir>/RUNS.tsv.
  merge    [--root .] [--keep <file>] [--out <file>] <runs>...
           merge run records into one complete records file, in plan order.

The jar is --jar or $TLC_JAR; java is --java or the java on PATH. A run is
bounded by --timeout (default 110s); --manual lifts the bound for an explicit
bench experiment.
`

// The exit statuses of the command: 0 the work was done, 1 the work was done
// and a result was not what its case declares, 2 the command refused before it
// did anything.
const (
	exitOK      = 0
	exitFailed  = 1
	exitRefused = 2
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run is the whole command: it is a function so tests drive every verb without
// a process.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitRefused
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	case "groups":
		return cmdGroups(args[1:], stdout, stderr)
	case "inputs":
		return cmdInputs(args[1:], stdout, stderr)
	case "run":
		return cmdRun(args[1:], stdout, stderr)
	case "merge":
		return cmdMerge(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "tlacheck: unknown verb %q\n\n%s", args[0], usage)
		return exitRefused
	}
}

// refuse prints a refusal and returns exitRefused.
func refuse(stderr io.Writer, verb string, err error) int {
	fmt.Fprintf(stderr, "tlacheck %s: %v\n", verb, err)
	return exitRefused
}

// fail prints a failure and returns exitFailed.
func fail(stderr io.Writer, verb string, err error) int {
	fmt.Fprintf(stderr, "tlacheck %s: %v\n", verb, err)
	return exitFailed
}

// readRuns reads root/tla/RUNS.tsv; a tree with no records reads as none.
func readRuns(root string) ([]tlc.Record, error) {
	path := filepath.Join(root, "tla", tlc.RunsFile)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return tlc.ReadRecordsFile(path)
}

func cmdGroups(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("groups", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "checkout root; the models are <root>/tla")
	stale := fs.Bool("stale", false, "print only the groups that hold a case with no current record")
	if err := fs.Parse(args); err != nil {
		return exitRefused
	}
	if fs.NArg() != 0 {
		return refuse(stderr, "groups", errors.New("groups takes no arguments"))
	}
	cases, err := tlc.LoadCases(*root)
	if err != nil {
		return refuse(stderr, "groups", err)
	}
	if err := tlc.CheckRunner(*root); err != nil {
		return refuse(stderr, "groups", err)
	}
	var out []string
	if *stale {
		src, err := tlc.SourceAt(*root)
		if err != nil {
			return refuse(stderr, "groups", err)
		}
		records, err := readRuns(*root)
		if err != nil {
			return refuse(stderr, "groups", err)
		}
		out, err = tlc.StaleGroups(src, cases, records)
		if err != nil {
			return refuse(stderr, "groups", err)
		}
	} else {
		out = tlc.RequiredGroups(cases)
	}
	if out == nil {
		out = []string{}
	}
	if err := json.NewEncoder(stdout).Encode(out); err != nil {
		return refuse(stderr, "groups", err)
	}
	return exitOK
}

func cmdInputs(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("inputs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "checkout root; the models are <root>/tla")
	if err := fs.Parse(args); err != nil {
		return exitRefused
	}
	if fs.NArg() != 1 {
		return refuse(stderr, "inputs", errors.New("inputs takes one configuration name"))
	}
	if err := tlc.CheckRunner(*root); err != nil {
		return refuse(stderr, "inputs", err)
	}
	src, err := tlc.SourceAt(*root)
	if err != nil {
		return refuse(stderr, "inputs", err)
	}
	inputs, err := src.Inputs(fs.Arg(0))
	if err != nil {
		return refuse(stderr, "inputs", err)
	}
	for _, in := range inputs {
		fmt.Fprintln(stdout, in.Path)
	}
	return exitOK
}

func cmdRun(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "checkout root; the models are <root>/tla")
	dir := fs.String("dir", "", "output directory: logs and RUNS.tsv (required)")
	jar := fs.String("jar", "", "the TLC jar; else $TLC_JAR")
	java := fs.String("java", "", "the java program; else the java on PATH")
	group := fs.String("group", "", "the group to run; else every case the shards select")
	shards := fs.Int("shards", 1, "divide the selection into this many shards")
	shard := fs.Int("shard", 0, "run this zero-based shard of the selection")
	timeout := fs.Duration("timeout", 110*time.Second, "the whole suite's time limit")
	workers := fs.Int("workers", 2, "TLC workers for a case expected to pass")
	parallel := fs.Int("parallel", max(1, runtime.NumCPU()/4), "cases to run at once (default a quarter of the CPUs)")
	manual := fs.Bool("manual", false, "an explicit bench experiment: mode=manual")
	if err := fs.Parse(args); err != nil {
		return exitRefused
	}
	if fs.NArg() != 0 {
		return refuse(stderr, "run", errors.New("run takes no positional arguments"))
	}
	if *dir == "" {
		return refuse(stderr, "run", errors.New("--dir is required: the output directory of the run"))
	}
	// The parallelism is checked before anything else is resolved, so a bad
	// value is refused without naming a bench that is not here.
	if err := tlc.CheckParallel(*parallel, runtime.NumCPU()); err != nil {
		return refuse(stderr, "run", err)
	}
	cases, err := tlc.LoadCases(*root)
	if err != nil {
		return refuse(stderr, "run", err)
	}
	if err := tlc.CheckRunner(*root); err != nil {
		return refuse(stderr, "run", err)
	}
	chosen, err := tlc.Select(cases, *group, *shards, *shard)
	if err != nil {
		return refuse(stderr, "run", err)
	}
	// java is resolved before the jar so a bench without java says so, and
	// nothing is started when either is missing.
	javaPath, err := tlc.FindHelper("java", *java, tlc.LookPath)
	if err != nil {
		return refuse(stderr, "run", err)
	}
	j, err := tlc.FindJar(*jar, os.Getenv)
	if err != nil {
		return refuse(stderr, "run", err)
	}
	if err := tlc.CheckLimits(*timeout, *workers, *manual); err != nil {
		return refuse(stderr, "run", err)
	}
	javaVer, err := tlc.ReadJavaVersion(context.Background(), javaPath)
	if err != nil {
		return refuse(stderr, "run", err)
	}
	platform, err := tlc.Platform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return refuse(stderr, "run", err)
	}
	res, err := tlc.RunSuite(tlc.Options{
		Root: *root, Cases: chosen, Jar: j, Java: javaPath, JavaVer: javaVer,
		Out: *dir, Budget: *timeout, Workers: *workers, Parallel: *parallel, Manual: *manual,
		Platform: platform, CPUs: runtime.NumCPU(),
		Selection: tlc.Selection{Group: *group, Shards: *shards, Shard: *shard},
		OnCase: func(r tlc.Record) {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", r.Config, r.Result, r.Seconds, r.StartedUTC)
		},
	})
	if err != nil {
		return refuse(stderr, "run", err)
	}
	if res.Refused != "" {
		return fail(stderr, "run", errors.New(res.Refused))
	}
	if res.Failed {
		return fail(stderr, "run", errors.New("a case did not meet the result it declares; the records are in "+filepath.Join(*dir, tlc.RunsFile)))
	}
	return exitOK
}

func cmdMerge(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("merge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "checkout root; the models are <root>/tla")
	keep := fs.String("keep", "", "a records file to carry forward; its stale records are dropped")
	out := fs.String("out", "", "the records file to write (required)")
	if err := fs.Parse(args); err != nil {
		return exitRefused
	}
	if *out == "" {
		return refuse(stderr, "merge", errors.New("--out is required: the records file to write"))
	}
	cases, err := tlc.LoadCases(*root)
	if err != nil {
		return refuse(stderr, "merge", err)
	}
	if err := tlc.CheckRunner(*root); err != nil {
		return refuse(stderr, "merge", err)
	}
	src, err := tlc.SourceAt(*root)
	if err != nil {
		return refuse(stderr, "merge", err)
	}
	var runs [][]tlc.Record
	for _, path := range fs.Args() {
		recs, err := tlc.ReadRecordsFile(path)
		if err != nil {
			return refuse(stderr, "merge", err)
		}
		runs = append(runs, recs)
	}
	var kept []tlc.Record
	if *keep != "" {
		base, err := tlc.ReadRecordsFile(*keep)
		if err != nil {
			return refuse(stderr, "merge", err)
		}
		var dropped []tlc.Dropped
		kept, dropped, err = tlc.Carry(src, cases, base, runs...)
		if err != nil {
			return refuse(stderr, "merge", err)
		}
		for _, d := range dropped {
			switch d.Why {
			case tlc.DroppedStale:
				fmt.Fprintf(stdout, "dropped %s (group %s): measured on other inputs\n", d.Config, d.Group)
			case tlc.DroppedGone:
				fmt.Fprintf(stdout, "dropped %s: no longer in the plan\n", d.Config)
			}
		}
	}
	merged, err := tlc.Merge(src, cases, append(runs, kept)...)
	if err != nil {
		return fail(stderr, "merge", err)
	}
	f, err := os.Create(*out)
	if err != nil {
		return refuse(stderr, "merge", err)
	}
	if err := tlc.WriteRecords(f, merged); err != nil {
		f.Close()
		return refuse(stderr, "merge", err)
	}
	if err := f.Close(); err != nil {
		return refuse(stderr, "merge", err)
	}
	fmt.Fprintf(stdout, "wrote %d records to %s\n", len(merged), *out)
	return exitOK
}
