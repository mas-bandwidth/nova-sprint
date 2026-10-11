package sprint

// PropReadDealIndex is the fleet table's persisted round-robin cursor for reads at a tier.
// The table property is epoch-scoped, so a restart keeps its place and clear starts the next
// epoch at the first reader (docs/SPEC-SPRINT.md section 1, the read-card deal).
func PropReadDealIndex(tier string) string { return "read_deal_index_" + tier }

// readDealRound is the cursor over units that may receive reads (docs/SPEC-SPRINT.md
// section 1; README.md, free capacity gets another ready card). Its tier-specific fleet
// property advances atomically with the read cards, and ineligible or full units are skipped.
func readDealRound(s *Snapshot, tier string, units []readUnit) *round {
	names := make([]string, 0, len(units))
	for _, u := range units {
		names = append(names, u.name)
	}
	return tableRound(s.Fleet, PropReadDealIndex(tier), names)
}
