package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/roadmap"
)

type roadmapCall struct {
	verb        string
	pos         []string
	stringFlags map[string]string
	boolFlags   map[string]bool
}

func parseRoadmapArgs(verb string, args []string) (*roadmapCall, error) {
	rc := &roadmapCall{
		verb:        verb,
		stringFlags: make(map[string]string),
		boolFlags:   make(map[string]bool),
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rc.pos = append(rc.pos, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") {
			flagName := strings.TrimLeft(arg, "-")
			flagVal := ""
			hasVal := false
			if strings.Contains(flagName, "=") {
				flagName, flagVal, hasVal = strings.Cut(flagName, "=")
			}

			switch flagName {
			case "json":
				if hasVal {
					rc.boolFlags["json"] = flagVal == "true" || flagVal == "1"
				} else {
					rc.boolFlags["json"] = true
				}
			case "stream", "brief-dir", "out", "text":
				if hasVal {
					rc.stringFlags[flagName] = flagVal
				} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					rc.stringFlags[flagName] = args[i+1]
					i++
				} else {
					return nil, fmt.Errorf("flag --%s requires an argument", flagName)
				}
			default:
				return nil, fmt.Errorf("unknown flag --%s", flagName)
			}
		} else {
			rc.pos = append(rc.pos, arg)
		}
	}
	return rc, nil
}

func hasHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			return true
		}
	}
	return false
}

const roadmapGroupHelp = `usage:
  nova-work roadmap add <file> --stream <s> --brief-dir <dir>
  nova-work roadmap remove <file> <id>...
  nova-work roadmap pull <file> <id>... --out <dir>
  nova-work roadmap check <file>
  nova-work roadmap note <file> <id>... --text <t>

the verbs are add, remove, pull, check, note.
`

func runRoadmap(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || hasHelp(args) && len(args) == 1 {
		fmt.Fprint(stdout, roadmapGroupHelp)
		return 0
	}

	sub := args[0]
	subArgs := args[1:]

	if hasHelp(subArgs) {
		switch sub {
		case "add":
			fmt.Fprint(stdout, `usage: nova-work roadmap add <file> --stream <s> --brief-dir <dir>

reads brief files (*.md) in --brief-dir, adds them as cards to stream --stream under the active release, increments release card count.

flags:
  --stream <s>          the stream to add the cards to (required)
  --brief-dir <dir>     directory containing *.md brief files (required)
  --json                print the result as one JSON object instead of lines

effect: local write: writes files on this machine
exit codes: 0 cards added; 2 could not run (missing flags, unreadable file or directory)
`)
			return 0
		case "remove":
			fmt.Fprint(stdout, `usage: nova-work roadmap remove <file> <id>...

removes cards by id, decrements release card count, drops any empty stream.

flags:
  --json                print the result as one JSON object instead of lines

effect: local write: writes files on this machine
exit codes: 0 cards removed; 2 could not run (missing arguments, card not found)
`)
			return 0
		case "pull":
			fmt.Fprint(stdout, `usage: nova-work roadmap pull <file> <id>... --out <dir>

writes <id>.md for each matched card into --out directory, and removes them from the roadmap.

flags:
  --out <dir>           directory to write pulled <id>.md briefs to (required)
  --json                print the result as one JSON object instead of lines

effect: local write: writes files on this machine
exit codes: 0 cards pulled; 2 could not run (missing flags, card not found)
`)
			return 0
		case "check":
			fmt.Fprint(stdout, `usage: nova-work roadmap check <file>

validates string-aware paren balance, verifies release :cards matches actual count of cards across its streams, checks for duplicate card IDs, checks for empty streams.

flags:
  --json                print the result as one JSON object instead of lines

effect: inspection: reads, writes nothing
exit codes: 0 check passed; 1 check found differences or empty streams; 2 could not run (unreadable file, invalid syntax)
`)
			return 0
		case "note":
			fmt.Fprint(stdout, `usage: nova-work roadmap note <file> <id>... --text <t>

appends text to the :brief of the specified cards.

flags:
  --text <t>            text to append to card briefs (required)
  --json                print the result as one JSON object instead of lines

effect: local write: writes files on this machine
exit codes: 0 note appended; 2 could not run (missing flags, card not found)
`)
			return 0
		}
	}

	switch sub {
	case "add":
		return runRoadmapAdd(subArgs, stdout, stderr)
	case "remove":
		return runRoadmapRemove(subArgs, stdout, stderr)
	case "pull":
		return runRoadmapPull(subArgs, stdout, stderr)
	case "check":
		return runRoadmapCheck(subArgs, stdout, stderr)
	case "note":
		return runRoadmapNote(subArgs, stdout, stderr)
	default:
		return emitRefused(stderr, "roadmap", fmt.Sprintf("unknown roadmap verb %q; the verbs are add, remove, pull, check, note", sub), false)
	}
}

