---------------------------- MODULE WorkImport ----------------------------
\* nova-work v1: the states of an issue under import and of a repository's
\* sync, from docs/SPEC-WORK-V1.md section 1 (the tree, layer 1) and
\* section 1.7 (the modes above it, specified here and not built: the
\* destructive close after import, the export that re-opens). The receipt
\* variable and Close/Export are design-only: verify only prints facts
\* (cmd/nova-work/verify.go), so nothing the built verbs do reads the receipt
\* or closes; they are held here for the destructive modes, which are
\* specified but not built.
\*
\* An issue's contents are abstracted to a version number: GitHub's copy
\* moves on every outside edit (ExternalEdit), the tree holds the version it
\* fetched. Equal versions stand for SPEC-WORK-V1's field-for-field equality
\* (cmd/nova-work verify, internal/workfile.Diff); the model does not look
\* inside an issue.
\*
\* Repositories: Repos partitions Issues (every issue lives in exactly one
\* repository). An import and a verify each take a scope, a non-empty subset
\* of Repos (nova-work import --repo / verify --repo; a whole-organization
\* run is the scope Repos). The tree does not record its scope (the model's
\* tree is treeVer/treeOpen alone); it holds only the issues its import
\* fetched, so a verify whose scope is wider than the import's reads the
\* extra repositories as missing and never reports done (D12: it fails safe).
\* scope and vscope are the model's record of the import's and the verify's
\* scopes, not tree data; they exist to state the invariants.
\*
\* An issue's import states (phase):
\*   absent            not read yet
\*   fetched           read from GitHub (workgh.Fetcher.Issues), not in the tree
\*   intree            in the tree file (import wrote it; nova-work import)
\*   mirrored          a verify found it equal to GitHub (nova-work verify)
\*   closed            closed on GitHub by a destructive import (not built)
\*   reopened          re-opened on GitHub by export (not built)
\* A repository's sync states (sync):
\*   idle -> importing -> imported -> verified -> closing -> exporting
\*   imported -> importing (a verify found drift; import again)
\*
\* Broken = "none" is the design. Every other value is a reversed witness,
\* one guard removed, and the invariant its comment names fails:
\*   "noreceipt"  close without a verify receipt         -> DestructiveOnlyWithReceipt
\*   "stale"      close on a receipt older than an edit   -> Lossless
\*   "external"   close an external issue                 -> ExternalStaysOpen
\*   "noreopen"   export that does not re-open            -> ExportRestores
\*   "partial"    write the tree before every issue is fetched -> TreeIsWholeForItsScope
\*   "scoped"     a wider verify passes on a scoped tree  -> VerifyNeverPassesOutsideItsScope

EXTENDS Naturals, FiniteSets

CONSTANTS Internal, External, Repos, MaxEdits, Broken

Issues == Internal \cup External
None == 0 - 1   \* no version: not fetched, no receipt
Scopes == SUBSET Repos \ {{}}   \* a scope is a non-empty subset of repositories
IssuesOf(S) == UNION S          \* the issues a scope covers

\* Repos partitions Issues: every issue is in one repository, and repositories
\* do not overlap.
ASSUME Issues = UNION Repos /\ \A r, s \in Repos : r # s => r \cap s = {}

VARIABLES
  ghVer,        \* GitHub's version of each issue's contents
  ghOpen,       \* whether the issue is open on GitHub
  fetched,      \* the version the running import read, or None
  treeVer,      \* the version the tree holds, or None
  treeOpen,     \* the state the tree holds
  receipt,      \* the version a zero-difference verify saw, or None (design-only)
  closedByImport, \* closed on GitHub by the destructive mode
  closedVer,    \* ghost: GitHub's version when the import closed it
  closedWithReceipt, \* ghost: a receipt was held when it was closed
  phase,
  sync,
  scope,        \* the scope the last import read (its repositories)
  vscope        \* the scope the last verify read

vars == <<ghVer, ghOpen, fetched, treeVer, treeOpen, receipt, closedByImport, closedVer, closedWithReceipt, phase, sync, scope, vscope>>

Phases == {"absent", "fetched", "intree", "mirrored", "closed", "reopened"}
Syncs == {"idle", "importing", "imported", "verified", "closing", "exporting"}
Vers == 0..MaxEdits \cup {None}

TypeOK ==
  /\ ghVer \in [Issues -> 0..MaxEdits]
  /\ ghOpen \in [Issues -> BOOLEAN]
  /\ fetched \in [Issues -> Vers]
  /\ treeVer \in [Issues -> Vers]
  /\ treeOpen \in [Issues -> BOOLEAN]
  /\ receipt \in [Issues -> Vers]
  /\ closedByImport \in [Issues -> BOOLEAN]
  /\ closedVer \in [Issues -> Vers]
  /\ closedWithReceipt \in [Issues -> BOOLEAN]
  /\ phase \in [Issues -> Phases]
  /\ sync \in Syncs
  /\ scope \in SUBSET Repos
  /\ vscope \in SUBSET Repos

