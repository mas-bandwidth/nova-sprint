// Command nova-card is retired: generate, template and lint are verbs of the one
// nova-sprint binary (internal/card, planning in internal/cardgen). This stub
// refuses each of them with the verb it moved to, and keeps `version`.
package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `nova-card is retired: its verbs moved to nova-sprint.
  nova-card generate  ->  nova-sprint card generate
  nova-card template  ->  nova-sprint card template
  nova-card lint      ->  nova-sprint card lint
nova-card version still prints this build.
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run is the whole stub: a moved verb refuses on stderr with exit 2, in the shape
// `nova-card <verb> moved to nova-sprint card <verb>; run: nova-sprint card <verb> -h`.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch verb := args[0]; verb {
	case "generate", "template", "lint":
		fmt.Fprintf(stderr, "nova-card %s moved to nova-sprint card %s; run: nova-sprint card %s -h\n", verb, verb, verb)
		return 2
	case "version", "--version":
		return cmdVersion(args[1:], stdout, stderr)
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "nova-card REFUSED: unknown verb %q; nova-card is retired, run: nova-sprint help\n", verb)
		return 2
	}
}
