package sprint_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A repository-declared shared registry (.nova/always-allowed) puts its entries inside
// every card's PATHS by rule (cardgen.SharedRegistries; docs/SPEC-LANDING.md,
// docs/SPEC-SPRINT.md section 7): a diff that changes a listed file outside PATHS passes
// E12 when the list names it, and is refused when the list is nil (the reversed witness,
// run in the same test).
func TestE12TakesARepoDeclaredSharedRegistry(t *testing.T) {
	t.Parallel()
	paths := []string{"internal/a/*.go"}
	diff := "diff --git a/internal/a/a.go b/internal/a/a.go\n" +
		"diff --git a/make/negative-controls.json b/make/negative-controls.json\n"

	amended, refused := sprint.LandScope(paths, diff, nil, "make/negative-controls.json")
	assert.Empty(t, refused, "the registry's file passes E12")
	assert.Empty(t, amended, "inside PATHS by rule is not a scope amendment")

	_, refusedNil := sprint.LandScope(paths, diff, nil)
	assert.Equal(t, []string{"make/negative-controls.json"}, refusedNil, "without the registry the same file is refused")
}

// The registry is read at the base, not the head: a head that adds its own stray file to
// .nova/always-allowed is still refused for that file (the base does not name it) and for
// the registry edit itself, which is never exempt.
func TestE12RegistryIsReadAtTheBaseNotTheHead(t *testing.T) {
	t.Parallel()
	paths := []string{"internal/a/*.go"}
	diff := "diff --git a/.nova/always-allowed b/.nova/always-allowed\n" +
		"diff --git a/tools/stray.txt b/tools/stray.txt\n"
	_, refused := sprint.LandScope(paths, diff, nil, "make/negative-controls.json")
	assert.ElementsMatch(t, []string{".nova/always-allowed", "tools/stray.txt"}, refused)
}

// A non-registry source file outside PATHS is still refused beside an allowed registry
// file: the registry names only its own entries.
func TestE12RegistryStillRefusesOtherSource(t *testing.T) {
	t.Parallel()
	paths := []string{"internal/a/*.go"}
	diff := "diff --git a/make/negative-controls.json b/make/negative-controls.json\n" +
		"diff --git a/internal/b/b.go b/internal/b/b.go\n"
	_, refused := sprint.LandScope(paths, diff, nil, "make/negative-controls.json")
	assert.Equal(t, []string{"internal/b/b.go"}, refused)
}
