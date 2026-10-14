package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no Makefile in any parent of working directory")
		}
		dir = parent
	}
}

func Generate(root string) ([]byte, error) {
	var b strings.Builder
	b.WriteString("# AGENTS.md — generated map\n\n")
	b.WriteString("Do not edit. `make map` regenerates this file.\n\n")
	b.WriteString("## Build\n\n```\nmake build   # go build ./...\nmake vet     # go vet ./...\n```\n\n")
	b.WriteString("## Test\n\n```\nmake test    # go vet ./... then go test -p $(GOTEST_P) ./... (the unit tier)\n```\n\n")
	b.WriteString("## Command map\n\n| cmd | guard | command |\n| --- | --- | --- |\n")
	cmds, err := cmdDirs(root)
	if err != nil {
		return nil, err
	}
	for _, c := range cmds {
		fmt.Fprintf(&b, "| %s | `go test ./%s` | `make build` |\n", c, c)
	}
	b.WriteString("\n## CLI rules\n\n")
	b.WriteString("- Each tool under `cmd/` builds one binary; run it with `-h` or `help` to list its verbs.\n")
	b.WriteString("- `nova-sprint` runs a sprint of work cards; `nova-card` reads and reviews cards; `nova-work` files work against a stream.\n")
	b.WriteString("- Comprehensive verb reference: [docs/CLI.md](docs/CLI.md).\n")
	return []byte(b.String()), nil
}

func cmdDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, filepath.ToSlash(filepath.Join("cmd", e.Name())))
		}
	}
	sort.Strings(names)
	return names, nil
}

func main() {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentsmap:", err)
		os.Exit(1)
	}
	data, err := Generate(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentsmap:", err)
		os.Exit(1)
	}
	out := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(out, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "agentsmap:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote AGENTS.md\n")
}
