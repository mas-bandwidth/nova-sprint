package sprint

// Wrappers for the offline replay (internal/sprintreplay): the rule code the replay steps
// an epoch's log through, exported as it is and never copied. No rule logic lives here.

// ClassifyEnd is how an attempt ended (rules.go classifyEnd, the attempt-end classifier the
// brief-defect rule and the brief bound read): "harness" when its worker never reported
// (ran is false: FieldRanAttempts says no), "brief" when the report names a brief defect
// (BriefDefectOf), else "worker".
func ClassifyEnd(ran bool, report string) string { return string(classifyEnd(ran, report)) }

// RanAttempt is attempt n's record in a primary's FieldRanAttempts (attempts.go ranAttempts,
// the record the lane-broken bound and the brief bound read): whether its worker ran and
// reported, the fault class it ended on when it did not, and whether the field records it.
func RanAttempt(field string, n int) (ran bool, fault string, ok bool) {
	r, ok := ranAttempts(&Card{Fields: map[string]string{FieldRanAttempts: field}})[n]
	return r.ran, r.fault, ok
}
