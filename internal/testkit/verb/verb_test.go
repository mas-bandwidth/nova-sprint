package verb

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMain is a stand-in tool: `ok` writes two stdout lines at exit 0, `json`
// writes one JSON object at exit 0, and nothing, `boom` or anything else
// refuses on stderr at exit 2.
func fakeMain(args []string, _ io.Reader, out, errb io.Writer) int {
	switch {
	case len(args) == 0:
		fmt.Fprintln(errb, "REFUSED: no door")
		return 2
	case args[0] == "ok":
		fmt.Fprint(out, "ok\nsecond\n")
		return 0
	case args[0] == "json":
		fmt.Fprint(out, `{"a": 1}`)
		return 0
	default:
		fmt.Fprintln(errb, "REFUSED: boom")
		return 2
	}
}

// recordingTB is a require.TestingT fake that records what a real test would
// have failed with, so a broken link is observed without failing the run.
type recordingTB struct {
	failed   bool
	messages []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.failed = true
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

func (r *recordingTB) FailNow() { r.failed = true }

func (r *recordingTB) joined() string { return strings.Join(r.messages, "\n") }

// TestChainLinks drives every chain link with a passing and a failing row: the
// passing row must report nothing through the recording fake, the failing row
// must report its own finding, and every finding prints both streams.
func TestChainLinks(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name   string
		args   []string
		check  func(require.TestingT, *Chain)
		broken string
	}{
		{"Code passes", []string{"ok"}, func(t require.TestingT, c *Chain) { c.Code(0) }, ""},
		{"Code fails", []string{"ok"}, func(t require.TestingT, c *Chain) { c.Code(2) }, "exit 0, want 2"},
		{"Stdout passes", []string{"ok"}, func(t require.TestingT, c *Chain) { c.Stdout("ok\nsecond\n") }, ""},
		{"Stdout fails", []string{"ok"}, func(t require.TestingT, c *Chain) { c.Stdout("nope") }, "stdout differs"},
		{"NotStdout passes", []string{"ok"}, func(t require.TestingT, c *Chain) { c.NotStdout("panic") }, ""},
		{"NotStdout fails", []string{"ok"}, func(t require.TestingT, c *Chain) { c.NotStdout("ok") }, `stdout holds "ok"`},
		{"Stderr passes", []string{"ok"}, func(t require.TestingT, c *Chain) { c.Stderr("") }, ""},
		{"Stderr fails", []string{"boom"}, func(t require.TestingT, c *Chain) { c.Stderr("") }, "stderr differs"},
		{"NotStderr passes", []string{"boom"}, func(t require.TestingT, c *Chain) { c.NotStderr("panic") }, ""},
		{"NotStderr fails", []string{"boom"}, func(t require.TestingT, c *Chain) { c.NotStderr("REFUSED") }, `stderr holds "REFUSED"`},
		{"JSON passes", []string{"json"}, func(t require.TestingT, c *Chain) { c.JSON(&struct{ A int }{}) }, ""},
		{"JSON fails", []string{"ok"}, func(t require.TestingT, c *Chain) { c.JSON(&struct{ A int }{}) }, "stdout is not JSON"},
		{"Lines passes", []string{"ok"}, func(t require.TestingT, c *Chain) { c.Lines("ok", "second") }, ""},
		{"Lines fails", []string{"ok"}, func(t require.TestingT, c *Chain) { c.Lines("ok") }, "stdout lines differ"},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingTB{}
			c := Run(rec, fakeMain, row.args...)
			row.check(rec, c)
			if row.broken == "" {
				assert.Falsef(t, rec.failed, "the passing link reported: %s", rec.joined())
				return
			}
			assert.Truef(t, rec.failed, "the broken link was not reported")
			assert.Containsf(t, rec.joined(), row.broken, "the finding is not the broken link's")
			assert.Containsf(t, rec.joined(), "stdout:", "the finding does not print stdout")
			assert.Containsf(t, rec.joined(), "stderr:", "the finding does not print stderr")
		})
	}
}

// TestRunKeepsTheRawRun pins that the chain hands the caller the exit code and
// both streams for assertions of its own, and that an empty stdin is given.
func TestRunKeepsTheRawRun(t *testing.T) {
	t.Parallel()
	c := Run(t, fakeMain, "boom")
	require.Equal(t, 2, c.Exit)
	require.Empty(t, c.Out)
	require.Contains(t, c.Err, "REFUSED: boom")
}

// TestJSONDecodes proves the JSON link really decodes stdout into the caller's
// value, not just that it does not error.
func TestJSONDecodes(t *testing.T) {
	t.Parallel()
	var got struct{ A int }
	Run(t, fakeMain, "json").JSON(&got)
	require.Equal(t, 1, got.A)
}

// TestRefusalsRunsEachRow pins that a correct refusal table passes: every row
// exits with its wanted code and its stderr holds the wanted text.
func TestRefusalsRunsEachRow(t *testing.T) {
	t.Parallel()
	Refusals(t, fakeMain, []Refusal{
		{Name: "no door", Args: nil, Code: 2, Stderr: "REFUSED: no door"},
		{Name: "boom", Args: []string{"boom"}, Code: 2, Stderr: "REFUSED: boom"},
	})
}

// TestRefusalsRunsEveryRowInParallel proves the table runner gives each row a
// t.Parallel subtest: every row's entry point blocks at a barrier only all
// rows can open, so a serial runner would deadlock, and the peak number of
// rows running at once, read at cleanup after the subtests finish, is more
// than one.
func TestRefusalsRunsEveryRowInParallel(t *testing.T) {
	t.Parallel()
	const n = 3
	var wg sync.WaitGroup
	wg.Add(n)
	var running, peak int32
	block := func(_ []string, _ io.Reader, _, errb io.Writer) int {
		cur := atomic.AddInt32(&running, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
				break
			}
		}
		wg.Done()
		wg.Wait()
		fmt.Fprintln(errb, "REFUSED: no door")
		return 2
	}
	t.Cleanup(func() {
		assert.GreaterOrEqual(t, atomic.LoadInt32(&peak), int32(2), "the rows ran one at a time, not in parallel")
	})
	rows := []Refusal{
		{Name: "a", Args: []string{"a"}, Code: 2, Stderr: "REFUSED"},
		{Name: "b", Args: []string{"b"}, Code: 2, Stderr: "REFUSED"},
		{Name: "c", Args: []string{"c"}, Code: 2, Stderr: "REFUSED"},
	}
	Refusals(t, block, rows)
}

// Example runs a tool, checks the chain, and prints what the tool wrote.
func Example() {
	r := Run(&recordingTB{}, fakeMain, "ok")
	r.Code(0).Stdout("ok\nsecond\n")
	fmt.Print(r.Out)
	// Output:
	// ok
	// second
}
