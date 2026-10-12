package swarm

// THE WALL DEATH (issue #918). A harness that DENIES a tool call returns a tool error the
// model routes around and keeps working -- that is the configured fence (fence.go). But a
// harness build that still auto-rejects stops the model there, and the run ends with no
// RESULT.md while the card's commits sit in ./repo: eight of thirty cards died this way on
// 2026-09-16 and the batch scored them `no-result`, a model that chose to publish nothing,
// with the work stranded and unpushed.
//
// WallDeath is what such a run is: the harness's own fence named a path it would not touch,
// the job published nothing, and ./repo is left with commits. The report NAMES the path and
// the commits so the harvester pushes the work -- `WALL task=<id> path=<p>` and, when there
// are commits past the repo's base, `commits=<n> branch=<name>`.
//
// A FENCE REJECTION BESIDE A RESULT IS NOT A DEATH. A run that emitted the line and then
// published is a model that took the tool error and finished, and it is done. The result is
// asked first, wherever the gather looks for one (FindCardResult), so the two shapes never
// trade places.

// fenceCapture is the file the harness's own words land in: the native path's own capture
// (<job>/harness-output.log), or the supervisor's and a runner's <job>/harness.log. Both
// names are asked because the two execution paths write different ones, and the fence line
// is in whichever path ran.

// repoBase is the ref ./repo was cloned from: the branch's own upstream, and failing that
// the remote's default branch under the names a clone writes.
// A WALL DEATH IS A NAMED END (issue #644 and its follow-up).
//
// Card 8311 committed its fix and then died on
//
//	permission requested: external_directory (/.../jobs/scratch/*); auto-rejecting
//
// with no RESULT.md, and the pool reported `no-result` -- the token for a MODEL that chose to
// publish nothing. The model never got the chance: the harness's own fence, or the OS wall,
// stopped it at a path, and the absence of a report is the machinery's doing. The fence half
// of this landed first (fence.go, the `fence` token); this names the END itself, with the
// refused path, the step the card reached, and the commits it left on its branch so the
// harvester can still push the work the dead card had already committed.
//
// THREE THINGS ARE READ OUT OF THE CARD'S OWN LOG, and nothing is guessed: the harness's
// permission auto-reject line (FenceRejection), the sandbox's own `SANDBOX REFUSED` line, and
// `Operation not permitted` ON A PATH -- a bare `kill: Operation not permitted` is not this
// class. The path and the last `STEP <n>` the card printed are what the one report line
// carries.

// The wall's own words, kept here beside the classifier that reads them.
const (
	// WallStepMark is how a card numbers its steps; the LAST one in the log is where the
	// wall stopped it.
	WallStepMark = "STEP "
)

// WallRefusal is one wall death: the path the wall refused and the last step the card
// reached. An empty Path or Step is the dash the report line writes.
type WallRefusal struct {
	Path string
	Step string
}

// WallRefused reports the first path the harness's own fence or the OS wall refused in one
// log, and whether either refused anything. The first is the one that matters: the model
// stops at it, and every later line is a consequence of the same closed path.
// WallStep returns the number on the LAST `STEP <n>` line the card printed, or "" when it
// printed none. A card that died at its second step says so, so the remedy can name where.
// isAllDigits reports whether every byte is a decimal digit and there is at least one.
// wallPathToken is the path a refusal line names: the first quoted string holding a `/`, else
// the first absolute token. A line with no path at all returns "".
// WallLine is the ONE line a wall death is reported on, to the coordinator and to the
// harvester. It names the task, the refused path and the last step the card reached, and --
// when the clone holds commits past its base -- the count and the branch, so work a dead card
// had already committed is not lost with it:
//
//	WALL task=<id> path=<the refused path> step=<the last STEP number seen in the log> [commits=<n> branch=<name>]

// WallCommits reports the branch a clone is on and the commits it holds past its base -- the
// commits reachable from HEAD and not from the base it cloned, which is exactly the work a
// card added before the wall stopped it. The base is the first remote-tracking ref named
// `dev`, `main` or `master`, else the first `refs/remotes/*`; a clone with no remote refs
// counts every commit it has, because there is no base to subtract. It reports false when the
// directory is not a clone or git cannot name the branch, so a report line is only decorated
// with numbers that are real.

// wallBaseRef is the remote-tracking ref a clone's work sits on top of: `dev`, `main` or
// `master` if one is present, else the first remote-tracking ref, else "" when there is none.

