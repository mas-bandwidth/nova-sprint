package dash

import (
	"context"
	"strings"
	"testing"
)

func TestSprintDSNRejectsEmbeddedCredentials(t *testing.T) {
	got, err := SprintDSN("postgres://nova_config@localhost/nova?sslmode=disable")
	if err != nil || got != "postgres://nova_sprint@localhost/nova?sslmode=disable" {
		t.Fatalf("dsn=%q err=%v", got, err)
	}
	for _, raw := range []string{"postgres://nova:secret@localhost/nova", "postgres://localhost/nova?password=secret", "postgres://localhost/nova?user=other"} {
		if _, err := SprintDSN(raw); err == nil {
			t.Errorf("accepted credential-bearing DSN")
		}
	}
}
func TestDashboardRequiresExplicitSprintCredential(t *testing.T) {
	t.Setenv("NOVA_SPRINT_PG_PASSWORD", "")
	t.Setenv("NOVA_PG_PASSWORD", "ambient")
	err := Command(context.Background(), []string{"--pg", "postgres://nova@localhost/nova"})
	if err == nil || !strings.Contains(err.Error(), "NOVA_SPRINT_PG_PASSWORD") {
		t.Fatalf("error=%v", err)
	}
}
