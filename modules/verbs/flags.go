package verbs

import (
	"fmt"
	"strconv"
	"strings"
)

// input is one verb's parsed arguments: the one flag parser's output.
// Positional arguments keep their order; a flag given twice is bad usage
// unless its spec repeats it.
type input struct {
	verb  string
	args  []string
	vals  map[string][]string
	bools map[string]bool
	actor string // --as, else NOVA_SPRINT_AS
}

// flagSpec is a verb's flags in one string: `name` is a switch, `name=`
// takes a value, `name=*` takes a value and repeats. `as=` and `epoch=` are
// every verb's (--epoch left out is the active epoch; given, a verb that
// reads the store refuses any other, L6.23).
type flagSpec map[string]byte

func parseSpec(s string) flagSpec {
	spec := flagSpec{}
	for _, f := range strings.Fields(s) {
		switch {
		case strings.HasSuffix(f, "=*"):
			spec[strings.TrimSuffix(f, "=*")] = '*'
		case strings.HasSuffix(f, "="):
			spec[strings.TrimSuffix(f, "=")] = '='
		default:
			spec[f] = 'b'
		}
	}
	return spec
}

// parse reads args against v's spec and positional bounds. `--x=v` and
// `--x v` are one form; an argument after `--` is positional.
func parse(v *verb, args []string, actor string) (*input, *Fail) {
	in := &input{verb: v.name, vals: map[string][]string{}, bools: map[string]bool{}, actor: actor}
	spec := parseSpec(v.flags + " as= epoch=")
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			in.args = append(in.args, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "--") {
			in.args = append(in.args, a)
			continue
		}
		name, val, hasVal := strings.Cut(a[2:], "=")
		kind, ok := spec[name]
		if !ok {
			return nil, misuse(v.name, "unknown flag --"+name)
		}
		if kind == 'b' {
			if hasVal || in.bools[name] {
				return nil, misuse(v.name, "--"+name+" takes no value and is given once")
			}
			in.bools[name] = true
			continue
		}
		if !hasVal {
			if i+1 >= len(args) {
				return nil, misuse(v.name, "--"+name+" needs a value")
			}
			i++
			val = args[i]
		}
		if val == "" || (kind == '=' && len(in.vals[name]) > 0) {
			return nil, misuse(v.name, "--"+name+" needs one non-empty value")
		}
		in.vals[name] = append(in.vals[name], val)
	}
	return in, in.finish(v)
}

func (in *input) finish(v *verb) *Fail {
	if as := in.str("as"); as != "" {
		in.actor = as
	}
	if _, f := in.num("epoch"); f != nil {
		return f
	}
	if len(in.args) < v.min || (v.max >= 0 && len(in.args) > v.max) {
		return misuse(v.name, fmt.Sprintf("%s takes %s", v.name, v.usage))
	}
	for _, a := range in.args {
		if a == "" || strings.HasPrefix(a, "-") {
			return misuse(v.name, "unknown argument "+a)
		}
	}
	return nil
}

// epoch is --epoch, 0 when left out (the active one); parse checked it.
func (in *input) epoch() int {
	n, _ := in.num("epoch")
	return n
}

func (in *input) str(name string) string {
	if v := in.vals[name]; len(v) > 0 {
		return v[0]
	}
	return ""
}

func (in *input) list(name string) []string { return in.vals[name] }

func (in *input) has(name string) bool { return in.bools[name] || len(in.vals[name]) > 0 }

// arg is positional argument i, or "".
func (in *input) arg(i int) string {
	if i < len(in.args) {
		return in.args[i]
	}
	return ""
}

// num is flag name as a positive integer; absent is 0.
func (in *input) num(name string) (int, *Fail) {
	s := in.str(name)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, misuse(in.verb, "--"+name+" takes a positive whole number ("+badNumber(s, err)+")")
	}
	return n, nil
}

// cardRef is `<card>@<gen>`.
func cardRef(verb, ref string) (string, int, *Fail) {
	i := strings.LastIndexByte(ref, '@')
	if i < 1 {
		return "", 0, misuse(verb, "name the card as <card>@<gen>, as the last output printed it")
	}
	gen, err := strconv.Atoi(ref[i+1:])
	if err != nil || gen < 0 {
		return "", 0, misuse(verb, "name the card as <card>@<gen>, as the last output printed it ("+badNumber(ref[i+1:], err)+")")
	}
	return ref[:i], gen, nil
}

// names is a comma list, each name non-empty.
func names(verb, flag, s string) ([]string, *Fail) {
	if s == "" {
		return nil, nil
	}
	out := strings.Split(s, ",")
	for i := range out {
		if out[i] = strings.TrimSpace(out[i]); out[i] == "" {
			return nil, misuse(verb, "--"+flag+" takes a comma list of names")
		}
	}
	return out, nil
}