func runRoadmapCheck(args []string, stdout, stderr io.Writer) int {
	rc, err := parseRoadmapArgs("check", args)
	asJSON := rc != nil && rc.boolFlags["json"]
	if err != nil {
		return emitRefused(stderr, "roadmap check", err.Error(), asJSON)
	}
	if len(rc.pos) == 0 {
		return emitRefused(stderr, "roadmap check", "roadmap check wants <file>", asJSON)
	}
	file := rc.pos[0]
	if !fileExists(file) {
		return emitRefused(stderr, "roadmap check", fmt.Sprintf("file %q not found", file), asJSON)
	}

	problems, err := roadmap.CheckFile(file)
	if err != nil {
		return emitRefused(stderr, "roadmap check", err.Error(), asJSON)
	}

	if len(problems) > 0 {
		if asJSON {
			out := map[string]any{
				"result": map[string]any{
					"verb":   "roadmap check",
					"status": "failed",
					"exit":   1,
					"why":    problems,
				},
				"facts": map[string]any{
					"file":     file,
					"problems": len(problems),
				},
			}
			data, _ := json.Marshal(out)
			fmt.Fprintln(stdout, string(data))
		} else {
			fmt.Fprintf(stdout, "ROADMAP-CHECK FAILED file=%s problems=%d\n", file, len(problems))
			for _, p := range problems {
				fmt.Fprintf(stderr, "  ROADMAP-CHECK DRIFT file=%s problem=%q\n", file, p)
			}
		}
		return 1
	}

	r, err := roadmap.Load(file)
	totalCards := 0
	relCount := 0
	streamCount := 0
	if err == nil {
		relCount = len(r.Releases)
		for _, rel := range r.Releases {
			totalCards += rel.CardsCount
			streamCount += len(rel.Streams)
		}
	}

	if asJSON {
		out := map[string]any{
			"result": map[string]any{
				"verb":   "roadmap check",
				"status": "ok",
				"exit":   0,
			},
			"facts": map[string]any{
				"file":     file,
				"cards":    totalCards,
				"releases": relCount,
				"streams":  streamCount,
			},
		}
		data, _ := json.Marshal(out)
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprintf(stdout, "ROADMAP-CHECK OK file=%s cards=%d releases=%d streams=%d\n",
			file, totalCards, relCount, streamCount)
	}
	return 0
}

