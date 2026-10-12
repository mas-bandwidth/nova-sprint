package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The refusal is a type, found with errors.As, never by its words: the sentence is the
// owner's and may change, and every other refusal of the same store open stays a plain error.
func TestTheHookGateRefusalIsATypedError(t *testing.T) {
	t.Parallel()
	r := newSwitchedRig(t)
	_, err := r.a.store(common{verb: "land", redis: "mem:0", actor: r.seat})
	require.Error(t, err)
	var h *hookGateError
	require.True(t, errors.As(err, &h), "the gate's refusal is a hookGateError: %v", err)
	assert.Equal(t, sprint.HookFirst, h.Error())

	r.hookLive(t)
	_, err = r.a.store(common{verb: "land", redis: "mem:0", actor: r.seat})
	assert.NoError(t, err, "hooked in: the gate lets it through")

	// a verb of the coordinator's alone, run by someone else, is refused, and not by the gate
	_, err = r.a.store(common{verb: "land", redis: "mem:0", actor: "someone-else"})
	require.Error(t, err)
	assert.False(t, errors.As(err, &h), "not the hook gate's refusal: %v", err)
}
