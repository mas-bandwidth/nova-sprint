// Package cardcontract is the frame around a card's task (docs/SPEC-CARD-CONTRACT.md): the
// frame the member hands native (Frame), the result shape a child ends with (Result), and
// the profiles, keyed by model family, that write JOB.md and the shims first on the child's
// PATH so the child meets the frame through the commands it already knows.
package cardcontract

// Frame is what the member knows of one launch and hands native as a file: the repository
// and the commit to stage, the branch the checkout is on (and the member pushes), the
// attempt, what the attempt before left, the tier and the model. It is the packet's, never
// the brief's prose.
type Frame struct {
	Kind       string   `json:"kind"` // work or read
	Card       string   `json:"card"`
	Attempt    int      `json:"attempt"`
	Tier       string   `json:"tier,omitempty"`
	Model      string   `json:"model"`
	Repo       string   `json:"repo"`                   // the clone URL or local path the card works in; "" when it names none
	BaseRef    string   `json:"base_ref,omitempty"`     // the ref the card's work is based on (a pull request's base)
	StageSha   string   `json:"stage_sha,omitempty"`    // the commit staged: a previous attempt's pushed head, a read's head under read, else the card's base sha
	Branch     string   `json:"branch"`                 // the branch the checkout is on: the card's sprint branch, a read's work branch
	PrevHead   string   `json:"prev_head,omitempty"`    // the last pushed head of any earlier attempt (sprint.BaseOf)
	PrevFrom   int      `json:"prev_attempt,omitempty"` // the attempt PrevHead is the head of
	Why        string   `json:"why,omitempty"`          // a rework: how the attempt before ended
	Finding    string   `json:"finding,omitempty"`      // a rework: what the readers of the attempt before found
	Fix        string   `json:"fix,omitempty"`          // a rework: what the coordinator asks of this attempt
	ReviewBase string   `json:"review_base,omitempty"`  // a read: the ref the change is reviewed against
	Stage      []string `json:"stage,omitempty"`        // the recipe files the brief's Stage: header lines name, relative to Recipes
	Recipes    string   `json:"recipes,omitempty"`      // the member's recipes directory, <root>/recipes
	// A decide read's bars on p(defect) (docs/SPEC-SPRINT.md section 6): native asks the
	// read decision over the card and the diff before any child and routes the read by
	// them; both empty for a strings read.
	DecideBounce string `json:"decide_bounce,omitempty"`
	DecideReview string `json:"decide_review,omitempty"`
	// A work card's gate decision bars (docs/SPEC-SPRINT.md section 5, the gate verdict):
	// native classifies a red gate's failures by them after the child, before the member
	// reports the take; both empty for none.
	DecideGateFlaky       string `json:"decide_gate_flaky,omitempty"`
	DecideGatePreexisting string `json:"decide_gate_preexisting,omitempty"`
}

// Staged is what native knows once the checkout is staged: the job directory, the checkout,
// the commit it is at, and the real git the shims hand through to.
type Staged struct {
	Job  string // <slot>/jobs/<label>
	Repo string // <job>/repo
	Head string // the full sha the checkout is at
	Git  string // the real git, absolute
	// Start is a read's: the commit the work under review started from, a full sha (the
	// merge base of Head and the review base, found when the checkout was staged), so the
	// read sees exactly the work's change however far the base branch has moved since;
	// "" for work, or when no merge base was found.
	Start   string
	GoCache string // the machine's shared build cache the child's GOCACHE names; "" when it has none
	Gate    *Gate  // a read's gate (ReadGate); nil: the card's
	// Carry is a rework's staging at the tip of its base branch (Carry); nil for a first
	// attempt, a read, and a rework whose base is a sha or a tag, which never moves.
	Carry *Carry
}

// Shim is one script a profile writes first on the child's PATH.
type Shim struct {
	Name   string // the command it answers for: git, gh
	Script string // a POSIX sh script
}

