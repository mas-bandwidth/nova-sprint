//go:build race

package policy

// raceSlowdown scales TickTime in a -race build, which runs Starlark five to
// ten times slower (measured on amd-3: the default's 5,000
// producer tick 60 ms to 472 ms; a 512 MiB string 0.36 s to 1.9 s). Three
// keeps both timing tests meaningful: the 10M-step tick (about
// 225 ms under -race) stops on steps, the 512 MiB string on time.
const raceSlowdown = 3
