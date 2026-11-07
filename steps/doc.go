// Package steps is layer 3 of nova-sprint: the steps (docs/LAYER-3-STEPS.md).
// Pure Go: no store, clock, bus, git or exec. From one View of the sprint (the
// snapshot plus the indexes the steps read, advanced by Fold), the tick's
// Inputs (Now, the inbox in arrival order, the edge requests in flight, the
// stream heads, when each row was last seen, the last tick's refused ops) and
// an Asker over layer 5's question set, Tick returns one tick's Plan: packed
// ops for layer 1, edge requests for layer 4, answers owed to rows, and the
// carry. Every move is a layer 2 move (package card); a step decides only
// which moves this tick holds, in what order, within what budget.
//
// The model is tla/Steps.tla (tla/README.md, "The steps (layer 3)"); the
// code follows its tick order and cites its invariants by name. The
// refinement mapping from this package's state to the model's variables is
// in trace.go.
package steps