Init ==
  /\ ghVer = [i \in Issues |-> 0]
  /\ ghOpen = [i \in Issues |-> TRUE]
  /\ fetched = [i \in Issues |-> None]
  /\ treeVer = [i \in Issues |-> None]
  /\ treeOpen = [i \in Issues |-> TRUE]
  /\ receipt = [i \in Issues |-> None]
  /\ closedByImport = [i \in Issues |-> FALSE]
  /\ closedVer = [i \in Issues |-> None]
  /\ closedWithReceipt = [i \in Issues |-> FALSE]
  /\ phase = [i \in Issues |-> "absent"]
  /\ sync = "idle"
  /\ scope = Repos
  /\ vscope = Repos

\* An outside event: someone edits the issue on GitHub (a comment, a label,
\* a title). Any time, whatever the tree holds.
ExternalEdit(i) ==
  /\ ghVer[i] < MaxEdits
  /\ ghVer' = [ghVer EXCEPT ![i] = @ + 1]
  /\ UNCHANGED <<ghOpen, fetched, treeVer, treeOpen, receipt, closedByImport, closedVer, closedWithReceipt, phase, sync, scope, vscope>>

\* nova-work import begins a pass over its scope (non-destructive).
StartImport(S) ==
  /\ S \in Scopes
  /\ sync \in {"idle", "imported"}
  /\ sync' = "importing"
  /\ scope' = S
  /\ fetched' = [i \in Issues |-> None]
  /\ phase' = [i \in Issues |-> IF phase[i] \in {"closed", "reopened"} THEN phase[i] ELSE "absent"]
  /\ receipt' = [i \in Issues |-> None]
  /\ UNCHANGED <<ghVer, ghOpen, treeVer, treeOpen, closedByImport, closedVer, closedWithReceipt, vscope>>

\* workgh.Fetcher.Issues reads one issue's full contents.
Fetch(i) ==
  /\ sync = "importing"
  /\ phase[i] = "absent"
  /\ i \in IssuesOf(scope)
  /\ fetched' = [fetched EXCEPT ![i] = ghVer[i]]
  /\ phase' = [phase EXCEPT ![i] = "fetched"]
  /\ UNCHANGED <<ghVer, ghOpen, treeVer, treeOpen, receipt, closedByImport, closedVer, closedWithReceipt, sync, scope, vscope>>

\* nova-work import writes the tree once every issue of the scope is fetched
\* and the encoded tree read back equals what was fetched
\* (cmd/nova-work/import.go). The tree is written fresh: it holds the fetched
\* issues of the scope and nothing else, so a re-import drops the repositories
\* the previous tree held.
WriteTree ==
  /\ sync = "importing"
  /\ \/ \A i \in IssuesOf(scope) : phase[i] \in {"fetched", "closed", "reopened"}
     \/ Broken = "partial" /\ \E i \in IssuesOf(scope) : phase[i] = "fetched"
  /\ treeVer' = [i \in Issues |-> IF phase[i] = "fetched" THEN fetched[i]
                                  ELSE IF phase[i] \in {"closed", "reopened"} THEN treeVer[i]
                                  ELSE None]
  /\ treeOpen' = [i \in Issues |-> IF phase[i] = "fetched" THEN ghOpen[i]
                                   ELSE IF phase[i] \in {"closed", "reopened"} THEN treeOpen[i]
                                   ELSE TRUE]
  /\ phase' = [i \in Issues |-> IF phase[i] = "fetched" THEN "intree" ELSE phase[i]]
  /\ sync' = "imported"
  /\ UNCHANGED <<ghVer, ghOpen, fetched, receipt, closedByImport, closedVer, closedWithReceipt, scope, vscope>>

\* Whether a verify of scope V finds zero differences. Every issue of the
\* scope must be in the tree and equal to GitHub; an issue of the scope that
\* the tree does not hold (a wider scope than the import) is a difference,
\* so the verify is not done. Broken = "scoped" drops the guard that every
\* issue of the scope is in the tree, so a wider verify passes on a scoped
\* tree.
VerifyDone(V) ==
  IF Broken = "scoped"
  THEN \A i \in IssuesOf(V) : treeVer[i] # None => treeVer[i] = ghVer[i] /\ treeOpen[i] = ghOpen[i]
  ELSE \A i \in IssuesOf(V) : treeVer[i] # None /\ treeVer[i] = ghVer[i] /\ treeOpen[i] = ghOpen[i]

