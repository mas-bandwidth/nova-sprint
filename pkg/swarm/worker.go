package swarm

// THE WORKER DESCRIPTION: which provider, which model, which environment variable the
// provider reads, which base URL, and where the key file is.
//
// It is a FILE because it is configuration a person wrote, and it is REQUIRED because this
// tool has no opinion about whose model runs. No field of it comes from the environment:
// the prototype defaulted the pool, the log directory, the worker directory and the key
// file from environment variables and $HOME, and every one of those is a flag or a field
// here (SPEC.md, no guessing).

// Worker is a worker description, decoded strictly: an unknown field is a refusal, because
// a misspelled field in a file that names a key's location is a silent default.
type Worker struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url,omitempty"`
	EnvVar   string `json:"env_var"`
	KeyFile  string `json:"key_file"`
	// SECRET (issue #881): the NAME of the environment variable that holds the key in
	// THIS process's own environment, delivered by `nova-secrets exec` around the run
	// (docs/SPEC-SECRETS.md, the second caller). It is the replacement for `key_file`
	// when the key is sealed once and delivered at use and NEVER a file on disk. Exactly
	// one of `key_file` and `secret` is set; run and supervise require that variable to
	// be present and non-empty, and the value is never written to a file, never printed,
	// and never in a RUN or SUPERVISE line.
	Secret string `json:"secret,omitempty"`
	Usage  string `json:"usage"`
	// CLASS (CARD-8390): `public` means this worker never sees private source
	// (the public-class gate); `paid` is the default. Empty decodes as paid so
	// no description written before the gate changes meaning by being re-read.
	//
	// CLASS (worker check): `class` is the one word public or paid, an OPTIONAL
	// field a description may carry and the launcher never requires. `nova-swarm
	// worker check` validates it when it is present. The two budgets below are
	// the same fields worker check validates.
	Class       string   `json:"class,omitempty"`
	Harness     string   `json:"harness"`
	HarnessArgs []string `json:"harness_args,omitempty"`
	WorkerDir   string   `json:"worker_dir"`
	Deadline    string   `json:"deadline"`
	Board       string   `json:"board,omitempty"`
	// READ ROOTS: the one field the wall added (docs/SPEC-SANDBOX.md, "there is no --root
	// flag"). Every job runs inside nova-sandbox, whose read set is the OS and toolchain
	// roots plus what the caller names; a toolchain installed into a USER directory -- Go
	// under ~/go, node under ~/.nvm, a machine's /Users/<user>/toolchains -- is under no
	// system root, so a harness that needs one dies inside the wall and runs outside it.
	// It is OPTIONAL, it is a list of absolute existing directories, and it is READ-ONLY:
	// the write set is the job's own and is never configurable from a file. A worker that
	// needs nothing beyond the system roots names nothing here.
	ReadRoots []string `json:"read_roots,omitempty"`

	// CARD BUDGET (CARD-8317): the optional per-card stop. A runaway card is a
	// prompt defect, not a model one: max_turns caps the harness output log's
	// assistant turns and max_cache_read caps the observed cache_read, and run
	// stops the card at either with end=budget and a PROMPT-DEFECT line. Zero
	// is unset; negative is refused at load.
	MaxTurns     int `json:"max_turns,omitempty"`
	MaxCacheRead int `json:"max_cache_read,omitempty"`

	// PROVIDER PHRASES: the second optional field, and it is the TRIAGE LINE'S (#103). A
	// job that dies because the request did not fit is its own failure class, and the only
	// evidence of it is a sentence the provider printed -- in the provider's own words.
	// OpenCode says `Rate limit reached: input token limit exceeded`; the tool carries a
	// table of the sentences it has met (InputLimitPhrases), and a description may ADD the
	// one its provider uses. It never SUBSTITUTES: a caller teaching the tool their
	// provider's words cannot silently un-teach it another's.
	InputLimitPhrases []string `json:"input_limit_phrases,omitempty"`

	// LAUNCH GRACE (issue #900): how long a harness may run before its exit stops
	// counting as a LAUNCH failure. Nine of forty Muse requests died in under two
	// seconds with a provider 5xx and wasted the slot they held; a death inside this
	// window whose tail names a provider server error is a launch that did not take,
	// and the dispatcher retries it instead of filing it. OPTIONAL: the default is
	// DefaultLaunchGrace (15s). A slow failure -- one that takes longer than this -- is
	// a real run that failed and is never retried.
	LaunchGrace string `json:"launch_grace,omitempty"`

	// KEEP DATA (issue #1048): a worker description that sets keep_data=true asks `run` not
	// to reap the finished slots' data/ and tmp/ at task end. It is a person's debugging
	// switch: the default is false, and the shared per-bench caches under <root>/cache are
	// never touched either way.
	KeepData bool `json:"keep_data,omitempty"`
}

