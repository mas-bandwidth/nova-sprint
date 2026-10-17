package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAHeldPrimaryWaitLineNamesTheHold(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		l    Line
		want string
		not  string
	}{
		{
			name: "a server-default hold names who and the judgment",
			l: Line{Card: "s1-1", Stream: "s1", Table: Work, From: "s1:" + string(Ready), To: "s1:" + string(Waiting), Actor: MachineActor, Set: map[string]string{FieldHeld: "2030-01-02T03:04:05Z", FieldHeldBy: ServerDefaultActor, "held_judgment": "j1", "decision_held_from": "ready"}},
			want: "s1-1 waits: held by server-default for judgment j1",
			not:  "sentinel",
		},
		{
			name: "a hold with no held_by names the actor",
			l:    Line{Card: "s1-1", Stream: "s1", Table: Work, From: "s1:" + string(Ready), To: "s1:" + string(Waiting), Actor: "coord", Set: map[string]string{FieldHeld: "2030-01-02T03:04:05Z"}},
			want: "s1-1 waits: held by coord",
		},
		{
			name: "a wait on needs names them",
			l:    Line{Card: "s1-1", Stream: "s1", Table: Work, From: "s1:" + string(Ready), To: "s1:" + string(Waiting), Actor: MachineActor, Set: map[string]string{"needs": "s1-0"}},
			want: "s1-1 waits on what it needs: s1-0",
		},
		{
			name: "a wait with no hold or need is the sentinel insert",
			l:    Line{Card: "s1-1", Stream: "s1", Table: Work, From: "s1:" + string(Ready), To: "s1:" + string(Waiting), Actor: MachineActor},
			want: "s1-1 waits: a sentinel was put in front of it by the machine",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Render(tc.l)
			assert.Equal(t, tc.want, got)
			if tc.not != "" {
				assert.NotContains(t, got, tc.not)
			}
		})
	}
}
