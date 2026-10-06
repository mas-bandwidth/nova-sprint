package main

import (
	"context"
	"io"
)

func (a *app) cmdStartBringUp(orig func(*app, []string, io.Writer, io.Writer) int, args []string, stdout, stderr io.Writer) int {
	code := orig(a, args, stdout, stderr)
	if code != 0 {
		return code
	}
	for _, arg := range args {
		if arg == "--json" || arg == "-json" {
			return code
		}
	}
	fs, c := a.verbSetup("start")
	if _, err := parse(fs, args); err == nil && !c.json {
		if st, err := a.store(*c); err == nil {
			a.printBringUp(context.Background(), st, stdout, stderr)
		}
	}
	return code
}
