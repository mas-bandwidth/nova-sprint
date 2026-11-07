//go:build !race

package policy

// raceSlowdown is 1 outside a -race build (race.go).
const raceSlowdown = 1
