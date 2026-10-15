package refmodel_test

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/refmodel"
	"github.com/mas-bandwidth/nova-tools/pkg/hostload"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Routing computes shares in place. Each parallel planner's clone owns its
// route records, including the transient Share that JSON deliberately omits.
func TestParallelCloneRouteSharesStayIsolated(t *testing.T) {
	t.Parallel()
	source := refmodel.Snapshot{Tables: &sprint.Snapshot{Routes: []sprint.Route{{Name: "route", Enabled: true}}}}
	first, second := source.Clone(), source.Clone()
	start := make(chan struct{})
	var done sync.WaitGroup
	for i, clone := range []refmodel.Snapshot{first, second} {
		done.Add(1)
		go func() {
			defer done.Done()
			<-start
			for n := 0; n < 1000; n++ {
				clone.Tables.Routes[0].Share = float64(i + 1)
			}
		}()
	}
	close(start)
	done.Wait()
	assert.Zero(t, source.Tables.Routes[0].Share, "computing a clone's shares leaves its source alone")
	assert.Equal(t, float64(1), first.Tables.Routes[0].Share)
	assert.Equal(t, float64(2), second.Tables.Routes[0].Share)
}

// Every nested mutable input is changed through a clone. Its source keeps the
// same complete metadata, including nil slices and absent pointer values.
func TestCloneIsolatesNestedSnapshotMetadata(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*refmodel.Snapshot){
		"providers": func(s *refmodel.Snapshot) {
			s.Tables.Providers["provider"] = sprint.Provider{Name: "provider", Funded: false}
		},
		"route":                     func(s *refmodel.Snapshot) { s.Tables.Routes[0].Name = "changed" },
		"tier map":                  func(s *refmodel.Snapshot) { s.Tables.Tiers["new"] = []string{"new"} },
		"tier array":                func(s *refmodel.Snapshot) { s.Tables.Tiers["flash"][0] = "changed" },
		"answered":                  func(s *refmodel.Snapshot) { s.Tables.Answered["note"] = "changed" },
		"held":                      func(s *refmodel.Snapshot) { s.Tables.Held["card"] = false },
		"rules":                     func(s *refmodel.Snapshot) { s.Tables.RulesOff[0] = "changed" },
		"reader states":             func(s *refmodel.Snapshot) { s.Tables.ReaderStates["reader"] = "changed" },
		"no room":                   func(s *refmodel.Snapshot) { s.Tables.NoRoom["member"] = "changed" },
		"pronouns":                  func(s *refmodel.Snapshot) { s.Tables.FriendTextPronouns["seat"] = "changed" },
		"queued property":           func(s *refmodel.Snapshot) { s.Tables.Queue[0].Prop.Value = "changed" },
		"queued entry":              func(s *refmodel.Snapshot) { s.Tables.Queue[0].Entry.ID = "changed" },
		"queued set":                func(s *refmodel.Snapshot) { s.Tables.Queue[0].Entry.Set["field"] = "changed" },
		"queued unset":              func(s *refmodel.Snapshot) { s.Tables.Queue[0].Entry.Unset[0] = "changed" },
		"queued create":             func(s *refmodel.Snapshot) { s.Tables.Queue[0].Entry.Create.Row = "changed" },
		"queued move":               func(s *refmodel.Snapshot) { s.Tables.Queue[0].Entry.Move.Row = "changed" },
		"queued score":              func(s *refmodel.Snapshot) { *s.Tables.Queue[0].Entry.Move.Score = 2 },
		"queued expectation":        func(s *refmodel.Snapshot) { s.Tables.Queue[0].Entry.Expect.Revision = "changed" },
		"queued place":              func(s *refmodel.Snapshot) { s.Tables.Queue[0].Entry.Expect.Place.Row = "changed" },
		"queued guards":             func(s *refmodel.Snapshot) { s.Tables.Queue[0].Entry.Expect.Fields["new"] = ntable.FieldGuard{} },
		"queued guard equals":       func(s *refmodel.Snapshot) { *s.Tables.Queue[0].Entry.Expect.Fields["field"].Equals = "changed" },
		"queued guard absent":       func(s *refmodel.Snapshot) { *s.Tables.Queue[0].Entry.Expect.Fields["field"].Absent = true },
		"queued guard alternatives": func(s *refmodel.Snapshot) { s.Tables.Queue[0].Entry.Expect.Fields["field"].OneOf[0] = "changed" },
	}
	for _, seat := range []struct {
		name string
		get  func(*refmodel.Snapshot) *sprint.FriendSeat
	}{
		{"model seat", func(s *refmodel.Snapshot) *sprint.FriendSeat { return &s.Friends[0] }},
		{"table seat", func(s *refmodel.Snapshot) *sprint.FriendSeat { return &s.Tables.Friends[0] }},
	} {
		for name, change := range map[string]func(*sprint.FriendSeat){
			"tiers":   func(f *sprint.FriendSeat) { f.Tiers[0] = "changed" },
			"roles":   func(f *sprint.FriendSeat) { f.Roles[0] = "changed" },
			"streams": func(f *sprint.FriendSeat) { f.Streams[0] = "changed" },
			"kinds":   func(f *sprint.FriendSeat) { f.Kinds[0] = "changed" },
			"running": func(f *sprint.FriendSeat) { f.Running[0] = "changed" },
		} {
			cases[seat.name+" "+name] = func(s *refmodel.Snapshot) { change(seat.get(s)) }
		}
	}
	for _, beat := range []struct {
		name string
		get  func(*refmodel.Snapshot) sprint.Beat
	}{
		{"beat", func(s *refmodel.Snapshot) sprint.Beat { return s.Beats["member"] }},
		{"model seat beat", func(s *refmodel.Snapshot) sprint.Beat { return s.Friends[0].Beat }},
		{"table seat beat", func(s *refmodel.Snapshot) sprint.Beat { return s.Tables.Friends[0].Beat }},
	} {
		for name, change := range map[string]func(sprint.Beat){
			"samples":        func(b sprint.Beat) { b.Samples[0].Pct = 2 },
			"live":           func(b sprint.Beat) { b.Live[0] = "changed" },
			"live seen":      func(b sprint.Beat) { b.LiveSeen["card"] = t0.Add(1) },
			"ticks":          func(b sprint.Beat) { b.Meter.Ticks.Busy = 2 },
			"files":          func(b sprint.Beat) { b.Meter.Files.Open = 2 },
			"holders":        func(b sprint.Beat) { b.Meter.Files.Top[0].Open = 2 },
			"report":         func(b sprint.Beat) { b.Friend.Window = "changed" },
			"report running": func(b sprint.Beat) { b.Friend.Running[0] = "changed" },
			"working":        func(b sprint.Beat) { *b.Friend.Working = 2 },
			"queue":          func(b sprint.Beat) { *b.Friend.Queue = 2 },
			"width":          func(b sprint.Beat) { *b.Friend.Width = 2 },
			"stop returns":   func(b sprint.Beat) { *b.Friend.StopReturns = 2 },
			"paced":          func(b sprint.Beat) { *b.Friend.Paced = 2 },
		} {
			cases[beat.name+" "+name] = func(s *refmodel.Snapshot) { change(beat.get(s)) }
		}
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, expected := metadataSnapshot(), metadataSnapshot()
			clone := source.Clone()
			require.Equal(t, expected, clone, "cloning preserves the input metadata")
			change(&clone)
			assert.Equal(t, expected, source, "changing a clone leaves its source unchanged")
		})
	}
}

