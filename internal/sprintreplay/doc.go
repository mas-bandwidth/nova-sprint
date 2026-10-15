// Package sprintreplay is an offline replay of one sprint epoch: it reads the epoch's log
// (docs/SPEC-SPRINT.md, the log: one sprint.Line JSON object per line) and the attempts table,
// orders them by At then by position, and steps them through the sprint's own rule code
// (sprint.ReplayOnto for places, sprint.FailureClass for failure classes, sprint.ClassifyEnd
// for the attempt-end classifier, sprint.BriefDefectOf for brief defects). It takes no clock:
// every decision is a function of the lines at or before a given At, and the same input gives
// byte-identical records.
//
// On those records it searches, on the first 60% of attempts by deal time only, for the
// earliest signal that an attempt will fail, and judges it on the last 40%, never seen while
// choosing (signal.go). Predict is the chosen signal as a pure function, for nova-sprint check
// or the deal step to call later; nothing here is wired into either.
package sprintreplay
