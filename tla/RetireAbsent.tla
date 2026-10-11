---------------------------- MODULE RetireAbsent ----------------------------
\* The tick's automatic retirement of a fleet row whose machine record is gone
\* from the inventory (nova-config), the owner's solution of 2026-10-11:
\* "what you need then is some process in the tick that removes old entries that
\* are no longer in nova-config." The Go action is sprint.TickRetireAbsent
\* (internal/sprint/fleet_retire.go), which plans against the row set; the
\* store's tick (internal/sprint/store/tick.go) reads the inventory and deletes
\* the row and its reader once the retire part has taken the row's control card
\* off the table. The fleet table is the row set, the inventory is the record
\* set.
\*
\* A row whose record is GONE and whose working set is empty leaves the fleet;
\* a row whose record is gone but still holds cards is drained first and retired
\* once its set is empty; a row whose record EXISTS but is offline stays down
\* (down is not an error when actually down); a config read that fails retires
\* nothing (never act on a missing read). The broken variant (MCRetireAbsentBroken)
\* retires on a failed read: it takes off a row whose record still exists, a
\* counterexample to RecordsKeepRows.
\*
\* RecordsKeepRows is the safety invariant. The tick cannot keep a per-state
\* "every empty row has a record": the config drops a record asynchronously and
\* the row leaves a tick later, so the state right after ConfigDrop has a row
\* with no record and no cards (the reviewer's finding of 2026-10-11). The
\* matching statement is temporal and is FleetRowsMatchConfig and LiveRetire.
\*-----------------------------------------------------------------------------
EXTENDS FiniteSets

CONSTANTS Members,         \* the machines
          BrokenRead       \* TRUE: retire even on a failed read (the broken variant)

VARIABLES row,             \* the fleet rows on the table (up, held or down)
          holds,           \* the rows whose working or dealt set is not empty
          records,         \* the machines nova-config has a record for
          read,            \* the records the tick last read
          failed           \* TRUE when that read failed

vars == <<row, holds, records, read, failed>>

Init ==
    /\ row = Members
    /\ holds = {}
    /\ records = Members
    /\ read = Members
    /\ failed = FALSE

\* The tick reads the inventory: the records it holds, and that it answered.
ReadInv ==
    /\ read' = records
    /\ failed' = FALSE
    /\ UNCHANGED <<row, holds, records>>

\* A read that fails: the tick read no record (never act on a missing read).
ReadFails ==
    /\ read' = {}
    /\ failed' = TRUE
    /\ UNCHANGED <<row, holds, records>>

\* A machine's record is gone (removed or renamed). Its row stays until the tick
\* retires it.
ConfigDrop(m) ==
    /\ m \in records
    /\ records' = records \ {m}
    /\ UNCHANGED <<row, holds, read, failed>>

\* A card is dealt to a row whose record the inventory still names: its working
\* set is not empty. No new deal reaches a row whose record is gone (the card
\* says "no new deals"): such a row is retired only once Empty, which is the
\* drain case.
Deal(m) ==
    /\ m \in records
    /\ m \in row
    /\ holds' = holds \cup {m}
    /\ UNCHANGED <<row, records, read, failed>>

\* A row's working set becomes empty (its cards return); a row is never made
\* non-empty again here after it is retired.
Empty(m) ==
    /\ m \in holds
    /\ holds' = holds \ {m}
    /\ UNCHANGED <<row, records, read, failed>>

\* RetireAbsent: the tick takes an empty row the answered read did not name off
\* the table. A failed read names no record and is not an answered read: with
\* BrokenRead the broken variant treats it as one and takes a recorded row off.
RetireAbsent(m) ==
    /\ m \in row
    /\ m \notin holds
    /\ m \notin read
    /\ (failed => BrokenRead)
    /\ row' = row \ {m}
    /\ UNCHANGED <<holds, records, read, failed>>

Next ==
    \/ ReadInv
    \/ ReadFails
    \/ \E m \in Members : ConfigDrop(m)
    \/ \E m \in Members : Deal(m)
    \/ \E m \in Members : Empty(m)
    \/ \E m \in Members : RetireAbsent(m)

\* The reads are fair and always answered eventually (WF ReadInv), every
\* draining row's cards return eventually (WF Empty, per row), and the tick
\* retires every enabled empty absent row at least infinitely often (SF
\* RetireAbsent, per row): enough for the row of an absent record to leave in the
\* end (FleetRowsMatchConfig, LiveRetire).
Spec == Init /\ [][Next]_vars
        /\ WF_vars(ReadInv)
        /\ \A m \in Members : WF_vars(Empty(m))
        /\ \A n \in Members : SF_vars(RetireAbsent(n))

\* FleetRowsMatchConfig: no up/held/down row outlives the record that left it.
\* Config changes asynchronously and a row drains over ticks, so this is a
\* leads-to, not a per-state invariant: an absent record's row is retired once
\* its working set is empty. The state right after ConfigDrop is not a
\* counterexample: there the row is still pending its tick.
FleetRowsMatchConfig ==
    \A m \in Members : (m \notin records /\ m \notin holds) ~> (m \notin row)

\* RecordsKeepRows: a machine whose record is present keeps its row, always. The
\* tick never retires what the config still names. A state invariant; the broken
\* model (BrokenRead) falsifies it by retiring a recorded row on a failed read.
RecordsKeepRows ==
    \A m \in Members : (m \in records) => (m \in row)

\* LiveRetire: an absent record's row is retired, a still-draining one included,
\* under the tick's fair reads and the row's eventual drain.
LiveRetire ==
    \A m \in Members : (m \notin records) ~> (m \notin row)

=============================================================================
