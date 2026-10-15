// Command sprintreplay reads an epoch's recorded log and attempts table, then
// prints the deterministic replay digest and the train/held-out signal report.
// It opens no sprint store and takes no clock or network input.
//
// Run from the repository root with:
//
//	go run ./tools/sprintreplay --data internal/sprintreplay/testdata
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mas-bandwidth/nova-sprint/internal/sprintreplay"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("sprintreplay", flag.ContinueOnError)
	flags.SetOutput(stderr)
	data := flags.String("data", "", "directory containing epoch21.jsonl.gz and attempts.tsv")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: sprintreplay --data <evidence-directory>")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *data == "" || flags.NArg() != 0 {
		flags.Usage()
		return 2
	}

	in, err := sprintreplay.LoadDir(*data)
	if err != nil {
		fmt.Fprintf(stderr, "sprintreplay: reading %s: %v\n", *data, err)
		return 1
	}
	result := sprintreplay.Replay(in)
	report := sprintreplay.Search(result)
	if report.TrainN == 0 || report.TestN == 0 {
		fmt.Fprintf(stderr, "sprintreplay: %s has too few scored attempts for a train and held-out split\n", *data)
		return 1
	}
	flagged := 0
	class, why := "", ""
	for _, attempt := range result.Attempts {
		if fires, predictedClass, reason := sprintreplay.Predict(attempt.View()); fires {
			flagged++
			class, why = predictedClass, reason
		}
	}
	fmt.Fprintf(stdout, "replay digest: %s\nlog lines: %d\nclassified failures: %d (other: %d)\n",
		result.Digest(), result.Lines, result.Labelled(), result.Other())
	fmt.Fprintf(stdout, "predictor at deal: %d/%d flagged", flagged, len(result.Attempts))
	if flagged > 0 {
		fmt.Fprintf(stdout, " as %s; %s", class, why)
	}
	fmt.Fprintln(stdout)
	fmt.Fprint(stdout, report.String())
	return 0
}
