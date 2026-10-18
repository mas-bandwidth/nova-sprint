REPO: mas-bandwidth/nova-sprint
BASE: sprint/v17
PATHS: cmd/nova-sprint/landgo.go,cmd/nova-sprint/land_gate_budget_test.go

THE TASK (the design is decided; follow it; a prototype of it passed the DONE-WHEN tests on a bench).
(1) In landgo.go add `const landGateRunBudget = 5 * time.Minute` beside landGoBudget, its comment: the bound of one tree-gate run on a bench (2026-10-17: a run sat 14 minutes on vision; CI's own bound is two minutes, so five is generous), and that a run past it is the bench's fault, never the tree's (tla/GateRing.tla (read) AskRefused).
(2) In gateOn: budget := time.Duration(len(runs)) * landGateRunBudget; gctx, cancel := context.WithTimeout(ctx, budget); run runOnBench on gctx.

STEP 1. (the rest of the brief is cut from this fixture)
