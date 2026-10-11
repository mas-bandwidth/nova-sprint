---------------------------- MODULE RetireAbsent ----------------------------
\* The tick's automatic retirement of a fleet row whose machine record is gone
\* from the inventory (nova-config), the owner's solution of 2026-10-11:
\* "what you need then is some process in the tick that removes old entries that
\* are no longer in nova-config." The Go action is sprint.TickRetireAbsent
\* (internal/sprint/fleet_retire.go); the fleet table is the row set, the
\* inventory is the record set.
\*
\* A row whose record is GONE and whose working set is empty leaves the fleet;
\* a row whose record is gone but still holds cards is drained first and retired
\* once its set is empty; a row whose record EXISTS but is offline stays down
\* (down is not an error when actually down); a config read that fails retires
\* nothing (never act on a missing read). The broken variant (MCRetireAbsentBroken)
\* retires on a failed read: it takes off a row whose record still exists, a
\* counterexample to RowsHaveRecords.
-----------------------------------------------------------------------------
EXTENDS FiniteSets

CONSTANTS Members,         \* the machines
          BrokenRead       \* TRUE: retire even on a failed read (the broken variant)

VARIABLES row,             \* the fleet rows on the table (up, held or down)
          holds,           \* the rows whose working set is not empty
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

\* The tick reads the inventory.
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

\* A row's working set becomes empty (its cards return); it is never empty again
\* here (a row retired for good).
Empty(m) ==
    /\ m \in holds
    /\ holds' = holds \ {m}
    /\ UNCHANGED <<row, records, read, failed>>

\* RetireAbsent: the tick takes an empty row with no record off the table, only
\* on a read that answered.
RetireAbsent(m) ==
    /\ ~failed \/ BrokenRead
    /\ m \in row
    /\ m \notin read
    /\ m \notin holds
    /\ row' = row \ {m}
    /\ UNCHANGED <<holds, records, read, failed>>

Next ==
    \/ ReadInv
    \/ ReadFails
    \/ \E m \in Members : ConfigDrop(m)
    \/ \E m \in Members : Empty(m)
    \/ \E m \in Members : RetireAbsent(m)

Spec == Init /\ [][Next]_vars /\ WF_vars(Next)

\* No up/held/down row is without a record once its set is empty: a row with no
\* record and no cards is never left on the table. A failed read acts on none.
FleetRowsMatchConfig ==
    ~failed => (\A m \in row : m \notin holds => m \in records)

\* A row on the table always has a record (the broken variant's counterexample).
RowsHaveRecords == \A m \in row : m \in records

\* Under fair ticks and no further config change, an absent record's row is
\* retired.
LiveRetire ==
    \A m \in Members : (m \notin records /\ m \notin holds) ~> (m \notin row)

=============================================================================
