package sprint

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/member"
)

// serverOnlyPacketFields is the json field name of internal/sprint.Packet that
// no member reads: each names the reason a member never reads it. The server
// writes the field, and it must be declared here or the member's decode would
// drop it silently.
var serverOnlyPacketFields = map[string]string{
	"need_heads": "the server records it; no member reads it yet (a member-side refusal would be a nova-tools card)",
	"read_job":   "a friend's read job, read by cmd/nova-sprint's friend cards path, never by a member",
}

func packetJSONFieldNames(t *testing.T, typ reflect.Type) []string {
	t.Helper()
	var names []string
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestEveryPacketFieldReachesTheMember holds internal/sprint.Packet's json
// fields (the server's `queue --json` and `take --json`) equal to nova-tools
// pkg/member.Packet's (the member's decode) plus serverOnlyPacketFields: every
// field the member declares is sent, and every field the server sends is either
// member by the member or named server-only with a reason.
func TestEveryPacketFieldReachesTheMember(t *testing.T) {
	t.Parallel()

	server := packetJSONFieldNames(t, reflect.TypeOf(Packet{}))
	memberFields := packetJSONFieldNames(t, reflect.TypeOf(member.Packet{}))

	serverSet := map[string]bool{}
	for _, f := range server {
		serverSet[f] = true
	}
	memberSet := map[string]bool{}
	for _, f := range memberFields {
		memberSet[f] = true
	}

	for name, reason := range serverOnlyPacketFields {
		if !serverSet[name] {
			t.Errorf("serverOnlyPacketFields names %q but the server does not send it", name)
		}
		if reason == "" {
			t.Errorf("serverOnlyPacketFields[%q] has an empty reason", name)
		}
	}

	for _, f := range memberFields {
		if !serverSet[f] {
			t.Errorf("member field %q is not sent by the server", f)
		}
		if reason, ok := serverOnlyPacketFields[f]; ok {
			t.Errorf("member field %q is server-only but the member reads it (%s)", f, reason)
		}
	}

	for _, f := range server {
		if memberSet[f] {
			continue
		}
		if _, ok := serverOnlyPacketFields[f]; !ok {
			t.Errorf("server field %q is not read by any member and is not in serverOnlyPacketFields", f)
		}
	}
}