// The usage sources a description may declare (rule 13). There are two.
//
// A SOURCE IS NAMED FOR WHAT IT IS. `opencode` promised that OpenCode's own accounting is
// read, and did not read it: on 2026-09-11 two real jobs burned 61,875 and 85,308 tokens
// against `--tokens 20000` and both reported `budget=-/20000`, because OpenCode writes
// `opencode.db` and this tool read a tab-separated file instead. The name is true now --
// `opencode` reads that database through `sqlite3`, read-only, in the data home this tool
// exported for the job -- and the tab-separated file is not a name a description may carry:
// a caller names a SOURCE, never a file some harness might write.
const (
	UsageOpenCode = "opencode"
	UsageNone     = "none"
)

// LoadWorker reads and checks a worker description, reporting EVERY independent problem in
// one run (ONBOARDING point 2) rather than sending a first run back once per field.

// EVERY PATH IN A WORKER DESCRIPTION IS ABSOLUTE FROM HERE ON. The harness runs with
// its cwd set to the SLOT directory, and the paths this tool hands it -- the prompt
// file, the job directory the prompt calls the only place it writes -- are built from
// the description. A relative `worker_dir`, the style the README teaches, made every
// one of them a path that does not exist from where the child stands: two jobs, rc=0,
// `result=no-result dest=failed`, under a RUN OK byte-identical to a good pass (the
// new-user audit, F3, 2026-09-11). The tests could not see it because they all used an
// absolute t.TempDir().

// SECRET IMPLIES ENV_VAR (issue #881): a description that names `secret` but no
// `env_var` loads with env_var defaulting to the secret NAME -- the key is delivered
// by `nova-secrets exec` under that NAME, and the harness config carries the
// variable's NAME, so the NAME is the same string in both fields. An explicit
// `env_var` beside `secret` is kept as typed.

// THE KEY IS NAMED TWO WAYS, AND A DESCRIPTION CARRIES EXACTLY ONE (issue #881). A
// `key_file` is the old shape: a file the person wrote, read as data. A `secret` is
// the name of the variable `nova-secrets exec` delivers into this process's own
// environment -- the key is sealed once and delivered at use, never a file on disk.
// Neither is a description with no key at all, and both is a description with two
// contradicting answers; each is refused here, at the one moment a person can still
// fix it.

// CLASS (CARD-8390): `public` confines the worker to listed public source,
// `paid` (and empty, the default) confines nothing. Anything else is a
// misspelled confinement and is refused here, where it can still be fixed.

// D1, 2026-09-11: the model must REACH THE CHILD. A description that names a model and
// never places it in the harness's argv launches a harness that was told nothing, and
// the jobs die under a green RUN OK. harness_args is the invocation, and `{model}` is
// where the model goes.

// A PHRASE IS A KNIFE, AND IT HAS A FLOOR. A job classed `input-limit` is a job that is
// never retried, and the only floor on a description's phrase was that it not be empty:
// `input_limit_phrases: ["limit"]` would class every failed job whose log carries the
// word (Fable's read of #150, finding 4). It is refused HERE, at the one moment a person
// can still type the sentence, and the refusal QUOTES what they typed.

// Rule 5 of the wall is "paths are resolved, absolute and existing", and a read root
// that is not there is refused BY THE WALL at every launch, one job at a time. It is
// worth one sentence here instead, at the one moment the caller can still fix it.

// THE KEY FILE IS IN NEITHER LIST, AND THE TOOL MUST NOT PUT IT IN ONE. The wall's
// caller section says "the key FILE is in neither list, so the job cannot read it even
// if it is told to" (SPEC-SANDBOX rule 6), and the job's read set is the SLOT
// directory -- which `RefreshSlot` fills by copying every regular file of `worker_dir`
// into it (copyTree). A `key_file` under `worker_dir` is therefore COPIED INSIDE THE
// WALL by this tool, at every refresh, and the job reads the copy under a green
// `SANDBOX OK` and a probe that passed: the probe's own `secret_inside_allow` check is
// made against the PROBE's lists, never against a job's (a friend's Fable read of #88 at
// d0c1841, M1). A `read_roots` entry holding the key is the same hole without the copy.
// Both are refused HERE, at the one moment a person can still move the file.

// AND THE REFUSAL NAMES THE KEY AS THE DESCRIPTION SPELLED IT. Every check below
// MATCHES on the resolved spelling, because that is the file the wall opens (rule
// 5) -- but the resolved spelling is a string that appears in no description and in
// no editor, so a line that leads with it names a path the reader cannot grep for
// and cannot edit. On windows a directory typed
// `C:\Users\RUNNER~1\AppData\Local\Temp\x` resolves to the long name and shares
// no prefix with what was typed; on darwin `/var/...` resolves to `/private/var/...`
// and merely hid the same fault behind a substring (windows CI of #88 at d8a5824).
// So the line leads with the TYPED, cleaned path and appends the resolved one ONCE
// when it differs -- one problem line per case, either way.

