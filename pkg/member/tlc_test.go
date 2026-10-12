package member

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureSprint records the verbs a member runs.
type captureSprint struct{ args [][]string }

func (s *captureSprint) Run(args ...string) (int, []byte) {
	s.args = append(s.args, append([]string(nil), args...))
	return 0, nil
}

// A member that found java on its PATH and its tla2tools.jar at start reports TLC on
// every beat (fleet beat --tlc --tlc-jar), so the deal gives it the cards that record a
// TLC run (tlc_deal.go, NeedsTLC); a member with no jar reports none.
func TestABeatReportsTLCWhenJavaAndTheJarAreThere(t *testing.T) {
	t.Parallel()
	oldLook := lookPath
	t.Cleanup(func() { lookPath = oldLook })
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }

	jar := filepath.Join(t.TempDir(), "tla2tools.jar")
	require.NoError(t, os.WriteFile(jar, []byte("x"), 0o644))

	sp := &captureSprint{}
	m := New(Config{As: "m1", TLAJar: jar}, sp, nil, nil, io.Discard)
	require.NoError(t, m.Beat())
	require.Len(t, sp.args, 1)
	assert.Contains(t, sp.args[0], "--tlc")
	assert.Contains(t, sp.args[0], "--tlc-jar")
	assert.Contains(t, sp.args[0], jar)

	m2 := New(Config{As: "m2"}, sp, nil, nil, io.Discard)
	require.NoError(t, m2.Beat())
	require.Len(t, sp.args, 2)
	assert.NotContains(t, sp.args[1], "--tlc", "a member with no jar reports no TLC")
}
