package swarm

import (
	_ "embed"
)

//go:embed testdata/providers.tsv
var providersTableData string

// providerEntry is one parsed row of the providers table.
type providerEntry struct {
	Name          string
	LinuxHarness  string
	DarwinHarness string
	HarnessArgs   []string
	EnvVar        string
	Model         string
	BaseURL       string
}

// readProvidersTable parses the embedded providers.tsv into a map keyed by
// provider name. It declares the four providers confirmed in docs/MODELS.md
// (one row per provider, both OSes) as the data the one launcher will read.

// header

// DefaultLaunchRow names the providers-table row a route launches with when its provider
// has no row of its own. It is declared in the table like every other row, so a route the
// table does not name still launches through the one launcher and its one argv shape --
// never through a guessed argv or a per-provider script.
const DefaultLaunchRow = "*"

// LaunchRow is the table row a provider launches with: its own row when the table names
// it, and DefaultLaunchRow otherwise. A table that cannot be read answers the provider
// itself, so the caller's LaunchArgvFor reports the read error.

// LaunchRequest is what one run hands the one launcher beyond the table. Every field is
// optional: an empty one takes the table's value (Harness: the row's column for goos;
// Model: the row's model; Title: the provider name; Prompt: the literal "PROMPT.md").
type LaunchRequest struct {
	Harness string // the resolved harness binary the caller runs
	Model   string // the provider/model the caller routed this run to
	Title   string // the run's label, the harness's --title
	Prompt  string // the prompt argument the harness is handed
}

// LaunchArgv returns the argv for launching a provider on the given OS. The argv
// is the harness binary followed by the expanded harness arguments: {model} is
// replaced with the provider's model from the table, {title} with the provider
// name, and {prompt} with the literal "PROMPT.md". Unknown providers return an
// error rather than a guessed argv -- a launcher that guesses is a bespoke
// launcher with extra steps.

// LaunchArgvFor is LaunchArgv for one run: the table's row for provider gives the
// argv shape, and the request's non-empty fields fill it. This is the one launcher:
// `nova-swarm native` builds every harness argv here (cmd/nova-swarm nativeLaunchArgv),
// so the table, not a per-provider script and not a literal in the caller, decides the
// shape a provider is launched with. The table declares OS-specific columns
// (linuxHarness, darwinHarness) and only those differ between the two OSes.

// One pass over each template argument, so a value that itself spells a placeholder
// (a card that mentions {model}) is handed to the harness verbatim.
