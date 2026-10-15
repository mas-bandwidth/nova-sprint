package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// listedRouteNames parses every ROUTE <name> line `nova-sprint routes` prints.
func listedRouteNames(out string) (all []string, gone map[string]bool) {
	gone = map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "ROUTE ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		name := fields[1]
		all = append(all, name)
		if fields[3] == "gone" || fields[3] == "pinned" {
			gone[name] = true
		}
	}
	return all, gone
}

// Every route name `nova-sprint routes` prints on a ROUTE line is resolved by
// `routes rest`, `routes wake` and `routes limit`, with and without --dry-run,
// including a disabled route; a listed route no longer in the store's route set
// is refused with the gone reason naming `nova-config apply --kind route`,
// never "names no route"; and a mem twin Snapshot/Restore keeps routes across
// commands (tla/RouteRest.tla, ListedIsResolvable).
func TestRoutesRestAndLimitResolveEveryListedRoute(t *testing.T) {
	t.Parallel()
	ta := mercuryApp(t)
	// Deal one card on gone-route, then replace the store's route set so `routes`
	// lists an enabled route, a disabled route (pro-glm53-openrouter), and a gone
	// route from the card that ran on it.
	ta.m.SetRoutes([]sprint.Route{
		{Name: "gone-route", Tier: "flash", Provider: "openrouter", Model: "z-ai/glm-5.3-old", Enabled: true},
	})
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)

	ta.m.SetRoutes([]sprint.Route{
		{Name: "pro-mercury3", Tier: "pro", Provider: "inception", Model: "mercury-3-preview-1002", Enabled: true},
		{Name: "pro-glm53-openrouter", Tier: "pro", Provider: "openrouter", Model: "z-ai/glm-5.3", Enabled: false},
	})

	// Round-trip the mem twin through Snapshot/Restore as `--redis mem:<file>`
	// does between commands: routes must survive across the snapshot.
	doc, err := ta.m.Snapshot()
	require.NoError(t, err)
	restored := store.NewMem()
	require.NoError(t, restored.Restore(doc))
	gotRoutes, _, err := restored.Routes(t.Context())
	require.NoError(t, err)
	assert.Len(t, gotRoutes.Routes, 2, "mem twin Snapshot/Restore keeps routes across commands")

	routesOut := ta.ok("routes")
	names, gone := listedRouteNames(routesOut)
	require.ElementsMatch(t, []string{"pro-mercury3", "pro-glm53-openrouter", "gone-route"}, names, routesOut)
	assert.True(t, gone["gone-route"], "gone-route is listed as gone:\n%s", routesOut)
	assert.False(t, gone["pro-glm53-openrouter"], "pro-glm53-openrouter is in the store's route set:\n%s", routesOut)

	const goneSuffix = " is listed from the cards that ran on it but is not in the sprint's route set: nova-config apply --kind route copies it, or rest its provider"

	for _, name := range names {
		if gone[name] {
			for _, cmd := range []string{
				"routes rest " + name + " --reason r --dry-run",
				"routes rest " + name + " --reason r",
				"routes wake " + name + " --reason r --dry-run",
				"routes wake " + name + " --reason r",
				"routes limit " + name + " --rpm 10 --reason r --dry-run",
				"routes limit " + name + " --rpm 10 --reason r",
			} {
				code, out, errs := ta.do(cmd)
				combined := out + errs
				assert.Equal(t, 1, code, "%s:\n%s", cmd, combined)
				assert.Contains(t, combined, name+goneSuffix, "%s should give the gone refusal:\n%s", cmd, combined)
				assert.NotContains(t, combined, "names no route", "%s must never say names no route:\n%s", cmd, combined)
			}
			continue
		}

		// Store route (enabled or disabled): dry-run and real run resolve it for rest, wake and limit.
		code, dryRest, errs := ta.do("routes rest " + name + " --reason r --dry-run")
		assert.Equal(t, 0, code, "%s%s", dryRest, errs)
		assert.Contains(t, dryRest, "ROUTES-REST OK DRY-RUN changes=1")
		assert.Contains(t, dryRest, "WOULD routes "+name+" rest until woken")

		realRest := ta.ok("routes rest " + name + " --reason r")
		assert.Contains(t, realRest, "MOVED routes "+name+" rest until woken")

		code, dryWake, errs := ta.do("routes wake " + name + " --reason r --dry-run")
		assert.Equal(t, 0, code, "%s%s", dryWake, errs)
		assert.Contains(t, dryWake, "ROUTES-WAKE OK DRY-RUN changes=1")
		assert.Contains(t, dryWake, "WOULD routes "+name+" woken")

		realWake := ta.ok("routes wake " + name + " --reason r")
		assert.Contains(t, realWake, "MOVED routes "+name+" woken")

		code, dryLim, errs := ta.do("routes limit " + name + " --rpm 10 --reason r --dry-run")
		assert.Equal(t, 0, code, "%s%s", dryLim, errs)
		assert.Contains(t, dryLim, "ROUTES-LIMIT OK DRY-RUN changes=1")
		assert.Contains(t, dryLim, "requests per minute 10")

		realLim := ta.ok("routes limit " + name + " --rpm 10 --reason r")
		assert.Contains(t, realLim, "requests per minute 10")
	}

	// An unlisted unknown name still says "names no route".
	code, out, errs := ta.do("routes rest no-such-route --reason r")
	require.Equal(t, 1, code)
	assert.Contains(t, out+errs, "no-such-route names no route and no provider of a route")
}