func runRoadmapRemove(args []string, stdout, stderr io.Writer) int {
	rc, err := parseRoadmapArgs("remove", args)
	asJSON := rc != nil && rc.boolFlags["json"]
	if err != nil {
		return emitRefused(stderr, "roadmap remove", err.Error(), asJSON)
	}
	if len(rc.pos) == 0 {
		return emitRefused(stderr, "roadmap remove", "roadmap remove wants <file> <id>...", asJSON)
	}
	if len(rc.pos) == 1 {
		return emitRefused(stderr, "roadmap remove", "at least one card id is required", asJSON)
	}

	file := rc.pos[0]
	ids := rc.pos[1:]
	if !fileExists(file) {
		return emitRefused(stderr, "roadmap remove", fmt.Sprintf("file %q not found", file), asJSON)
	}

	r, err := roadmap.Load(file)
	if err != nil {
		return emitRefused(stderr, "roadmap remove", err.Error(), asJSON)
	}

	removed, err := r.Remove(ids...)
	if err != nil {
		return emitRefused(stderr, "roadmap remove", err.Error(), asJSON)
	}

	if err := r.Save(file); err != nil {
		return emitRefused(stderr, "roadmap remove", fmt.Sprintf("save %s: %v", file, err), asJSON)
	}

	remaining := 0
	for _, rel := range r.Releases {
		remaining += rel.CardsCount
	}

	if asJSON {
		out := map[string]any{
			"result": map[string]any{
				"verb":   "roadmap remove",
				"status": "ok",
				"exit":   0,
			},
			"facts": map[string]any{
				"file":      file,
				"removed":   removed,
				"remaining": remaining,
			},
		}
		data, _ := json.Marshal(out)
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprintf(stdout, "ROADMAP-REMOVE OK file=%s removed=%d remaining=%d\n", file, removed, remaining)
	}
	return 0
}

func runRoadmapPull(args []string, stdout, stderr io.Writer) int {
	rc, err := parseRoadmapArgs("pull", args)
	asJSON := rc != nil && rc.boolFlags["json"]
	if err != nil {
		return emitRefused(stderr, "roadmap pull", err.Error(), asJSON)
	}
	outDir := rc.stringFlags["out"]
	if outDir == "" {
		return emitRefused(stderr, "roadmap pull", "--out is required; it wants a directory", asJSON)
	}
	if len(rc.pos) == 0 {
		return emitRefused(stderr, "roadmap pull", "roadmap pull wants <file> <id>...", asJSON)
	}
	if len(rc.pos) == 1 {
		return emitRefused(stderr, "roadmap pull", "at least one card id is required", asJSON)
	}

	file := rc.pos[0]
	ids := rc.pos[1:]
	if !fileExists(file) {
		return emitRefused(stderr, "roadmap pull", fmt.Sprintf("file %q not found", file), asJSON)
	}

	r, err := roadmap.Load(file)
	if err != nil {
		return emitRefused(stderr, "roadmap pull", err.Error(), asJSON)
	}

	pulled, err := r.Pull(outDir, ids...)
	if err != nil {
		return emitRefused(stderr, "roadmap pull", err.Error(), asJSON)
	}

	if err := r.Save(file); err != nil {
		return emitRefused(stderr, "roadmap pull", fmt.Sprintf("save %s: %v", file, err), asJSON)
	}

	remaining := 0
	for _, rel := range r.Releases {
		remaining += rel.CardsCount
	}

	if asJSON {
		out := map[string]any{
			"result": map[string]any{
				"verb":   "roadmap pull",
				"status": "ok",
				"exit":   0,
			},
			"facts": map[string]any{
				"file":      file,
				"pulled":    len(pulled),
				"out":       outDir,
				"remaining": remaining,
			},
		}
		data, _ := json.Marshal(out)
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprintf(stdout, "ROADMAP-PULL OK file=%s pulled=%d out=%s remaining=%d\n",
			file, len(pulled), outDir, remaining)
	}
	return 0
}

