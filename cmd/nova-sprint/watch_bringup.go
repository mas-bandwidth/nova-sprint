package main

import (
	"io"
)

// The bring-up and watch wrappers.
// This file's init runs after verbs.go's (which builds verbs), wrapping start,
// check and watch with their bring-up behaviors.
func init() {
	for i := range verbs {
		switch verbs[i].name {
		case "start":
			orig := verbs[i].run
			verbs[i].run = func(a *app, args []string, stdout, stderr io.Writer) int {
				return a.cmdStartBringUp(orig, args, stdout, stderr)
			}
		case "check":
			orig := verbs[i].run
			verbs[i].syntax = "[--bring-up]"
			verbs[i].run = func(a *app, args []string, stdout, stderr io.Writer) int {
				return a.cmdCheckBringUp(orig, args, stdout, stderr)
			}
		case "watch":
			orig := verbs[i].run
			verbs[i].syntax = "(--wake [--every <duration>] [--state <file>] [--check <duration>] [--judgment-every <duration>] [--merge-every <duration>] [--backlog-every <duration>] [--land-after <duration>] [--merge-over <n>] [--merging-over <n>] [--review-over <n>] | --events)"
			verbs[i].run = func(a *app, args []string, stdout, stderr io.Writer) int {
				return a.cmdWatchBringUp(orig, args, stdout, stderr)
			}
		}
	}
}