// gitOut runs one local git command in dir and returns its trimmed stdout; an error is the
// empty string, because every caller here treats a missing answer as "no answer" and never
// as zero.
// A DENIAL IN THE CAPTURE IS NEVER AN OK, AND IT IS NEVER A DIAGNOSIS EITHER
// (issue #1465, and a friend's HOLD on PR #1478, comment 5737662335).
//
// THE RUN THIS CLOSES. A `native` Go card was handed GOMODCACHE, GOCACHE and
// GOTOOLCHAIN=local for a toolchain under a user directory that the wall admitted no root
// for. It wrote its test, could not compile it, said so in its own RESULT.md, and the tool
// reported `NATIVE OK ... rc=0 sandbox=landlock harness=ok`. A commit nobody had compiled
// read as green, because the only record of the failure was one line in a log:
//
//	/usr/bin/bash: line 1: /home/worker/go/bin/go: Permission denied
//
// WHAT THAT LINE PROVES, AND WHAT IT DOES NOT. The first version of this reader called the
// line an EXEC refusal, named the path a program, concluded that the gate never ran and that
// nothing had been compiled, and prescribed `read_roots`. A friend measured that none of it
// follows. An owned bash running `: > "$1"; printf "RECOVERED\n"` against a non-writable
// directory prints
//
//	/bin/bash: <absolute output path>: Permission denied
//
// -- the identical shape -- attempts no program at all, and exits 0 having RECOVERED. A `cd`
// into an unreadable directory is a third shape with the same words. The shell names a PATH
// and a refusal; it does not name the OPERATION. And this reader runs on `--no-wall` runs
// too, where there is no wall to attribute anything to.
//
// SO THE VERDICT IS A REFUSAL AND THE CAUSE IS UNVERIFIED. What is established is enough on
// its own: the card's shell was denied something, nobody read it, and a disposition of OK
// over an unread denial is what cost the card of #1465. The run is refused. The line is
// quoted verbatim, the operation is labelled `unverified`, and the remedy is to re-run the
// gate and read its stderr -- not a cause invented to fill the field. On a WALLED run the
// read set is offered as ONE CANDIDATE, said to be a candidate.
//
// WHAT THIS STILL CANNOT DO. It cannot bind the verdict to the card's own declared gate,
// because `native` is handed a card as free text and no machine-readable declaration of what
// the gate is or what it returned. Until a card declares its gate in a form the tool can
// read, the honest signal is this one: a denial was seen, its operation is unknown, and the
// disposition is refused rather than OK.

// NewShellDenialReader returns an empty reader for one run's capture.

// Write consumes the child's output line by line.

// line reads one complete line: a STEP mark moves the step, and the first denial is kept.

// Denied is the first denial the capture held and whether it held one. A last line the child
// left without a newline is read too, as ShellDenied reads the end of a file, without being
// consumed, so a later Write still completes it.

// deniedPath is the grammar, and nothing outside it is this class. Five spellings are read,
// each measured off a real log:
//
//	/usr/bin/bash: line 1: /home/worker/go/bin/go: Permission denied   bash, by path
//	bash: /home/worker/go/bin/go: Permission denied                    bash, by name
//	sh: 1: /opt/sdk/go1.26.5/bin/go: Permission denied                dash, which numbers
//	zsh: permission denied: /opt/sdk/go1.26.5/bin/go                  zsh, path last
//	fork/exec /opt/sdk/go1.26.5/bin/go: permission denied             Go's own os/exec
//
// THE PATH IS THE WHOLE SEGMENT BETWEEN THE DELIMITERS (a friend's P1). A shell delimits its
// fields with `: `, and everything between two delimiters is the path -- spaces, parentheses
// and all, because those are ordinary pathname characters. The first version required a
// token with no spaces in it, and `/opt/sdk tool/bin/go` therefore recreated the whole silent
// green of #1465 through an ordinary absolute path. A whitespace-bearing path is not prose.
//
// WHAT KEEPS IT FROM EATING PROSE is the shell at the head of the line, not the shape of the
// path: `cat: /etc/shadow: Permission denied` is some other program's complaint, and a
// sentence a model wrote is not a shell speaking at all.

// Go's own os/exec, which a harness that launched a program itself prints. The words are
// a whole segment, never a substring of a longer one.

// zsh puts its refusal before the path: `zsh: permission denied: <path>`.

// Every other shell puts the path in the segment before the refusal, with an optional
// `line <n>` or bare `<n>` segment in between which this never has to read.

// absSegment is one delimited segment read as a path: absolute, and taken whole. Surrounding
// quotes are stripped because a shell that quotes a path is naming the same path. A segment
// that does not begin with `/` is not one.

// isShellWord says whether a line's first field is a shell, by the base name of whatever
// path ran it: `/usr/bin/bash` and `bash` are the same shell talking.

// DeniedPathRoots is the read set a coordinator would have to open IF the denied path was one
// the child had to read or execute -- a candidate, never a diagnosis. A path reached through
// a symlink needs TWO roots, because the kernel checks the grant against the resolved target
// and a coordinator who names one and not the other loses the card again. A launcher in a
// `bin` directory belongs to the tree above it; anything else is named by its own directory.
//
// Nothing here is specific to Go, to a bench or to a user: every path is read off the denied
// path itself.

// ShellDenialReason is the ONE line an unread denial owes its caller, and the one place its
// words live. It separates what was measured from what was not, by construction:
//
//   - MEASURED: the step, the child's exit code, the wall this ran under, the path named, and
//     the shell's own line, quoted.
//   - NOT MEASURED: which operation was denied. The field says `operation=unverified` and the
//     sentence says why, so nobody reads a cause into it.
//   - THE REMEDY IS A MEASUREMENT, not a guess: re-run the card's own gate against this
//     commit and read its stderr. On a walled run the read set is offered beside it as one
//     candidate among the others.
//
// `wall` is the wall's own name, or empty for a run that had none; a run with no wall is told
// so and nothing is attributed to a sandbox that was not there.
