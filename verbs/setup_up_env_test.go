package verbs

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// upEnvWith is a machine named studio that has every need of UpNeeds
// except the one named without.
func upEnvWith(without string) UpEnv {
	vars := map[string]string{"NOVA_SECRETS_STORE": "/s/store", "NOVA_SECRETS_KEY": "/s/studio.key", "NOVA_SECRETS_PINS": "/s/pins", "NOVA_SECRETS_SEAT": "studio", "NOVA_PG_DSN": "postgres://nova_config@db:5432/nova", "NOVA_BUS_REDIS": "db:6379"}
	delete(vars, without)
	return UpEnv{Host: "studio", Env: func(n string) string { return vars[n] },
		OnPath: func(p string) bool { return without != "nova-tools" || p != "nova-bus" },
		Exists: func(p string) bool { return strings.HasPrefix(p, "/s/") }}
}

// up refuses each missing need by its name, with what it is and the line
// that sets it (one table: UpNeeds), and nothing when all are there.
func TestUpRefusalsNameEveryPrerequisiteWithACommand(t *testing.T) {
	t.Parallel()
	if err := UpNeedRefusal("v2.0.3", upEnvWith("")); err != nil {
		t.Fatal(err)
	}
	for _, n := range UpNeeds("v2.0.3") {
		err := UpNeedRefusal("v2.0.3", upEnvWith(n.Name))
		var op *OperationError
		if !errors.As(err, &op) || op.Fix != n.Fix || !strings.Contains(op.Problem, n.What) || n.Fix == "" {
			t.Errorf("%s missing: %v", n.Name, err)
			continue
		}
		name := n.Name
		if name == "nova-tools" {
			name = "nova-bus"
		}
		if !strings.HasPrefix(op.Problem, name+" ") {
			t.Errorf("%s missing: the refusal does not open with its name: %q", n.Name, op.Problem)
		}
	}
	other := upEnvWith("")
	other.Host = "amd-1"
	if err := UpNeedRefusal("v2.0.3", other); err == nil || !strings.Contains(err.Error(), `NOVA_SECRETS_SEAT is "studio", not this machine's name "amd-1"`) {
		t.Fatal(err)
	}
}

// nova-fleet check's missing names reach up and check as names, never
// redacted (a name is not a value).
func TestFleetMissingNamesAreNotRedacted(t *testing.T) {
	t.Parallel()
	out := []byte("CHECK OK go 1.26\nCHECK MISSING TAILSCALE_API_KEY: nova-secrets set TAILSCALE_API_KEY\nCHECK MISSING ansible: python3 -m venv v\n")
	if got := FleetMissing(out); got != "missing: TAILSCALE_API_KEY, ansible" {
		t.Fatal(got)
	}
	if got := FleetMissing([]byte("CHECK OK missing=0\n")); got != "" {
		t.Fatal(got)
	}
}

// docs/SETUP.md section 0 is UpNeeds rendered, and section 3 names exactly
// the services up offers: the doc cannot drift from the refusals.
func TestSetupDocMatchesUpNeeds(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile("../docs/SETUP.md")
	if err != nil {
		t.Fatal(err)
	}
	var want strings.Builder
	want.WriteString("| Needs | What it is | The line that sets it |\n|---|---|---|\n")
	for _, n := range UpNeeds("") {
		want.WriteString("| `" + n.Name + "` | " + n.What + " | `" + n.Fix + "` |\n")
	}
	if !strings.Contains(string(doc), want.String()) {
		t.Errorf("docs/SETUP.md section 0 is not UpNeeds; paste:\n%s", want.String())
	}
	services := "`nova-sprint up --services` accepts `" + strings.Join(SetupServiceNames(), "`, `") + "`, or `none`."
	if !strings.Contains(string(doc), services) {
		t.Errorf("docs/SETUP.md section 3 does not name the services up offers; want the line:\n%s", services)
	}
}

// help up prints the same table up's refusals use, with this build's pin.
func TestHelpUpPrintsUpNeeds(t *testing.T) {
	t.Parallel()
	r := (&Engine{ToolPin: "v2.0.3"}).Execute(context.Background(), []string{"help", "up"})
	for _, n := range UpNeeds("v2.0.3") {
		if !strings.Contains(r.Output, "  "+n.Name+": "+n.What+"; "+n.Fix) {
			t.Fatalf("help up lacks %s:\n%s", n.Name, r.Output)
		}
	}
}
