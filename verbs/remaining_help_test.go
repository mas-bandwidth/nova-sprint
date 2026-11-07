package verbs

import (
	"context"
	"strings"
	"testing"
)

func TestRemainingHelp(t *testing.T) {
	e := Engine{}
	for _, args := range [][]string{nil, {"unknown"}, {"edit"}} {
		r := e.help(context.Background(), args)
		if r.Exit != 0 || !strings.HasSuffix(r.Output, "next: nova-sprint\n") {
			t.Fatal(r)
		}
	}
	if got := e.help(context.Background(), []string{"edit"}); !strings.Contains(got.Output, "--brief-file") {
		t.Fatal(got)
	}
}

func TestRemainingHelpRulings(t *testing.T) {
	e := Engine{}
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"-h"}, "getting-started:"}, {[]string{"check"}, "--history | --integrity"}, {[]string{"policy", "set"}, "<file> --reason <why>"}, {[]string{"policy", "activate"}, "<version> --reason <why>"}, {[]string{"policy", "show"}, "--list"}, {[]string{"policy", "diff"}, "<version|default>"}, {[]string{"policy", "check"}, "<file|version>"}, {[]string{"work"}, "worker"}, {[]string{"row", "add"}, "--kind friend|worker"}, {[]string{"stream", "set"}, "--mode auto|group|hand"}, {[]string{"drop"}, "[--waive | --relink <other>] [--expect <dependant-ids>]"}, {[]string{"release"}, "--row <row> --all"},
	}
	for _, tc := range cases {
		r := e.help(context.Background(), tc.args)
		if r.Exit != 0 || !strings.Contains(r.Output, tc.want) || !strings.HasSuffix(r.Output, "next: nova-sprint\n") {
			t.Fatal(tc.args, r)
		}
	}
}