// AND THE SLOT DIRECTORIES, WHICH ARE SIBLINGS OF worker_dir, NOT UNDER IT. A job's
// `--read` is its slot, `<worker_dir>-<n>` (SlotDir), so a key file placed directly in a
// slot -- `<worker_dir>-1/.key`, or anything beneath it such as under its `jobs/` -- is
// READ INSIDE THE WALL under a probe that passed, and neither check above sees it:
// `insideDir` treats `worker-1` as a sibling of `worker` by design, and no `read_roots`
// entry names it (a friend's Fable read 2 of #88 at fc400ce, L1). It is refused HERE rather
// than in `Run` beside the gate so that it holds for EVERY slot number, not only the ones
// one run happens to use, and so that `run` and `supervise` say it too.
//
// BOTH SPELLINGS OF worker_dir, TYPED AND RESOLVED. The check above resolves because what
// the wall COPIES is the target's contents; this one is different in kind. `SlotDir` is
// `fmt.Sprintf("%s-%d", w.WorkerDir, slot)` on the TYPED spelling, and `worker_dir` is only
// made absolute at load, never symlink-resolved -- so with `worker_dir` a symlink
// `/typed/worker -> /real/worker` the slot the tool creates and hands to `--read` is
// `/typed/worker-1`, while the resolved sibling `/real/worker-1` is a directory nobody
// builds. Asking only the resolved one let a key at `/typed/worker-1/.key` pass (a friend's
// Fable read 3 of #88 at 56c7dcc, L1). The typed spelling is the one the slot is built
// from; the resolved one stays as defence in depth. Deduped, so the ordinary case where
// the two coincide refuses once.

// `slot` and `dir` are the spelling this hit was found under, and the typed
// one is tried first, so the ordinary case and a symlinked `worker_dir` both
// name the directory the tool would actually build and hand to `--read`.

// resolvePath is a path with its symlinks followed, which is how the wall reads one (rule
// 5): a check made on the typed spelling is a check a symlink walks around. A path that
// cannot be resolved -- it does not exist yet, most often -- is cleaned and answered as
// typed, because a refusal is worth more than a silence and the caller can still see it.

// spellings is the distinct ways one path can be written for a check: as typed (cleaned)
// and as resolved. A check that asks only one of them is a check the other walks around --
// which side matters depends on what the wall does with the path, so a check that cannot
// choose asks both (read 3 of #88, L1). An empty path has no spellings.

// resolvedTail is the ONE tail a key refusal grows, and only where the resolution is the
// thing that made the match: a `key_file` that is a symlink into the read set, or a
// `worker_dir` that is one. Everything else in the line is the spelling the DESCRIPTION
// used, because that is the string a person greps for and the one they will edit; the
// resolved spelling appears in no file they have.
//
// Naming the resolved path unconditionally is what the windows CI of #88 at d8a5824 caught:
// a temp directory typed `C:\Users\RUNNER~1\AppData\Local\Temp\x` resolves to the long
// name and shares no prefix with what was typed, so the refusal named a path nobody had
// written; on darwin the same fault hid behind a substring, because `/var/...` resolves to
// `/private/var/...` and merely gains a prefix. One problem line per case, either way.

// insideDir says whether a path lies under a directory. The directory itself is not inside
// itself, and a sibling whose name merely starts the same way is not either.
//
// AND THE ANSWER IS NOT A STRING COMPARISON, BECAUSE A FILESYSTEM IS NOT A STRING.
// `strings.HasPrefix` over cleaned, resolved paths is case-SENSITIVE and APFS is
// case-INsensitive by default, so a `key_file` typed `/x/Worker/.key` under a `worker_dir` of
// `/x/worker` is THE SAME FILE the slot copy picks up and the prefix said no: the load passed
// and `RefreshSlot` put the key inside the wall under a green `SANDBOX OK` (both cold readers
// of #88 at fc400ce; issue #100). `filepath.EvalSymlinks` does not fold case on darwin, so
// resolving the path does not close it either -- nor would lowercasing, which is wrong on a
// case-sensitive filesystem where `/x/Worker` and `/x/worker` are two directories.
//
// So the string prefix stays as the CHEAP answer, and it is the only answer available for a
// path that does not exist yet -- `resolvePath` answers as typed for one, and a refusal is
// worth more than a silence. Where it says no, each EXISTING ancestor of the path is asked
// `os.SameFile` against the directory: device and inode is the question the filesystem itself
// answers, so it holds for a case fold, for a mount of the same directory at two names, and
// for a hard-linked directory where one exists. The walk starts at the path's parent, so a
// path that IS the directory under another spelling is still not inside it, which is what the
// first line of this function says. This runs at load, once per description, never per job.

