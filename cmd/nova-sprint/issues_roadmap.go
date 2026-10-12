package main

import (
	"io"
	"os"

	"github.com/mas-bandwidth/nova-sprint/internal/issues"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/verbflag"
)

// cmdIssuesRoadmap is nova-sprint issues roadmap.
func (a *app) cmdIssuesRoadmap(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && verbflag.IsHelp(args[0]) {
		panic(verbflag.Help{FS: verbflag.New("issues roadmap")})
	}
	return issues.Main(append([]string{"roadmap"}, args...), os.Stdin, stdout, stderr)
}
