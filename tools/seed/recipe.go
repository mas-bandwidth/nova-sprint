package main

import (
	"fmt"
	"os"
	"strings"
)

// recipe is RECIPE parsed: see that file for what each rule means.
type recipe struct {
	fromMod, toMod string
	move           []string
	rename         map[string]string
	literal        [][2]string
	literalFile    []fileLiteral
	doc            []string
	section        []section
	ratings        []string
	model          []string
	keep           []string
	gomod          bool
}

type section struct {
	file  string
	names []string
}

type fileLiteral struct{ path, from, to string }

func readRecipe(path string) (*recipe, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseRecipe(string(raw))
}

func parseRecipe(text string) (*recipe, error) {
	r := &recipe{rename: map[string]string{}}
	for i, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		bad := func() error { return fmt.Errorf("recipe line %d: %q", i+1, line) }
		switch f[0] {
		case "module":
			if len(f) != 3 {
				return nil, bad()
			}
			r.fromMod, r.toMod = f[1], f[2]
		case "move", "doc", "model", "keep":
			if len(f) != 2 || f[1] == "" {
				return nil, bad()
			}
			switch f[0] {
			case "move":
				r.move = append(r.move, f[1])
			case "doc":
				r.doc = append(r.doc, f[1])
			case "model":
				r.model = append(r.model, f[1])
			case "keep":
				r.keep = append(r.keep, f[1])
			}
		case "rename":
			if len(f) != 3 {
				return nil, bad()
			}
			r.rename[f[1]] = f[2]
		case "literal":
			if len(f) != 3 {
				return nil, bad()
			}
			r.literal = append(r.literal, [2]string{f[1], f[2]})
		case "literal-file":
			if len(f) != 4 || f[1] == "" || f[2] == "" {
				return nil, bad()
			}
			r.literalFile = append(r.literalFile, fileLiteral{path: f[1], from: f[2], to: f[3]})
		case "section":
			if len(f) < 3 {
				return nil, bad()
			}
			r.section = append(r.section, section{file: f[1], names: f[2:]})
		case "ratings":
			if len(f) < 2 {
				return nil, bad()
			}
			r.ratings = append(r.ratings, f[1:]...)
		case "gomod":
			if len(f) != 1 {
				return nil, bad()
			}
			r.gomod = true
		default:
			return nil, bad()
		}
	}
	if r.fromMod == "" || r.toMod == "" {
		return nil, fmt.Errorf("recipe: no module line")
	}
	return r, nil
}
