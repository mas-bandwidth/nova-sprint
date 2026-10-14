package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// settings is a read: any actor may run it (coordinator.go's verb classes).
func init() {
	verbClasses["settings"] = classRead
	verbEffect["settings"] = "inspection: reads the sprint's settings and prints each with its value and default, writes nothing"
	verbExit["settings"] = "exit codes: 0 done, 1 failed, 2 usage or a store that did not answer"
}

// cmdSettings prints every setting with its value and its default
// (docs/SPEC-SPRINT.md section 11, Verbs, the settings row).
func (a *app) cmdSettings(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("settings")
	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, "settings", argErr("takes no words ", err, pos...))
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "settings", err.Error())
	}
	s, err := st.Load(context.Background(), []string{sprint.Work}, nil)
	if err != nil {
		return a.readFailed("settings", err, stderr)
	}
	printSettings(stdout, "settings", s, c.json)
	return 0
}

// printSettings is the settings lines, shared by `settings` and `set` with
// no flags: one SETTING line a setting, then the SETTINGS OK line. --json
// prints one JSON object, the common flag's promise ("print one JSON object
// for a program instead of the lines"): the OK line is included as top-level
// fields, the settings array holds every setting value, and verb names the
// verb that printed, which is the caller's own, not always "settings"
// (sprint-settings-json-is-one-object; docs/SPEC-SPRINT.md section 11,
// Verbs, the settings row).
func printSettings(stdout io.Writer, verb string, s *sprint.Snapshot, asJSON bool) {
	vals := sprint.SettingsOf(s)
	set := 0
	for _, v := range vals {
		if v.Set {
			set++
		}
	}
	if asJSON {
		b, _ := json.Marshal(map[string]any{
			"ok":       true,
			"verb":     verb,
			"settings": vals,
			"count":    len(vals),
			"set":      set,
			"epoch":    s.Epoch,
		})
		fmt.Fprintln(stdout, string(b))
		return
	}
	for _, v := range vals {
		yn := "no"
		if v.Set {
			yn = "yes"
		}
		fmt.Fprintf(stdout, "SETTING %s value=%s default=%s set=%s\n", v.Flag, orDash(v.Value), orDash(v.Default), yn)
	}
	fmt.Fprintf(stdout, "SETTINGS OK count=%d set=%d epoch=%d\n", len(vals), set, s.Epoch)
}
