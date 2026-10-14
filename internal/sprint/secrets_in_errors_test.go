package sprint_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/refmodel"
	sprintstore "github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/internal/testkit/repotree"
	"github.com/mas-bandwidth/nova-sprint/pkg/secretcheck"
)

// THE CLASS RULE: NO SECRET REACHES AN ERROR (docs/SPEC-CI.md, `secrets-never-in-errors`),
// over nova-sprint's own paths. internal/ci's TestNoSecretReachesAnError holds it over the
// rest of the tree and leaves these out; this is the same rule with the same harness
// (internal/secretcheck), its table and allowlist the rows that were internal/ci's for
// these paths.

// sprintSecretRoots are nova-sprint's paths the walk reads, from the repository's root.
var sprintSecretRoots = []string{
	"cmd/nova-sprint", "cmd/nova-card", "cmd/nova-work",
	"internal/sprint", "internal/sprintdash", "internal/card", "internal/cardgen",
	"internal/workfile", "internal/workgh", "internal/worklang",
}

// sprintSecretOpeners is the reviewed table of every function the rule drives here, keyed
// `<repo-relative package directory>.<Name>`.
var sprintSecretOpeners = map[string]any{
	"internal/cardgen.ParseFindings":        cardgen.ParseFindings,
	"internal/cardgen.ParseLedger":          cardgen.ParseLedger,
	"internal/sprint.NewTable":              sprint.NewTable,
	"internal/sprint.OpenKey":               sprint.OpenKey,
	"internal/sprint.ParseAttempts":         sprint.ParseAttempts,
	"internal/sprint.ParseDeadline":         sprint.ParseDeadline,
	"internal/sprint.ParseFriendReadReport": sprint.ParseFriendReadReport,
	"internal/sprint.ParseLaneCap":          sprint.ParseLaneCap,
	"internal/sprint.ParseLive":             sprint.ParseLive,
	"internal/sprint.ParseMembers":          sprint.ParseMembers,
	"internal/sprint.ParseReadCard":         sprint.ParseReadCard,
	"internal/sprint.ParseReaderTiers":      sprint.ParseReaderTiers,
	"internal/sprint.ParseRoute":            sprint.ParseRoute,
	"internal/sprint.ParseSiblings":         sprint.ParseSiblings,
	"internal/sprint.ParseWidth":            sprint.ParseWidth,
	"internal/sprint.ParseWorkCard":         sprint.ParseWorkCard,
	"internal/sprint/refmodel.New":          refmodel.New,
	"internal/sprint/store.NewDeliverer":    sprintstore.NewDeliverer,
}

// sprintSecretLeakAllowlist are the functions known to carry a secret-shaped string into
// an error, one `<key> <reason>` per line. It only shrinks.
const sprintSecretLeakAllowlist = sprintNovaToolsLeak + `
internal/sprint.ParseAttempts echoes the rejected value with %q (internal/sprint/brief_bound.go:83)
internal/sprint.ParseDeadline echoes the rejected value with %q (internal/sprint/deadline.go:165)
internal/sprint.ParseMembers echoes the rejected width with %q (internal/sprint/width.go:42)
internal/sprint.ParseReaderTiers echoes the rejected tier names (internal/sprint/reader_tiers.go:51)
internal/sprint.ParseRoute echoes the rejected route with %q (internal/sprint/remind.go:108)
internal/sprint.ParseWidth echoes the rejected width with %q (internal/sprint/width.go:42)
internal/sprint/store.NewDeliverer echoes the rejected route with %q (internal/sprint/store/remind.go:147 returns the error of internal/sprint/remind.go:108; the bus refusal at remind.go:155 echoes it too)
`

// TestNoSecretReachesAnErrorInTheSprint is the class rule over nova-sprint's paths.
func TestNoSecretReachesAnErrorInTheSprint(t *testing.T) {
	t.Parallel()
	parsed := repotree.Parsed(t, repotree.Query{
		Under:      sprintSecretRoots,
		Ext:        ".go",
		SkipSuffix: []string{"_test.go"},
	})
	files := make([]secretcheck.File, len(parsed))
	for i, pf := range parsed {
		files[i] = secretcheck.File{
			Rel:      pf.Path,
			AST:      pf.AST,
			Testdata: strings.Contains("/"+pf.Path, "/testdata/"),
		}
	}
	found := secretcheck.OpenersIn(files)
	require.NotEmpty(t, found, "the walk found no Open*/Parse*/Dial*/New* function; a rule that checks nothing passes")
	assert.Empty(t, secretcheck.TableFindings(found, sprintSecretOpeners, nil), "a table row names a function the sprint no longer has")
	allowed, bad := secretcheck.ParseAllowlist(sprintSecretLeakAllowlist)
	assert.Empty(t, bad, "an allowlist line is malformed")
	assert.Empty(t, secretcheck.Verdict(secretcheck.LeakFindings(sprintSecretOpeners, secretcheck.Shapes), allowed), "a secret reaches an error")
	assert.True(t, found["internal/sprint.ParseRoute"], "the walk reads the sprint's own package")
}
