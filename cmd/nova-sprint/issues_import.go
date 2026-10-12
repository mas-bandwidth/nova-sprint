package main

import (
	"io"
	"os"

	"github.com/mas-bandwidth/nova-sprint/internal/issues"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/verbflag"
)

// cmdIssuesImport is nova-sprint issues import.
func (a *app) cmdIssuesImport(args []string, stdout, stderr io.Writer) int {
	if verbflag.Asked(args) {
		panic(verbflag.Help{FS: issues.FlagSet("import")})
	}
	return issues.Main(append([]string{"import"}, args...), os.Stdin, stdout, stderr)
}