\* nova-work verify: a fresh read compared field for field over the scope.
\* Zero differences over the scope is the receipt; any difference leaves
\* the repository imported, to be imported again.
Verify(V) ==
  /\ V \in Scopes
  /\ sync \in {"imported", "verified"}
  /\ vscope' = V
  /\ IF VerifyDone(V)
       THEN /\ receipt' = [i \in Issues |-> IF i \in IssuesOf(V) /\ treeVer[i] # None THEN ghVer[i] ELSE None]
            /\ phase' = [i \in Issues |-> IF i \in IssuesOf(V) /\ treeVer[i] # None THEN "mirrored" ELSE phase[i]]
            /\ sync' = "verified"
       ELSE /\ receipt' = [i \in Issues |-> None]
            /\ phase' = [i \in Issues |-> IF i \in IssuesOf(V) /\ phase[i] = "mirrored" THEN "intree" ELSE phase[i]]
            /\ sync' = "imported"
  /\ UNCHANGED <<ghVer, ghOpen, fetched, treeVer, treeOpen, closedByImport, closedVer, closedWithReceipt, scope>>

VerifyAny == \E V \in Scopes : Verify(V)

\* The destructive mode (specified, not built): close an internal, open
\* issue on GitHub after the import, only on a receipt taken at GitHub's
\* current version (the close is conditional on the issue unchanged since
\* the verify).
ReceiptHolds(i) ==
  CASE Broken = "noreceipt" -> TRUE
    [] Broken = "stale" -> receipt[i] # None
    [] OTHER -> receipt[i] = ghVer[i]

Close(i) ==
  /\ sync \in {"verified", "closing"} \/ (Broken = "noreceipt" /\ sync = "imported")
  /\ phase[i] \in {"mirrored", "intree"}
  /\ i \in Internal \/ Broken = "external"
  /\ ghOpen[i]
  /\ ReceiptHolds(i)
  /\ ghOpen' = [ghOpen EXCEPT ![i] = FALSE]
  /\ closedByImport' = [closedByImport EXCEPT ![i] = TRUE]
  /\ closedVer' = [closedVer EXCEPT ![i] = ghVer[i]]
  /\ closedWithReceipt' = [closedWithReceipt EXCEPT ![i] = receipt[i] # None]
  /\ phase' = [phase EXCEPT ![i] = "closed"]
  /\ sync' = "closing"
  /\ UNCHANGED <<ghVer, fetched, treeVer, treeOpen, receipt, scope, vscope>>

\* Export (specified, not built): re-open an issue the import closed.
Export(i) ==
  /\ sync \in {"closing", "exporting"}
  /\ phase[i] = "closed"
  /\ closedByImport[i]
  /\ ghOpen' = [ghOpen EXCEPT ![i] = Broken # "noreopen"]
  /\ phase' = [phase EXCEPT ![i] = "reopened"]
  /\ sync' = "exporting"
  /\ UNCHANGED <<ghVer, fetched, treeVer, treeOpen, receipt, closedByImport, closedVer, closedWithReceipt, scope, vscope>>

Next ==
  \/ \E i \in Issues : ExternalEdit(i) \/ Fetch(i) \/ Close(i) \/ Export(i)
  \/ \E S \in Scopes : StartImport(S)
  \/ WriteTree
  \/ VerifyAny

Spec == Init /\ [][Next]_vars /\ WF_vars(WriteTree) /\ WF_vars(VerifyAny) /\ \A i \in Issues : WF_vars(Fetch(i))

----------------------------------------------------------------------------
\* The invariants.

\* A tree is written only whole for its scope: every issue of the scope the
\* import read is in it.
TreeIsWholeForItsScope ==
  sync \in {"imported", "verified", "closing", "exporting"} => \A i \in IssuesOf(scope) : treeVer[i] # None

\* A verify never reports done for a scope wider than the import's: the
\* extra repositories are missing from the tree, so there are differences.
VerifyNeverPassesOutsideItsScope ==
  sync = "verified" => vscope \subseteq scope

\* Only a verify receipt licenses a close.
DestructiveOnlyWithReceipt ==
  \A i \in Issues : closedByImport[i] => closedWithReceipt[i]

\* Lossless: an issue the import closed is held by the tree exactly as GitHub
\* held it when it was closed, and the tree says it was open.
Lossless ==
  \A i \in Issues : closedByImport[i] => closedVer[i] = treeVer[i] /\ treeOpen[i]

\* An external issue is never closed by the import; it stays open until its
\* fix closes it.
ExternalStaysOpen ==
  \A i \in External : ~closedByImport[i]

\* The round trip: an issue export re-opened, not edited since it was
\* closed, is on GitHub exactly as the tree holds it.
ExportRestores ==
  \A i \in Issues : phase[i] = "reopened" /\ ghVer[i] = closedVer[i] => ghVer[i] = treeVer[i] /\ ghOpen[i] = treeOpen[i]

\* A receipt names what GitHub held when it was taken; the tree held it too.
ReceiptIsTheTree ==
  \A i \in Issues : receipt[i] # None => receipt[i] = treeVer[i]

\* Liveness, under fairness: an import that starts writes a whole tree.
ImportEnds == sync = "importing" ~> sync \in {"imported", "verified"}
=============================================================================