// Profile is how one model family meets the frame (docs/SPEC-CARD-CONTRACT.md section 5).
type Profile interface {
	Family() string
	JobText(f Frame, s Staged) string
	Shims(f Frame, s Staged) []Shim
}

// Families are the model families a profile is keyed by, plain last: the fallback.
var Families = []string{"claude", "openai", "gemini", "grok", "deepseek", "plain"}

// profiles are the built profiles by family; a family with none here serves plain.
var profiles = map[string]Profile{"claude": claude{}, "openai": openai{}, "plain": plain{family: "plain"}}

// For is the profile of a family: its own, else plain under the family's name.
func For(family string) Profile {
	if p, ok := profiles[family]; ok {
		return p
	}
	return plain{family: family}
}

// FrameName is the file name the member writes a frame under, beside the card file.
const FrameName = ".frame.json"

// JobName is the frame's text in the job directory, the first thing the child reads.
const JobName = "JOB.md"

// ReadTitle begins the first line of a read's JOB.md in every profile, and of no work's: a
// brief that speaks to its readers names it, so a work card never takes itself for a read
// and stops with nothing to do.
const ReadTitle = "# JOB: read"

// PushedName is the file in the job directory the git shim records each push in:
// branch, head and checkout top, tab separated, one line a push.
const PushedName = ".sprint/pushed.tsv"

// FinishName is the file in the job directory the gh shim records the finish in
// (gh pr create, gh pr review), in the result shape: a file the child is never
// told to write, so a RESULT.md the child writes after it does not overwrite it.
const FinishName = ".sprint/finish.md"

// StagedName is the file in the slot directory (outside the job, which the wall
// lets the child write) where native records the commit it staged, the one the
// member counts the child's commits from.
const StagedName = "staged"

// RecipesName is the directory recipes are kept in, in the member's root, and staged into,
// in the job directory.
const RecipesName = "recipes"

// BrokenFindingText is what every profile's read JOB.md says of a broken verdict
// (docs/SPEC-CARD-CONTRACT.md section 3): it names the defect, or it is no verdict.
const BrokenFindingText = "A broken verdict tells them what to do: at least one finding line names the file (file:line), the line, or the card's STEP or RULE the work breaks, and says what to change. \"Request changes.\" alone, or an approval's words, is no finding: a broken verdict that names no file, line or rule is not a verdict, and the sprint asks another reader."

// ShapeRules is what a job says right after the shape, in the worker's own terms: the
// file opens with `head:` on line 1 and carries all six keys, written as plain `key: value`
// lines. A RESULT.md in another form is refused with the reason `no RESULT.md shape` and the
// attempt is lost, however much work it reports: measured on a worker that wrote a
// `RESULT: <card> sha=<sha>` line and the word DONE (main-report-schema-new-leg-2.w4, the
// head it named the staged commit and not its own), and on one that left out the report
// line. Plain text, no markdown on the keys, nothing before line 1.
const ShapeRules = "Write the file as plain `key: value` lines, exactly as above: line 1 is `head:` (not a `RESULT:` line, no heading, no code fence, no bold or bullets on the keys), all six keys are required (`-` where the line says \"or -\"). A file in any other form (DONE or BLOCKED words, a prose summary) is refused as `no RESULT.md shape` and the attempt is lost, however much work it reports.\n"

// ShapeText is the result shape as JOB.md quotes it, for a work card or a read.
func ShapeText(kind string) string {
	verdict := "ok | not-done | nothing"
	body := "what you would put in a pull request body"
	if kind == "read" {
		verdict = "ok | broken"
		body = "your findings, each with file:line"
	}
	return "    head: <the commit, full sha>\n    branch: <the branch it is on>\n    verdict: " + verdict +
		"\n    gate: <the gate command you ran, or ->\n    output: <the path of its output, or ->\n    report: <one line>\n\n    ## Body\n\n    <" + body + ">\n"
}

// ShapeWorkRule is the rule a work card's job adds: the head is the commit the work made.
const ShapeWorkRule = "`head:` is the commit you made on this branch, never the commit the checkout started from.\n"
