package main

import (
	"regexp"
	"sort"
	"strings"
)

// These are modules bundled with the TLC jar used by nova-sprint. References
// outside this set must resolve to a local tla/<name>.tla file.
var standardTLA = map[string]bool{
	"Bags": true, "FiniteSets": true, "Integers": true, "Naturals": true,
	"Randomization": true, "RealTime": true, "Reals": true,
	"Sequences": true, "TLC": true, "Toolbox": true,
}

var (
	moduleOpenRE = regexp.MustCompile(`^\s*-{4,}\s*MODULE\s+([A-Za-z0-9_]+)`)
	moduleEndRE  = regexp.MustCompile(`^\s*={4,}`)
	extendsRE    = regexp.MustCompile(`\bEXTENDS\b\s*([A-Za-z0-9_]+(?:\s*,\s*[A-Za-z0-9_]+)*)`)
	instanceRE   = regexp.MustCompile(`\bINSTANCE\s+([A-Za-z0-9_]+)`)
)

// moduleReferences finds EXTENDS and INSTANCE references inside module bodies,
// including nested modules. Comments and quoted strings are blanked first so
// example text cannot pull unrelated modules into the seeded tree.
func moduleReferences(src []byte) []string {
	text := strings.Split(string(stripTLAComments(src)), "\n")
	first := -1
	for i, line := range text {
		if moduleOpenRE.MatchString(line) {
			first = i
			break
		}
	}
	if first < 0 {
		return nil
	}
	declared := map[string]bool{}
	var body strings.Builder
	depth := 0
	for _, line := range text[first:] {
		if m := moduleOpenRE.FindStringSubmatch(line); m != nil {
			depth++
			declared[m[1]] = true
			body.WriteString(line)
			body.WriteByte('\n')
			continue
		}
		if moduleEndRE.MatchString(line) {
			depth--
			if depth <= 0 {
				break
			}
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	clean := body.String()
	type hit struct {
		at    int
		names []string
	}
	var hits []hit
	for _, m := range extendsRE.FindAllStringSubmatchIndex(clean, -1) {
		var names []string
		for _, name := range strings.Split(clean[m[2]:m[3]], ",") {
			names = append(names, strings.TrimSpace(name))
		}
		hits = append(hits, hit{m[0], names})
	}
	for _, m := range instanceRE.FindAllStringSubmatchIndex(clean, -1) {
		hits = append(hits, hit{m[0], []string{clean[m[2]:m[3]]}})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].at < hits[j].at })
	seen := map[string]bool{}
	var refs []string
	for _, h := range hits {
		for _, name := range h.names {
			if !declared[name] && !seen[name] {
				seen[name] = true
				refs = append(refs, name)
			}
		}
	}
	return refs
}

func stripTLAComments(src []byte) []byte {
	out := append([]byte(nil), src...)
	blank := func(a, b int) {
		for i := a; i < b; i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	for i := 0; i < len(src); {
		switch {
		case src[i] == '"':
			j := i + 1
			for j < len(src) && src[j] != '"' && src[j] != '\n' {
				if src[j] == '\\' && j+1 < len(src) {
					j++
				}
				j++
			}
			if j < len(src) && src[j] == '"' {
				j++
			}
			blank(i, j)
			i = j
		case src[i] == '\\' && i+1 < len(src) && src[i+1] == '*':
			j := i + 2
			for j < len(src) && src[j] != '\n' {
				j++
			}
			blank(i, j)
			i = j
		case src[i] == '(' && i+1 < len(src) && src[i+1] == '*':
			depth, j := 1, i+2
			for j < len(src) && depth > 0 {
				if src[j] == '(' && j+1 < len(src) && src[j+1] == '*' {
					depth++
					j += 2
				} else if src[j] == '*' && j+1 < len(src) && src[j+1] == ')' {
					depth--
					j += 2
				} else {
					j++
				}
			}
			blank(i, j)
			i = j
		default:
			i++
		}
	}
	return out
}
