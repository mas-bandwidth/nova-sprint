package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnitPackages(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		module  string
		changed []string
		want    []string
	}{
		{name: "shared package source", changed: []string{"pkg/hygiene/data.go"}, want: []string{"..."}},
		{name: "shared internal source", changed: []string{"internal/sprint/store.go"}, want: []string{"..."}},
		{name: "deleted key shapes", changed: []string{"pkg/hygiene/keyshapes.txt"}, want: []string{"..."}},
		{name: "deleted kinds", changed: []string{"pkg/hygiene/kinds.txt"}, want: []string{"..."}},
		{name: "deleted stray rules", changed: []string{"pkg/hygiene/stray.txt"}, want: []string{"..."}},
		{name: "other shared asset", changed: []string{"internal/sprintdash/page/app.js"}, want: []string{"..."}},
		{name: "whole tree replaces earlier scope", changed: []string{"cmd/nova-sprint/main.go", "pkg/hygiene/data.go"}, want: []string{"..."}},
		{name: "whole tree precedes later scope", changed: []string{"pkg/hygiene/data.go", "cmd/nova-sprint/main.go"}, want: []string{"..."}},
		{name: "package test", changed: []string{"pkg/hygiene/data_test.go"}, want: []string{"pkg/hygiene"}},
		{name: "internal test", changed: []string{"internal/sprint/store_test.go"}, want: []string{"cmd/nova-sprint", "internal/sprint"}},
		{name: "command source", changed: []string{"cmd/nova-sprint/main.go"}, want: []string{"cmd/nova-sprint", "internal/sprint"}},
		{name: "other source", changed: []string{"tools/seed/seed.go"}, want: []string{"tools/seed"}},
		{name: "markdown", changed: []string{"README.md", "pkg/hygiene/README.md"}},
		{name: "documentation directory", changed: []string{"internal/sprint/docs/notes.txt"}},
		{name: "deduplicated and sorted", changed: []string{"pkg/z/z_test.go", "pkg/a/a_test.go", "pkg/z/other_test.go"}, want: []string{"pkg/a", "pkg/z"}},
		{name: "test data", changed: []string{"internal/sprint/testdata/file.txt", "internal/sprint/testdata/input.go"}, want: []string{"cmd/nova-sprint", "internal/sprint"}},
		{name: "other module", module: "github.com/mas-bandwidth/nova-tools", changed: []string{"pkg/member/tree.go"}, want: []string{"pkg/member"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			module := tc.module
			if module == "" {
				module = "github.com/mas-bandwidth/nova-sprint"
			}
			assert.Equal(t, tc.want, UnitPackages(module, tc.changed))
		})
	}
}
