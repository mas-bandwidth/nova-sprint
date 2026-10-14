package safepath

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The refusal texts are part of the contract: a reaper, a map and a person all
// read them, so refuseUnsafeRoot and refuseUnsafePath must keep saying exactly
// this, the root and the path phrased differently.
func TestRefuseUnsafeKeepsTheTexts(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	p := Policy{UserHomeDir: func() (string, error) { return home, nil }}

	cases := []struct {
		name string
		give string
		want string
	}{
		{"root disk", string(os.PathSeparator),
			`refusing to remove an unsafe path: the root is "/", the whole disk`},
		{"path disk", string(os.PathSeparator),
			`refusing to remove an unsafe path: the path resolves to "/", the whole disk`},
		{"root home", home,
			`refusing to remove an unsafe path: the root is the user's home "` + home + `"`},
		{"path home", home,
			`refusing to remove an unsafe path: the path resolves to the user's home "` + home + `"`},
	}

	for _, c := range cases {
		var err error
		if c.name[:4] == "root" {
			err = p.refuseUnsafeRoot(c.give)
		} else {
			err = p.refuseUnsafePath(c.give)
		}
		assert.EqualError(t, err, c.want, c.name)
	}
}