// a directory that is not there holds nothing

// the root is its own parent: the walk is over

// DefaultDeadline is the worker description's own, which add --deadline overrides per task.

// validEnvName is the shape a `secret` may take: an environment variable NAME, the same
// shape a POSIX shell would accept for one. A name with a `=` is a value dressed as a
// name, and a name with whitespace or a dash is a typo the loader catches where the caller
// can still fix it rather than at the first run.

// slotDirHolding answers the slot directory of workerDir that holds path -- a sibling of
// workerDir spelled <base>-<digits>, which is what SlotDir builds -- or "" when path is
// under no slot. A path that IS a slot directory is not held by it, which matches insideDir.
//
// AND THE SLOT'S NAME IS JUDGED UNDER THE FILESYSTEM'S OWN EQUALITY, NOT AS TEXT (#145). The
// previous revision cut `<base>-` off the first path component with `strings.CutPrefix`, a
// case-SENSITIVE comparison, while APFS is case-INsensitive by default: with `worker_dir`
// `<dir>/worker`, a `key_file` at `<dir>/Worker-1/.key` is in the directory `RefreshSlot`
// opens and hands to `--read`, `CutPrefix("Worker-1", "worker-")` said no, and the job read
// the key inside the wall under a green `SANDBOX OK` (SPEC-SANDBOX rule 6; #100 one directory
// over). Lowercasing is not the repair either: on a case-SENSITIVE filesystem `<dir>/Worker-1`
// and `<dir>/worker-1` are two directories and folding them refuses a sound placement.
//
// THE CANDIDATE COMES FROM THE PATH, NOT FROM workerDir, WHICH IS WHY THIS REPAIR IS A
// DIFFERENT SHAPE FROM insideDir's. insideDir can ask `os.SameFile` of the two directories
// because both exist at load; a slot directory need not -- slots are created at run -- so
// there may be no `<parent>/<base>-<n>` inode to compare, while the directory holding the key
// is right there in the path. So the ancestors of the path are walked toward
// `filepath.Dir(workerDir)`, and the ancestor that is a direct child of that parent has its
// name judged: `<digits>` as digits, and `<base>` under the filesystem's own equality
// (namesOneFile, which measures the fold rather than reading runtime.GOOS).
//
// The walk starts at the path's PARENT, so a path that IS a slot directory is still not held
// by it. Both spellings of workerDir are still asked by the caller, for the reason read 3 of
// #88 gave: `SlotDir` builds the slot from the TYPED spelling.

// the root is its own parent: the walk is over

// no slot number, so not a slot directory

// The FULL spellings, the candidate against the slot `SlotDir` would build for
// that number: where both are there the two directories answer for themselves
// and no fold is guessed (a friend's read of #159, comment 5648066751).

// another worker's slot, or a neighbour that merely reads alike

// SlotDir is a slot's own working directory, <worker-dir>-<slot>.

// JobDir is a slot's job directory for one task.

// DataHome is the job's own data home, exported as XDG_DATA_HOME so the harness keeps its
// own database there, one per job. It is the whole reason slots exist.

// RefreshSlot copies the home worker directory into the slot directory, ONE WAY. It is a
// copy rather than a mount or a link: a worker that could write back into the home copy
// could change the next worker's self, and the next worker would load it without anybody
// reading the change. The jobs/ subtree is left alone, because it is the slot's own work
// and not the home copy's.

// A symlink or a device in a worker's home copy is not copied: what a slot gets
// is files, and a link that pointed outside the home copy would be the one way
// the refresh could stop being one way.

// HarnessConfig is the config this tool writes for the harness, and it carries the
// environment variable's NAME, never its value: a value written here would be a key at rest
// in a directory nobody treats as a secret store. It is rewritten at every start, so a
// stale provider declaration cannot outlive a worker description.

// THE SUBAGENT REFUSAL HOOK (issue #2533). A card that tries to spawn a
// subagent has the spawn refused by this hook and logged once. A spawned
// agent multiplies exploration tokens, hides its work from the log, and on
// a darwin bench goes silent under the wall and the card is killed as idle.

// WriteHarnessConfig writes that config into the slot directory at every start.

// HarnessTail is the LAST thing the harness said, bounded: the one diagnosis of a failed
// job, which lived in <job>/harness.log, was printed by no verb, and was then deleted with
// the job directory by `reclaim`. A line whose key is wrong had no printed route to the
// word `unauthorized` (the new-user audit, F5, 2026-09-11).
