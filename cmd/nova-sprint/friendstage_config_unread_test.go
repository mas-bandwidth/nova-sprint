package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/stretchr/testify/assert"
)

// A read error from nova-config refuses the stage before anything is written: a
// friend's git identity is unknown then, and attributing her commits to the
// sprint's identity would be a silent lie. The fake runner records every step,
// so the test asserts nothing was staged. It sets the package's runner seam, so
// it is not parallel.
func TestFriendStageRefusesWhenNovaConfigCannotBeRead(t *testing.T) {
	ta, _ := amyWithIdentity(t)
	dealToAmy(t, ta)
	ta.a.friends = func(context.Context, string) ([]config.Row, error) {
		return nil, errors.New("connection refused")
	}

	rec := &benchRec{t: t, files: map[string]string{}}
	rec.install()

	code, out, errs := ta.do(fmt.Sprintf("friend stage s1-1.w1@1 --as friend.amy --bench m1 --dir %s", filepath.Join(t.TempDir(), "jobs")))
	assert.Equal(t, 1, code, "%s", errs)
	assert.Empty(t, out)
	assert.Contains(t, errs, "nova-sprint friend stage REFUSED: nova-config could not be read (connection refused), so friend amy's git identity is unknown; nothing was staged; run: nova-config friend list")
	assert.Empty(t, rec.calls, "a config read failure stages nothing")
}
