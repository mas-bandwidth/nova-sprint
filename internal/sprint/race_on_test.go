//go:build race

package sprint

// underRace says the tests run under the race detector, whose instrumentation adds
// allocations of its own, so an allocation-count bound is asserted only without it.
const underRace = true