// Give every public metadata field a nonzero value so a fresh snapshot cannot
// silently omit an input field added to the sprint snapshot later.
func TestClonePreservesEveryPublicSnapshotField(t *testing.T) {
	t.Parallel()
	source := metadataSnapshot()
	original := reflect.ValueOf(source.Tables).Elem()
	for i := 0; i < original.NumField(); i++ {
		field, info := original.Field(i), original.Type().Field(i)
		if !info.IsExported() || !field.IsZero() {
			continue
		}
		switch field.Kind() {
		case reflect.String:
			field.SetString(info.Name)
		case reflect.Bool:
			field.SetBool(true)
		case reflect.Int, reflect.Int64:
			field.SetInt(int64(i + 1))
		case reflect.Uint64:
			field.SetUint(uint64(i + 1))
		case reflect.Map:
			field.Set(reflect.MakeMap(field.Type()))
		case reflect.Slice:
			field.Set(reflect.MakeSlice(field.Type(), 1, 1))
		case reflect.Pointer:
			field.Set(reflect.New(field.Type().Elem()))
		}
	}
	clone := reflect.ValueOf(source.Clone().Tables).Elem()
	for i := 0; i < original.NumField(); i++ {
		info := original.Type().Field(i)
		if !info.IsExported() {
			assert.True(t, clone.Field(i).IsZero(), "%s is a derived planner cache reset on clone", info.Name)
			continue
		}
		switch info.Name {
		case "Work", "Readers", "Merge", "Fleet":
			continue // existing table properties cover these
		}
		assert.Equal(t, original.Field(i).Interface(), clone.Field(i).Interface(), info.Name)
	}
	zero := refmodel.Snapshot{Tables: &sprint.Snapshot{}}
	assert.Equal(t, zero, zero.Clone(), "absent maps, slices and pointers stay absent")
	assert.Nil(t, (refmodel.Snapshot{}).Clone().Tables)
	blank := refmodel.Snapshot{Beats: map[string]sprint.Beat{"member": {}}, Friends: []sprint.FriendSeat{{}}, Tables: &sprint.Snapshot{Queue: []sprint.QueuedChange{{}}}}
	assert.Equal(t, blank, blank.Clone(), "absent nested readings and queue pointers stay absent")
}

