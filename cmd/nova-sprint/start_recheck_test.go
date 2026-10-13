package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

func TestStartPrintsEveryObsoleteJudgmentClosure(t *testing.T) {
	t.Parallel()
	for _, asJSON := range []bool{false, true} {
		t.Run(map[bool]string{false: "lines", true: "json"}[asJSON], func(t *testing.T) {
			t.Parallel()
			ta := newTestApp(t)
			ta.ok("init --members m1 --readers reader-a")
			ta.m.SetRoutes([]sprint.Route{{Name: "flash", Tier: "flash", Provider: "funded", Enabled: true, Model: "fixture", Tokens: 1000, Deadline: 600}})
			ta.m.SetProviders(map[string]sprint.Provider{"funded": {Funded: true}})
			ta.ok("add --stream s1 --count 1 --one")
			st := &store.Store{B: ta.m, Actor: "coordinator", Now: ta.a.now, NewID: store.NewID}
			_, err := st.Run(context.Background(), store.Step{Verb: "fixture judgment", Load: store.All, Plan: func(s *sprint.Snapshot) sprint.Plan {
				return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Judgment, Type: sprint.NWorkFailed, Primaries: []string{"absent"}, At: s.Now}}}
			}})
			require.NoError(t, err)
			args := "start"
			if asJSON {
				args += " --json"
			}
			code, out, errs := ta.do(args)
			require.Equal(t, 0, code, out+errs)
			if asJSON {
				var result struct {
					Closed []string `json:"closed"`
				}
				require.NoError(t, json.Unmarshal([]byte(out), &result))
				require.Len(t, result.Closed, 1)
				assert.Contains(t, result.Closed[0], "on absent")
			} else {
				assert.Contains(t, out, "START NOTE closed judgment")
				assert.Contains(t, out, "on absent")
			}
		})
	}
}
