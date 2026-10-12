package main

import (
	"io"
	"os"

	"github.com/mas-bandwidth/nova-sprint/internal/issues"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/verbflag"
)

// cmdIssuesVerify is nova-sprint issues verify.
func (a *app) cmdIssuesVerify(args []string, stdout, stderr io.Writer) int {
	if verbflag.Asked(args) {
		panic(verbflag.Help{FS: issues.FlagSet("verify")})
	}
	return issues.Main(append([]string{"verify"}, args...), os.Stdin, stdout, stderr)
}
