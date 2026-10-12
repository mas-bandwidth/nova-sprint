//go:build functional

// selfByAddress asks the machine's resolver, under a wall-clock bound: a functional
// test (RULES 6a: no external service or real time in a unit test). The seat check's
// unit tests are seat_check_offline_test.go.
package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSelfByAddressKnowsThisMachine(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	self := selfByAddress(func() string { return "studio" })
	assert.True(t, self(ctx, "Studio"), "its own hostname, any case")
	assert.True(t, self(ctx, "localhost"), "a name that resolves to loopback")
	assert.False(t, self(ctx, "no-such-member.invalid"), "a name that does not resolve is not this machine")
}