func metadataSnapshot() refmodel.Snapshot {
	values := []int{1, 1, 1, 1, 1}
	equal, absent, score := "value", false, float64(1)
	beat := sprint.Beat{
		Samples: []sprint.LoadSample{{Pct: 1}}, Live: []string{"card"}, LiveSeen: map[string]time.Time{"card": t0},
		Meter:  hostload.State{Ticks: &hostload.Ticks{Busy: 1}, Files: &hostload.Files{Open: 1, Top: []hostload.Holder{{Open: 1}}}},
		Friend: &sprint.FriendReport{Running: []string{"card"}, Working: &values[0], Queue: &values[1], Width: &values[2], StopReturns: &values[3], Paced: &values[4]},
	}
	seat := sprint.FriendSeat{Tiers: []string{"flash"}, Roles: []string{"reader"}, Streams: []string{"stream"}, Kinds: []string{"fix"}, Running: []string{"card"}, Beat: beat}
	return refmodel.Snapshot{Friends: []sprint.FriendSeat{seat}, Beats: map[string]sprint.Beat{"member": beat}, Tables: &sprint.Snapshot{
		Now: t0, Cleared: t0,
		Providers: map[string]sprint.Provider{"provider": {Name: "provider", Funded: true}},
		Routes:    []sprint.Route{{Name: "route", Enabled: true, Share: 0.25}}, Tiers: map[string][]string{"flash": {"route"}},
		Answered: map[string]string{"note": "answer"}, Held: map[string]bool{"card": true}, RulesOff: []string{"rule"},
		ReaderStates: map[string]string{"reader": "up"}, NoRoom: map[string]string{"member": "held"}, FriendTextPronouns: map[string]string{"seat": "they/them"},
		Friends: []sprint.FriendSeat{seat}, Queue: []sprint.QueuedChange{{Prop: &sprint.PropWrite{Value: "value"}, Entry: &ntable.BatchMemberEntry{
			ID: "card", Set: map[string]string{"field": "value"}, Unset: []string{"field"}, Create: &ntable.MemberCreateOp{Row: "stream"}, Move: &ntable.MemberMoveOp{Row: "stream", Score: &score},
			Expect: &ntable.MemberExpect{Revision: "1", Place: &ntable.PlaceExpect{Row: "stream"}, Fields: map[string]ntable.FieldGuard{"field": {Equals: &equal, Absent: &absent, OneOf: []string{"value"}}}},
		}}},
	}}
}
