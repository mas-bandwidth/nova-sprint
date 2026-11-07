package edge

import (
	"slices"
	"strings"
	"testing"
)

// TestParseGate: the one gate parser. Words split at white space, quotes join
// and hide everything, shell syntax outside a quote is refused, and the first
// word must be a program.
func TestParseGate(t *testing.T) {
	good := map[string][]string{
		`go test ./...`:                         {"go", "test", "./..."},
		`sh -c 'a | b && c > d; $(e) ` + "`f`'": {"sh", "-c", "a | b && c > d; $(e) `f`"},
		`sh -c "make && echo 'ok (fine)'"`:      {"sh", "-c", "make && echo 'ok (fine)'"},
		`go test -run "A B"x`:                   {"go", "test", "-run", "A Bx"},
		`./gate.sh a`:                           {"./gate.sh", "a"},
		`/usr/bin/env FOO=1 go vet`:             {"/usr/bin/env", "FOO=1", "go", "vet"},
		`env FOO=1 go test`:                     {"env", "FOO=1", "go", "test"},
		`go test -run ^TestX$`:                  {"go", "test", "-run", "^TestX$"},
		`golangci-lint run`:                     {"golangci-lint", "run"},
		`cmd.exe /c x`:                          {"cmd.exe", "/c", "x"},
		`clang++ -c a.cc`:                       {"clang++", "-c", "a.cc"},
		`echo ""`:                               {"echo", ""},
	}
	for line, want := range good {
		got, why := ParseGate(line)
		if why != "" || !slices.Equal(got, want) {
			t.Errorf("ParseGate(%q) = %q, %q; want %q", line, got, why, want)
		}
	}
	bad := map[string]string{
		``:                           "the command is empty",
		`   `:                        "the command is empty",
		`echo 'x`:                    "a ' is left open",
		`echo "x`:                    `a " is left open`,
		"cat `f`":                    "a backquote is shell syntax",
		`go test && go vet`:          "&& is shell syntax",
		`go test || true`:            "|| is shell syntax",
		`go test ; go vet`:           "; is shell syntax",
		`go test | tee x`:            "| is shell syntax",
		`go test &`:                  "& is shell syntax",
		`go test > out.txt`:          "> is shell syntax",
		`go test >> out.txt`:         ">> is shell syntax",
		`wc -l < in.txt`:             "< is shell syntax",
		`echo $(date)`:               "$( is shell syntax",
		`echo ${HOME}`:               "${ is shell syntax",
		`echo $HOME`:                 "$H is shell syntax",
		`(cd x)`:                     "( is shell syntax",
		`echo a\ b`:                  "a backslash is shell syntax",
		`go test x)`:                 ") is shell syntax",
		`cd pkg`:                     "cd is a shell builtin",
		`export A=1`:                 "export is a shell builtin",
		`source env.sh`:              "source is a shell builtin",
		`A=1 go test`:                "A=1 is an assignment",
		`~/bin/x`:                    "starts with ~",
		`Verify that the tests pass`: "the first word Verify is not a program name",
		`tests, pass`:                "the first word tests, is not a program name",
		`done.`:                      "the first word done. is not a program name",
		`-v go`:                      "the first word -v is not a program name",
		`it works for me ; yes`:      "; is shell syntax",
	}
	for line, want := range bad {
		got, why := ParseGate(line)
		if got != nil || !strings.Contains(why, want) {
			t.Errorf("ParseGate(%q) = %q, %q; want a reason holding %q", line, got, why, want)
		}
	}
}

// TestParseGateFixNamesShC: the fix says to wrap the line in sh -c.
func TestParseGateFixNamesShC(t *testing.T) {
	if !strings.Contains(GateFix, "sh -c '...'") {
		t.Fatalf("GateFix %q", GateFix)
	}
}
