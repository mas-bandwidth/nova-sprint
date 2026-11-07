package main

import (
	"bytes"
	"strings"
	"testing"
)

// lint is the rules found in src (a file body after "package p").
func lint(t *testing.T, src string) []string {
	t.Helper()
	found, err := LintSource("x.go", []byte("package p\n\n"+src))
	if err != nil {
		t.Fatal(err)
	}
	var rules []string
	for _, f := range found {
		rules = append(rules, f.Rule)
	}
	return rules
}

func TestEachRuleFindsItsSilentForm(t *testing.T) {
	cases := []struct {
		name, src string
		want      string
	}{
		{"S1 fixed Errorf", `func f() error { if err := g(); err != nil { return fmt.Errorf("schema creation failed") }; return nil }`, "S1"},
		{"S1 errors.New", `func f() error { err := g(); if err != nil { return errors.New("role setup failed") }; return nil }`, "S1"},
		{"S1 in an || branch", `func f() error { n, err := g(); if err != nil || n == 0 { return fmt.Errorf("bad n") }; return nil }`, "S1"},
		{"S1 refusal literal", `func f() error { if err := g(); err != nil { return &OperationError{Problem: "x", Fix: "y"} }; return nil }`, "S1"},
		{"S1 refusal constructor", `func f() error { if err := g(); err != nil { return serviceConfigError("bad") }; return nil }`, "S1"},
		{"S2 re-wrap", `func f() error { if err := g(); err != nil { return &OperationError{Problem: "x: " + err.Error(), Fix: "y"} }; return nil }`, "S2"},
		{"S2 broken", `func f() error { if err := g(); err != nil { return broken("x ("+err.Error()+")", "fix") }; return nil }`, "S2"},
		{"S3 discard", `func f() { _ = g() }`, "S3"},
		{"S3 pair discard", `func f() { _, _ = fmt.Fprint(w, "x") }`, "S3"},
		{"S4 bare return", `func f() int { if err := g(); err != nil { return 1 }; return 0 }`, "S4"},
		{"S4 bare return pair", `func f() (int, bool) { if err := g(); err != nil { return 2, true }; return 0, false }`, "S4"},
		{"S4 os.Exit", `func f() { if err := g(); err != nil { os.Exit(1) } }`, "S4"},
		{"S4 message without err", `func f() int { if err := g(); err != nil { fmt.Fprintln(os.Stderr, "setup configuration unavailable"); return 1 }; return 0 }`, "S4"},
	}
	for _, c := range cases {
		got := lint(t, c.src)
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("%s: %v, want [%s]", c.name, got, c.want)
		}
	}
}

func TestCarriedErrorsAndAllowedFormsAreClean(t *testing.T) {
	for _, src := range []string{
		`func f() error { if err := g(); err != nil { return fmt.Errorf("schema creation failed: %w", err) }; return nil }`,
		`func f() error { if err := g(); err != nil { return fmt.Errorf("x (%v)", err) }; return nil }`,
		`func f() error { if err := g(); err != nil { return cause("x", err) }; return nil }`,
		`func f() error { if err := g(); err != nil { why := err.Error(); return fmt.Errorf("x: %s", why) }; return nil }`,
		`func f() error { if err := g(); err != nil { return RefusalOr(err, &OperationError{Problem: "x: " + err.Error()}) }; return nil }`,
		`func f() error { if err := g(); err != nil { if r := AsRefusal(err); r != nil { return r }; return broken("x "+err.Error(), "y") }; return nil }`,
		`func f() error { n, err := g(); if err != nil { return misuse("v", "bad ("+badNumber(s, err)+")") }; return nil }`,
		`func f() error { if err := g(); err != nil { if errors.Is(err, io.EOF) { return errors.New("empty") }; return err }; return nil }`,
		`func f() int { if err := g(); err != nil { return ChildFailed(err, 1) }; return 0 }`,
		`func f() int { if err := g(); err != nil { fmt.Fprintln(os.Stderr, err); return 1 }; return 0 }`,
		`func f() int { if err := g(); err != nil { return 0 }; return 0 }`,
		`func f() { _ = x.Close(); _ = tx.Rollback(ctx); _ = os.Remove(p); _ = os.RemoveAll(d); _ = p.Kill(); _ = p.Signal(s); _ = cmd.Wait() }`,
		`func f() { _ = v; x, _ := g(); _ = x }`,
		`func f() error { stderr := g(); if stderr != nil { return errors.New("x") }; return nil }`,
		`func f() error { if err := g(); err != nil { go func() { log.Print(errors.New("in a closure")) }(); return err }; return nil }`,
	} {
		if got := lint(t, src); len(got) != 0 {
			t.Errorf("%s: %v", src, got)
		}
	}
}

func TestRunPrintsFindingsAndExits(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"testdata/silent"}, &out, &errOut); code != 1 || !strings.Contains(out.String(), "silent.go:7: S1:") || !strings.Contains(errOut.String(), "1 silent form(s) in 1 file(s)") {
		t.Fatalf("%d %q %q", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := run([]string{"testdata/clean"}, &out, &errOut); code != 0 || !strings.HasPrefix(out.String(), "silentlint: clean (1 files") {
		t.Fatalf("%d %q", code, out.String())
	}
	if code := run([]string{"testdata/none-such"}, &out, &errOut); code != 2 {
		t.Fatalf("a missing directory is %d", code)
	}
}
