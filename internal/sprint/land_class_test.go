package sprint

import (
	"slices"
	"testing"
)

func TestUnitPackages(t *testing.T) {
	t.Run("nova-sprint internal/sprint file", func(t *testing.T) {
		changed := []string{"internal/sprint/store.go"}
		pkgs := UnitPackages("github.com/mas-bandwidth/nova-sprint", changed)
		expected := []string{"cmd/nova-sprint", "internal/sprint"}
		if !slices.Equal(pkgs, expected) {
			t.Errorf("got %v, want %v", pkgs, expected)
		}
	})

	t.Run("nova-sprint cmd/nova-sprint file", func(t *testing.T) {
		changed := []string{"cmd/nova-sprint/main.go"}
		pkgs := UnitPackages("github.com/mas-bandwidth/nova-sprint", changed)
		expected := []string{"cmd/nova-sprint", "internal/sprint"}
		if !slices.Equal(pkgs, expected) {
			t.Errorf("got %v, want %v", pkgs, expected)
		}
	})

	t.Run("nova-sprint other file", func(t *testing.T) {
		changed := []string{"tools/seed/seed.go"}
		pkgs := UnitPackages("github.com/mas-bandwidth/nova-sprint", changed)
		expected := []string{"tools/seed"}
		if !slices.Equal(pkgs, expected) {
			t.Errorf("got %v, want %v", pkgs, expected)
		}
	})

	t.Run("nova-sprint docs file only", func(t *testing.T) {
		changed := []string{"README.md"}
		pkgs := UnitPackages("github.com/mas-bandwidth/nova-sprint", changed)
		if len(pkgs) != 0 {
			t.Errorf("got %v, want empty", pkgs)
		}
	})

	t.Run("nova-tools module", func(t *testing.T) {
		changed := []string{"pkg/member/tree.go"}
		pkgs := UnitPackages("github.com/mas-bandwidth/nova-tools", changed)
		expected := []string{"pkg/member"}
		if !slices.Equal(pkgs, expected) {
			t.Errorf("got %v, want %v", pkgs, expected)
		}
	})

	t.Run("deduplicated and sorted", func(t *testing.T) {
		changed := []string{"internal/sprint/a.go", "internal/sprint/b.go", "internal/sprint/c.go"}
		pkgs := UnitPackages("github.com/mas-bandwidth/nova-sprint", changed)
		expected := []string{"cmd/nova-sprint", "internal/sprint"}
		if !slices.Equal(pkgs, expected) {
			t.Errorf("got %v, want %v", pkgs, expected)
		}
	})

	t.Run("testdata directory", func(t *testing.T) {
		changed := []string{"internal/sprint/testdata/file.txt"}
		pkgs := UnitPackages("github.com/mas-bandwidth/nova-sprint", changed)
		expected := []string{"cmd/nova-sprint", "internal/sprint"}
		if !slices.Equal(pkgs, expected) {
			t.Errorf("got %v, want %v", pkgs, expected)
		}
	})
}
