package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Unit coverage for the width text (internal/sprint/width.go): WidthText, the
// fleet table's width cell, the config width as nova-config wrote it. All
// pure: hand-made cards, no store, clock, sleep, subprocess, network or live
// service.

// widthCoverCtl is a member's fleet row cell holding the width field it names
// (empty when it names none).
func widthCoverCtl(member, width string) *Card {
	c := &Card{ID: CtlID(member), Row: member, Col: Ctl, Fields: map[string]string{}}
	if width != "" {
		c.Fields[FieldWidth] = width
	}
	return c
}

// TestWidthCoverWidthTextRendersTheConfigWidth pins WidthText: the width cell
// as nova-config wrote it; a member whose width is not in nova-config renders
// empty, never a default nobody configured.
func TestWidthCoverWidthTextRendersTheConfigWidth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		ctl  *Card
		want string
	}{
		{name: "main: a width of 8 renders 8", ctl: widthCoverCtl("m1", "8"), want: "8"},
		{name: "main: the drain renders 0", ctl: widthCoverCtl("m2", DrainWidth), want: DrainWidth},
		{name: "refusal: no width renders empty, never a default", ctl: widthCoverCtl("m3", ""), want: ""},
		{name: "refusal: no row at all renders empty", ctl: nil, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, WidthText(tc.ctl))
		})
	}
}
