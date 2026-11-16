package card

import (
	"testing"
)

func TestResourcesParsingAndNeeds(t *testing.T) {
	t.Parallel()
	def := DefaultReadNeeds()
	if def.Amounts["cpu"] != 1 || def.Amounts["memory"] != 1 {
		t.Fatalf("DefaultReadNeeds = %+v", def)
	}

	brief := "tier: pro\nRESOURCES: cpu 4, memory 8, mac\n\nTask\n"
	raw, count, err := ParseBriefNeeds(brief)
	if err != nil || count != 1 || raw != "cpu 4, memory 8, mac" {
		t.Fatalf("ParseBriefNeeds = (%q, %d, %v)", raw, count, err)
	}

	n, err := ParseNeeds(raw, map[string]bool{"mac": true}, nil)
	if err != nil || n.Amounts["cpu"] != 4 || n.Amounts["memory"] != 8 || len(n.Tags) != 1 || n.Tags[0] != "mac" {
		t.Fatalf("ParseNeeds = (%+v, %v)", n, err)
	}

	cn := CardNeeds(brief)
	if cn.Amounts["cpu"] != 4 || cn.Amounts["memory"] != 8 {
		t.Fatalf("CardNeeds = %+v", cn)
	}

	rn := ResolveNeeds("read", brief, nil, nil)
	if rn.Amounts["cpu"] != 1 || len(rn.Tags) != 0 {
		t.Fatalf("ResolveNeeds(read) = %+v", rn)
	}

	wn := ResolveNeeds("work", brief, map[string]bool{"mac": true}, nil)
	if wn.Amounts["cpu"] != 4 {
		t.Fatalf("ResolveNeeds(work) = %+v", wn)
	}

	retiredBrief := "tier: pro\nNEEDS-ROUTE: mac\n\nTask\n"
	if _, _, err := ParseBriefNeeds(retiredBrief); err == nil {
		t.Fatal("expected error on retired NEEDS-ROUTE")
	}

	dupBrief := "tier: pro\nRESOURCES: cpu 1\nRESOURCES: cpu 2\n\nTask\n"
	if _, _, err := ParseBriefNeeds(dupBrief); err == nil {
		t.Fatal("expected error on duplicate RESOURCES")
	}
}