func runRoadmapAdd(args []string, stdout, stderr io.Writer) int {
	rc, err := parseRoadmapArgs("add", args)
	asJSON := rc != nil && rc.boolFlags["json"]
	if err != nil {
		return emitRefused(stderr, "roadmap add", err.Error(), asJSON)
	}
	stream := rc.stringFlags["stream"]
	briefDir := rc.stringFlags["brief-dir"]
	if stream == "" {
		return emitRefused(stderr, "roadmap add", "--stream is required; it wants the stream name", asJSON)
	}
	if briefDir == "" {
		return emitRefused(stderr, "roadmap add", "--brief-dir is required; it wants a directory of *.md brief files", asJSON)
	}
	if len(rc.pos) == 0 {
		return emitRefused(stderr, "roadmap add", "roadmap add wants <file>", asJSON)
	}

	file := rc.pos[0]
	if !fileExists(file) {
		return emitRefused(stderr, "roadmap add", fmt.Sprintf("file %q not found", file), asJSON)
	}
	fi, err := os.Stat(briefDir)
	if err != nil || !fi.IsDir() {
		return emitRefused(stderr, "roadmap add", fmt.Sprintf("brief directory %q not found", briefDir), asJSON)
	}

	r, err := roadmap.Load(file)
	if err != nil {
		return emitRefused(stderr, "roadmap add", err.Error(), asJSON)
	}

	added, err := r.Add(stream, briefDir)
	if err != nil {
		return emitRefused(stderr, "roadmap add", err.Error(), asJSON)
	}

	if err := r.Save(file); err != nil {
		return emitRefused(stderr, "roadmap add", fmt.Sprintf("save %s: %v", file, err), asJSON)
	}

	totalCards := 0
	for _, rel := range r.Releases {
		totalCards += rel.CardsCount
	}

	if asJSON {
		out := map[string]any{
			"result": map[string]any{
				"verb":   "roadmap add",
				"status": "ok",
				"exit":   0,
			},
			"facts": map[string]any{
				"file":   file,
				"stream": stream,
				"added":  added,
				"total":  totalCards,
			},
		}
		data, _ := json.Marshal(out)
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprintf(stdout, "ROADMAP-ADD OK file=%s stream=%s added=%d total=%d\n",
			file, stream, added, totalCards)
	}
	return 0
}

func runRoadmapNote(args []string, stdout, stderr io.Writer) int {
	rc, err := parseRoadmapArgs("note", args)
	asJSON := rc != nil && rc.boolFlags["json"]
	if err != nil {
		return emitRefused(stderr, "roadmap note", err.Error(), asJSON)
	}
	text := rc.stringFlags["text"]
	if text == "" {
		return emitRefused(stderr, "roadmap note", "--text is required; it wants text to append to card briefs", asJSON)
	}
	if len(rc.pos) == 0 {
		return emitRefused(stderr, "roadmap note", "roadmap note wants <file> <id>...", asJSON)
	}
	if len(rc.pos) == 1 {
		return emitRefused(stderr, "roadmap note", "at least one card id is required", asJSON)
	}

	file := rc.pos[0]
	ids := rc.pos[1:]
	if !fileExists(file) {
		return emitRefused(stderr, "roadmap note", fmt.Sprintf("file %q not found", file), asJSON)
	}

	r, err := roadmap.Load(file)
	if err != nil {
		return emitRefused(stderr, "roadmap note", err.Error(), asJSON)
	}

	noted, err := r.Note(text, ids...)
	if err != nil {
		return emitRefused(stderr, "roadmap note", err.Error(), asJSON)
	}

	if err := r.Save(file); err != nil {
		return emitRefused(stderr, "roadmap note", fmt.Sprintf("save %s: %v", file, err), asJSON)
	}

	if asJSON {
		out := map[string]any{
			"result": map[string]any{
				"verb":   "roadmap note",
				"status": "ok",
				"exit":   0,
			},
			"facts": map[string]any{
				"file":  file,
				"noted": noted,
			},
		}
		data, _ := json.Marshal(out)
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprintf(stdout, "ROADMAP-NOTE OK file=%s noted=%d\n", file, noted)
	}
	return 0
}

func emitRefused(stderr io.Writer, verb, reason string, asJSON bool) int {
	remedy := "nova-work " + verb + " -h"
	if asJSON {
		out := map[string]any{
			"result": map[string]any{
				"verb":   verb,
				"status": "refused",
				"exit":   2,
				"why":    []string{reason},
				"remedy": remedy,
			},
		}
		data, _ := json.Marshal(out)
		fmt.Fprintln(stderr, string(data))
	} else {
		token := strings.ToUpper(strings.ReplaceAll(verb, " ", "-"))
		fmt.Fprintf(stderr, "%s REFUSED: %s; run: %s\n", token, reason, remedy)
	}
	return 2
}
