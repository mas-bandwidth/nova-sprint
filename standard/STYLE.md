# The nova-sprint standard: style guide

This file is for the people who write the standard. It fixes, once, every choice a section
writer would otherwise make, so that twelve sections written by twelve hands read as one
document. It is not part of the standard and binds no implementation.
`internal/ci/standard_pins_test.go` enforces every rule below that a program can check; the
rest are for the writer and the cold reader.

## 1. The documents

| Document | Tag | Status | Files |
|---|---|---|---|
| The standard | NS-1 | normative | `standard/SPRINT.md` (the title and the index, in the outline's order) and one file per section, `standard/sprint/<id>.md` |
| The rationale | NS-R | informative | one file per section, `standard/rationale/<id>.md`: the reasons, the history and the owner's dated words |
| The pins | - | the checker's input | one file per section, `standard/pins/<id>.tsv` |
| This guide | - | for writers | `standard/STYLE.md` |

NS-1 states requirements and nothing else. `docs/SPEC-SPRINT.md` stays as the descriptive
document: how the code does it, with its names. Where the two disagree, NS-1 states the intent
and the disagreement is a divergence pin (section 9).

One file per section, so two section cards never share a file. A section card edits only its
own `standard/sprint/<id>.md`, `standard/rationale/<id>.md` and `standard/pins/<id>.tsv`.

## 2. Vocabulary

One word for one thing, everywhere in NS-1:

| Word | Means | Never |
|---|---|---|
| machine | the running sprint: its server, its tick and its land loop, which make every mechanical move | system, daemon, bot |
| tick | one pass of the machine over the store, which deals, promotes, judges and closes | cycle, loop |
| member | a fleet computer enrolled in the sprint, which takes and works cards on its own row | machine, host, box |
| friend | an outside session (a person's model, in its own harness) enrolled under a name, which works cards on its own row | agent, helper |
| row | the place a member's or a friend's cards are dealt to | lane (a lane is one unit of a row's width) |
| lane | one unit of a row's width: one work card or two reads | slot (a read holds half a lane) |
| coordinator | the one seat that answers judgments and decides what the machine does not | captain, admin |
| card | one unit of work with its brief | ticket, job, task |
| primary | the card a stream lands; its work card and its read cards are cut from it | parent |
| work card | the card a worker takes to do a primary's work at one attempt | job |
| read card | the card a reader takes to read a primary's attempt | review card |
| attempt | one try at a primary's work, numbered from 1; a rework starts the next | pass, retry |
| worker | the member or friend that works an attempt | builder |
| reader | a member or friend dealt a read card | reviewer |
| read | a reader's verdict on an attempt's head, ok or broken | review (review is the primary's column while it is read) |
| deal | the tick's step that places ready cards on rows | assign, dispatch |
| take | a row's step that moves a dealt card from ready to working | claim, grab |
| need | a dependency without which a card cannot compile or run | blocker, prerequisite |
| order | a land-after relation between cards of a stream that is not a need | soft dependency |
| stream | a named sequence of primaries that land on one base | track, epic |
| base | the branch a stream lands on | target, trunk |
| landing | the step that merges a primary's head onto its base and pushes it | merge (a merge is the git step inside it) |
| tier | the class of model a card needs: flash, pro, heavy or frontier, in that order | level (a level is a card's priority) |
| level | a card's priority: blocker, critical, fix, high, normal, low | tier |
| judgment | a decision the machine raises for the coordinator | alert, ticket |
| bound | a limit the machine enforces by itself, with its value in a Bounds table | timeout (a timeout is one kind of bound) |
| push | a message the machine sends to a row, over the bus, when an event happens | notification, poll |
| hook | a row's live subscription to its pushes, proven by the challenge | listener, watcher |
| playbook | the text a verb prints that tells a seat how to operate: the coordinator's playbook and the worker playbook | runbook (the runbook is a document, not a verb's output) |
| epoch | one numbered start of the sprint; a clear begins the next | session, run |

## 3. Section ids, headings and the outline

Every section has an id, `s1` to `s12`, the same in NS-1 and NS-R. NS-1's section file begins

    <a id="s5"></a>
    # s5. Reads

and NS-R's begins `# s5. Reads (rationale)` under the same anchor. The heading text after
`s5. ` is exactly the outline's heading. Subsections are `## s5.1. Who reads` and so on.

The outline below is the one source of the section list. The checker reads it from this file
and fails when `standard/SPRINT.md`'s index, a section file's heading or a rationale file's
heading differs from it in text, id or order, and when a section file exists that the outline
does not name. Adding, removing or renaming a section is an edit here first.

<!-- outline:begin -->
| Id | Heading |
|---|---|
| s1 | Introduction, terminology and conventions |
| s2 | Cards and briefs |
| s3 | The deal |
| s4 | Attempts and finishes |
| s5 | Reads |
| s6 | Landing |
| s7 | Bases and forward-merges |
| s8 | Judgments and bounds |
| s9 | The bus, hooks and pushes |
| s10 | The store and tables |
| s11 | Config sources |
| s12 | The machine: start, stop, epochs and playbooks |
<!-- outline:end -->

`standard/SPRINT.md`'s index is one line per section, in the outline's order:

    - [s5 Reads](sprint/s5.md)

## 4. Requirement rows

A requirement is a numbered row. Its id is `NS-<section>.<n>`, `n` from 1 in the section,
never reused and never renumbered once committed; a row removed leaves its number unused. Each
row has a stable anchor `R-<NAME>` (capitals, digits and hyphens), unique across the standard
and never reused for another rule. A row is written

    <a id="R-READS-PARALLEL"></a>
    **NS-5.1 R-READS-PARALLEL.** The deal MUST ...

and runs to the next blank line. Every BCP 14 key word in NS-1 lies inside a row; a key word
outside one is refused. A row the owner set as a hard row carries `(hard)` after its anchor:

    **NS-5.1 R-READS-PARALLEL.** (hard) The deal MUST ...

## 5. Keywords

NS-1 uses the BCP 14 key words (RFC 2119, RFC 8174): MUST, MUST NOT, REQUIRED, SHALL, SHALL
NOT, SHOULD, SHOULD NOT, RECOMMENDED, NOT RECOMMENDED, MAY and OPTIONAL.

- A key word is in capitals exactly where it binds, and in no other place.
- The lower-case words "must", "shall", "should" and "required" do not appear in NS-1 at all:
  write the requirement in capitals, or rephrase it as a plain statement of fact. The checker
  refuses them.
- The BCP 14 sentence appears once, in s1. No other section restates it.
- NS-R is informative and contains no capitalised MUST, SHALL, SHOULD or REQUIRED. The checker
  refuses one there; a quotation that needs one is written in lower case.
- Each requirement names its actor: "The tick MUST", "A member MUST NOT", "The coordinator
  MAY". Never "it is required that".

## 6. Normative text carries no history

NS-1 never contains history, past rulings, corrections, "previously", dates, names of people,
machines or customers, source file names, paths or `file:line`, card names, test names, TLA+
module or property names, or Go identifiers. A row carries at most one sentence of reason; the
rest goes to NS-R under the same section id, with the owner's dated words quoted. The checker
refuses dates, paths, test names, `TLA+`, the owner's and the fleet's names, and identifiers
written in camel case. The verbs of the command line (`nova-sprint help playbook`) are the
interface, not names, and NS-1 may quote them.

## 7. The section shape

Every written section of NS-1 is, in this order, each part present only when the section has
one:

1. **Rule.** The requirement rows (section 4), grouped in subsections when there are many.
2. **State table**, where the section has states:

   | State | Event | Guard | Action | Next state |
   |---|---|---|---|---|

   One row per (state, event, guard). An event a state does not list is ignored in that state,
   and the section says so once.
3. **Bounds table**, where the section has bounds:

   | Bound | Value | Holds | On reaching it |
   |---|---|---|---|

   Each bound is also a row (section 4) pinned to its constant.

## 8. Pending and written

A section not yet written carries, directly under its heading, the line

    Status: pending

followed by a `Scope:` paragraph of one sentence naming what the section will contain. A
section is written when that line is gone. The checker lists pending sections and does not fail
on them; a pending section's scope is not normative. A hard row in a pending section is normative
and checked: it carries its pin from the commit that writes it.

## 9. Pins

Every row with MUST, MUST NOT, SHALL, SHALL NOT or REQUIRED is pinned in
`standard/pins/<section id>.tsv`. A pin file is tab-separated, one pin a line; a line beginning
`#` is a comment, and every pin file begins with a comment naming its section. A row may have
several pins, and a pin file holds pins of its own section's rows only. The kinds:

    <R-NAME>	test	<package dir>	<TestName>
    <R-NAME>	tla	<Module>	<invariant or property>
    <R-NAME>	const	<package dir>	<Name>=<value>
    <R-NAME>	external	<owner/repo>	<package dir> <TestName>
    <R-NAME>	divergence	<fix card id>	<package dir> <owed TestName>

- **test**: the test is declared in a `_test.go` file of the package directory (`./internal/sprint`).
- **tla**: `tla/<Module>.tla` defines the name, and a case in `tla/CASES.tsv` expecting `pass`
  checks it (its cfg names it and its model extends the module).
- **const**: the package declares the constant and its value equals `<value>`: a Go literal, or
  a duration (`60m`) for a duration constant.
- **external**: a test in another repository. The checker checks its form only and lists it.
- **divergence**: the code does not hold the row yet. The row states the intent; the pin names
  the card that fixes the code and the test that card owes, which does not exist yet (the
  checker fails a divergence whose owed test already exists: pin the test instead). An owed test
  in another repository is written `external <owner/repo> <package dir> <TestName>`. The checker
  prints `STANDARD DIVERGENCE <R-NAME> fix=<card> owed=<test>` for each and does not fail on it.
  A row the code holds in part carries a passing pin for the part it holds and a divergence for
  each part it does not.

The checker fails an unpinned row, a pin naming a row that does not exist or lies in another
section, a test that is not declared, a name the module does not define or no passing case
checks, and a constant that differs.

## 10. Hard rows

The owner's hard requirements are rows marked `(hard)`. The table below is the one list of them;
the checker fails when a listed row is missing from its section, is not marked hard, carries no
MUST or MUST NOT, or has no pin. A later card clarifies a hard row or pins it, and never weakens
or removes one: a change to this table that drops a row is refused in review.

<!-- hard:begin -->
| Hard | Anchor | Section |
|---|---|---|
| H1 | R-READS-PARALLEL | s5 |
| H2 | R-LAND-PARALLEL | s6 |
| H3 | R-PROMOTE | s3 |
| H4 | R-SPLIT-HEAVY | s3 |
| H5 | R-SPLIT-PRO | s3 |
| H6 | R-AUTO-TAKE | s3 |
| H7 | R-MINIMAL-DEPS | s2 |
| H8 | R-LINT-ON-ADD | s2 |
| H9 | R-CHECK-BEFORE-START | s12 |
| H10 | R-HOOKED | s9 |
| H11 | R-PUSH-ONLY | s9 |
| H11 | R-PUSH-DEAL | s9 |
| H11 | R-PUSH-READ | s9 |
| H11 | R-PUSH-JUDGMENT | s9 |
| H11 | R-PUSH-STUCK | s9 |
| H11 | R-PUSH-BOUND | s9 |
| H11 | R-PUSH-STALLED | s9 |
| H11 | R-PUSH-FINISH-DUE | s9 |
| H11 | R-PUSH-FRIEND-STATE | s9 |
| H11 | R-PUSH-LANDING | s9 |
| H11 | R-PUSH-CI | s9 |
| H12 | R-PLAYBOOK | s12 |
| H13 | R-WORKER-PLAYBOOK | s12 |
<!-- hard:end -->

H11 is the rule that every communication is a push (`R-PUSH-ONLY`) and one row per event class
(`R-PUSH-<CLASS>`), each a hard row of its own.

## 11. Cross-references

A reference to another section or row is a link whose text is its id:
`[s6](s6.md)`, `[NS-6.1](s6.md#R-LAND-PARALLEL)`. External standards are cited by name and
number (RFC 2119) with no link.
