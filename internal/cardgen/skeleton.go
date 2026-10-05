package cardgen

import (
	"fmt"
	"os"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

// SkeletonOptions holds the inputs for rendering a new card brief skeleton.
type SkeletonOptions struct {
	ID      string
	Repo    string
	Base    string
	Task    string
	Paths   string
	Shared  string
	Test    string
	Gate    string
	Tier    string
	Needs   string
	Minutes int
	Kind    string
	Start   string
	Stop    string
}

// RenderSkeleton formats a lint-clean brief from the given card options.
func RenderSkeleton(opts SkeletonOptions) string {
	tier := opts.Tier
	if tier == "" {
		tier = "pro"
	}
	minutes := opts.Minutes
	if minutes == 0 {
		minutes = Deadline(tier)
	}
	kind := opts.Kind
	if kind == "" {
		kind = "fix-red"
	}
	needs := opts.Needs
	if needs == "" {
		needs = "-"
	}

	tl, _ := cardhdr.ParseTest(opts.Test)
	testName := tl.Name
	if testName == "" {
		testName = "the class test"
	}

	start := opts.Start
	if start == "" {
		firstPath := opts.Paths
		if idx := strings.Index(firstPath, ","); idx >= 0 {
			firstPath = strings.TrimSpace(firstPath[:idx])
		}
		if tl.Package != "" && tl.Package != firstPath {
			start = firstPath + ", " + tl.Package
		} else {
			start = firstPath
		}
	}

	stop := opts.Stop
	if stop == "" {
		if tl.None {
			stop = "the task is complete and the STEP 4 gate passes"
		} else {
			stop = fmt.Sprintf("the test %s is red before the change and green after it, and the STEP 4 gate passes", testName)
		}
	}

	gateCmd := strings.TrimSpace(opts.Gate)
	if !strings.HasPrefix(gateCmd, "go ") {
		gateCmd = "go test -count=1 -timeout 600s " + gateCmd
	}

	var b strings.Builder
	fmt.Fprintf(&b, "RESULT: %s tier: %s\n", opts.ID, tier)
	fmt.Fprintf(&b, "REPO: %s\n", opts.Repo)
	fmt.Fprintf(&b, "BASE: %s\n", opts.Base)
	fmt.Fprintf(&b, "KIND: %s\n", kind)
	fmt.Fprintf(&b, "DEPENDS-ON: %s\n", needs)
	fmt.Fprintf(&b, "PATHS: %s\n", opts.Paths)
	if opts.Shared != "" && opts.Shared != "-" && opts.Shared != "none" {
		fmt.Fprintf(&b, "SHARED: %s\n", opts.Shared)
	}
	fmt.Fprintf(&b, "TEST: %s\n", opts.Test)
	fmt.Fprintf(&b, "START: %s\n", start)
	fmt.Fprintf(&b, "STOP: %s\n", stop)
	fmt.Fprintf(&b, "Deadline: finish within %d minutes.\n\n", minutes)

	b.WriteString("You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.\n\n")

	b.WriteString("RULES.\n")
	for _, r := range swarm.DefaultChildRules {
		b.WriteString(r.Sentence + "\n")
	}
	b.WriteString("\n")

	b.WriteString("ATTRIBUTION. Commits and reports name your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.\n\n")

	fmt.Fprintf(&b, "THE TASK. %s\n", strings.TrimSpace(opts.Task))
	b.WriteString("Libraries considered: none new; standard library and testify.\n\n")

	b.WriteString("STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.\n")

	if tl.None {
		b.WriteString("STEP 2. Write the red test first where appropriate with fakes and the in-memory twin; no test sleeps on the wall clock.\n")
	} else {
		fmt.Fprintf(&b, "STEP 2. Write the red test first with %s; run %s -run %s and keep the failing line as evidence.\n", testName, gateCmd, testName)
	}

	b.WriteString("STEP 3. Make it pass in the files this card names, and only those.\n")

	fmt.Fprintf(&b, "STEP 4. Run the gate: %s and read the last line of each. %s\n", gateCmd, swarm.GateNamesWhoseFile)

	fmt.Fprintf(&b, "STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6), and the member makes both, against %s, from outside the wall when the card finishes. The pull request body states the diff stat, what was deleted, the tests with what each pins, and what was not done.\n", opts.Base)

	b.WriteString("STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): where JOB.md ends the card with its pull request, that is the end and there is nothing else to write, the gate's lines in the pull request body; where it asks for RESULT.md, write it in JOB.md's shape (head, branch, verdict, gate, output, report).\n")

	return b.String()
}

// ParseBatchTSV reads a TSV file of card specifications, applying defaults for missing columns.
func ParseBatchTSV(text string, defaults SkeletonOptions) ([]SkeletonOptions, error) {
	lines := strings.Split(text, "\n")
	var cards []SkeletonOptions
	colMap := map[string]int{}
	headerParsed := false

	for lineNum, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}

		if !headerParsed {
			// Check if this line looks like a header
			firstCol := strings.ToLower(fields[0])
			if firstCol == "id" || firstCol == "card" || firstCol == "name" {
				for i, col := range fields {
					name := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(col, "-", "_"), " ", "_"))
					colMap[name] = i
				}
				headerParsed = true
				continue
			}
			headerParsed = true
		}

		opts := defaults
		getCol := func(names ...string) string {
			for _, name := range names {
				if idx, ok := colMap[name]; ok && idx < len(fields) {
					return fields[idx]
				}
			}
			return ""
		}

		if len(colMap) > 0 {
			if v := getCol("id", "card", "card_id"); v != "" {
				opts.ID = v
			}
			if v := getCol("repo", "repository"); v != "" {
				opts.Repo = v
			}
			if v := getCol("base", "branch"); v != "" {
				opts.Base = v
			}
			if v := getCol("task_file", "taskfile", "file"); v != "" {
				if rawText, err := os.ReadFile(v); err == nil {
					opts.Task = string(rawText)
				} else {
					opts.Task = v
				}
			}
			if v := getCol("task", "task_text"); v != "" {
				opts.Task = v
			}
			if v := getCol("paths", "path"); v != "" {
				opts.Paths = v
			}
			if v := getCol("shared"); v != "" {
				opts.Shared = v
			}
			if v := getCol("test"); v != "" {
				opts.Test = v
			}
			if v := getCol("gate"); v != "" {
				opts.Gate = v
			}
			if v := getCol("tier"); v != "" {
				opts.Tier = v
			}
			if v := getCol("needs", "depends_on", "deps"); v != "" {
				opts.Needs = v
			}
		} else {
			// Positional columns fallback
			// 0: id, 1: task/file, 2: paths, 3: test, 4: gate, 5: tier, 6: shared, 7: needs
			if len(fields) > 0 && fields[0] != "" {
				opts.ID = fields[0]
			}
			if len(fields) > 1 && fields[1] != "" {
				if rawText, err := os.ReadFile(fields[1]); err == nil {
					opts.Task = string(rawText)
				} else {
					opts.Task = fields[1]
				}
			}
			if len(fields) > 2 && fields[2] != "" {
				opts.Paths = fields[2]
			}
			if len(fields) > 3 && fields[3] != "" {
				opts.Test = fields[3]
			}
			if len(fields) > 4 && fields[4] != "" {
				opts.Gate = fields[4]
			}
			if len(fields) > 5 && fields[5] != "" {
				opts.Tier = fields[5]
			}
			if len(fields) > 6 && fields[6] != "" {
				opts.Shared = fields[6]
			}
			if len(fields) > 7 && fields[7] != "" {
				opts.Needs = fields[7]
			}
		}

		if opts.ID == "" {
			return nil, fmt.Errorf("line %d: missing card id", lineNum+1)
		}
		cards = append(cards, opts)
	}

	return cards, nil
}
