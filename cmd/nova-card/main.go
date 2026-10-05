// Command nova-card is retired but for new: generate, template and lint are verbs
// of the one nova-sprint binary (internal/card, planning in internal/cardgen). This
// stub refuses each of them with the verb it moved to, runs new (internal/card),
// and keeps `version`.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/mas-bandwidth/nova-sprint/internal/card"
	"github.com/mas-bandwidth/nova-sprint/internal/nsprint/verbflag"
)

const usage = `nova-card is retired: its verbs moved to nova-sprint.
  nova-card generate  ->  nova-sprint card generate
  nova-card template  ->  nova-sprint card template
  nova-card lint      ->  nova-sprint card lint
nova-card version still prints this build.
nova-card new writes one brief from its parts, held to the lint nova-sprint add runs:
  nova-card new <id> --repo <owner/name> --base <branch> --task-file <file> --paths <paths> [--shared <paths>] --test <test> --gate <pkgs> --tier <tier> [--needs <ids>] [--out <file>]
  nova-card new --batch <tsv> --out <dir> [--repo <owner/name>] [--base <branch>] [--tier <tier>] [--gate <pkgs>]
example:
  nova-card new fix-x --repo owner/repo --base main --task-file task.txt --paths 'cmd/x/**' --test './cmd/x TestX' --gate './cmd/x/' --tier pro --out cards/fix-x.md
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run is the whole stub: a moved verb refuses on stderr with exit 2, in the shape
// `nova-card <verb> moved to nova-sprint card <verb>; run: nova-sprint card <verb> -h`.
func run(args []string, stdout, stderr io.Writer) (code int) {
	defer verbflag.RecoverWith(stdout, "nova-card", usage, &code, func(verb string) string {
		return "effect: local write: one brief to stdout or --out, or with --batch a directory of briefs; nothing when a part is missing or the brief is red\n"
	})
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch verb := args[0]; verb {
	case "generate", "template", "lint":
		fmt.Fprintf(stderr, "nova-card %s moved to nova-sprint card %s; run: nova-sprint card %s -h\n", verb, verb, verb)
		return 2
	case "new":
		return card.New(args[1:], stdout, stderr)
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
