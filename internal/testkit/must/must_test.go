package must

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// recorder is a testing.TB fake that records a failure instead of stopping the
// test, so a failing table row is observed.
type recorder struct {
	testing.TB
	failed bool
	msgs   []string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.failed = true
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

func (r *recorder) Fail() { r.failed = true }

func (r *recorder) Logf(string, ...any) {}

func (r *recorder) Cleanup(func()) {}

func (r *recorder) Name() string { return "recorder" }

func TestContainsAll(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		got   string
		wants []string
		fail  bool
	}{
		{"all present", "the needs would make a cycle: s1-1 and s1-2", []string{"needs", "s1-1", "s1-2"}, false},
		{"one missing", "the needs would make a cycle", []string{"needs", "s1-2"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{}
			ContainsAll(rec, tc.got, tc.wants...)
			require.Equal(t, tc.fail, rec.failed, rec.msgs)
		})
	}
}

func TestContainsNone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		got  string
		nots []string
		fail bool
	}{
		{"none present", "moved 0", []string{"panic", "boom"}, false},
		{"one present", "moved 0 then panic", []string{"panic"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{}
			ContainsNone(rec, tc.got, tc.nots...)
			require.Equal(t, tc.fail, rec.failed, rec.msgs)
		})
	}
}

func TestFileReads(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	require.NoError(t, os.WriteFile(good, []byte("body\n"), 0o644))
	for _, tc := range []struct {
		name string
		path string
		body string
		fail bool
	}{
		{"reads as body", good, "body\n", false},
		{"different body", good, "other", true},
		{"missing file", filepath.Join(dir, "nope"), "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{}
			FileReads(rec, tc.path, tc.body)
			require.Equal(t, tc.fail, rec.failed, rec.msgs)
		})
	}
}

func TestJSONHas(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		doc  string
		keys []string
		fail bool
	}{
		{"keys present", `{"cards":0,"archived_cards":1}`, []string{"cards", "archived_cards"}, false},
		{"key missing", `{"cards":0}`, []string{"cards", "archived_cards"}, true},
		{"not an object", `[1,2]`, []string{"cards"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{}
			JSONHas(rec, tc.doc, tc.keys...)
			require.Equal(t, tc.fail, rec.failed, rec.msgs)
		})
	}
}

func TestSoleRefusal(t *testing.T) {
	t.Parallel()
	type refusal struct{ Why string }
	for _, tc := range []struct {
		name    string
		moved   []int
		refused []refusal
		fail    bool
	}{
		{"one refusal", nil, []refusal{{Why: "over the bound"}}, false},
		{"moved something", []int{1}, []refusal{{Why: "x"}}, true},
		{"no refusal", nil, nil, true},
		{"two refusals", nil, []refusal{{Why: "a"}, {Why: "b"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{}
			got := SoleRefusal(rec, tc.moved, tc.refused)
			require.Equal(t, tc.fail, rec.failed, rec.msgs)
			if !tc.fail {
				require.Equal(t, "over the bound", got.Why)
			}
		})
	}
}

func ExampleContainsAll() {
	rec := &recorder{}
	ContainsAll(rec, "the needs would make a cycle: s1-1 and s1-2", "s1-1", "s1-2")
	fmt.Println("passed:", !rec.failed)
	// Output: passed: true
}
