package run

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseWho(t *testing.T) {
	cases := []struct {
		input string
		want  []string
		bad   bool
	}{
		{"WHO OK present=0\n", []string{}, false},
		{"WHO OK present=2\nalice\t1m\nbob\t2s\n", []string{"alice", "bob"}, false},
		{"WHO OK present=1\n", nil, true},
		{"WHO OK present=2\nalice\t1s\nalice\t2s\n", nil, true},
		{"WHO OK present=1\nbad name\t1s\n", nil, true},
	}
	for _, tc := range cases {
		got, err := parseWho(tc.input)
		if (err != nil) != tc.bad || !tc.bad && !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("input=%q got=%v err=%v", tc.input, got, err)
		}
	}
}
func TestBusEnvironmentContainsOnlyBusConfiguration(t *testing.T) {
	b, err := NewNovaBus(BusConfig{Binary: "/test/nova-bus", Redis: "127.0.0.1:1", Password: "test-secret", ReplyExecutable: "/test/nova-sprint"})
	if err != nil {
		t.Fatal(err)
	}
	env := strings.Join(b.env(), "\n")
	if !strings.Contains(env, "NOVA_BUS_PASSWORD=test-secret") || strings.Contains(env, "NOVA_PG_") {
		t.Fatalf("environment does not contain the intended keys")
	}
}
