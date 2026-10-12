package hygiene

import (
	"os"
	"strings"
	"testing"
)

func TestKindDataIsEmbedded(t *testing.T) {
	if strings.TrimSpace(kindData) == "" {
		t.Fatal("kindData is empty: the //go:embed kinds.txt directive is missing above it")
	}
}

func TestKindsDeclareEveryKindInKindsTxt(t *testing.T) {
	raw, err := os.ReadFile("kinds.txt")
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, name := range Kinds() {
		declared[name] = true
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		name := strings.TrimSpace(fields[0])
		if name == "" {
			continue
		}
		if !declared[name] {
			t.Errorf("kind %q is in kinds.txt but is not declared; declared: %v", name, Kinds())
		}
	}
}
