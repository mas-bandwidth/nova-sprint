# The standard checker's fixtures

The reversed witnesses of `internal/ci/standard_pins_test.go`
(`TestTheStandardRefusesEachReversedWitness`). Each fixture is a small repository held as
text in the fenced blocks below, so no fixture file is ever compiled, linted or read as part
of this repository. A block opens with a line `~~~ file <fixture> <path>` and closes with a
line `~~~`. The fixture `base` is a clean standard of two sections that passes every check;
every other fixture is the base with its own files laid over it, and breaks exactly one rule.
The test names the line each witness must fail with; that line names the row or section.

| Fixture | Breaks |
|---|---|
| unpinned-must | a binding row with no pin |
| missing-test | a test pin naming a test no `_test.go` file declares |
| wrong-constant | a constant pin whose value differs from the code |
| divergence-test-exists | a divergence whose owed test already exists |
| heading-off-outline | a section heading that is not the outline's |
| lower-case-must | a lower-case "must" in the normative text |
| tla-undefined | a tla pin naming a property the module does not define |
| tla-not-gated | a tla pin naming a property no passing case checks |
| hard-row-removed | a hard row of the hard table gone from its section |
| pin-in-wrong-section | a pin in another section's pin file |
| key-word-outside-a-row | a key word outside a numbered row |
| tla-not-extended | a tla pin whose passing case's model does not extend the module |
| tla-bench-only | a tla pin checked only by a bench case, which no gate requires |
| tla-only-a-constant | a tla pin whose name the cfg carries only outside INVARIANTS and PROPERTIES |
| skipped-test | a test pin naming a test that calls t.Skip |
| build-tagged-test | a test pin naming a test whose file the unit tier's build leaves out |
| owed-test-elsewhere | a divergence whose owed test exists in another package |

## base

~~~ file base standard/STYLE.md
# Style

<!-- outline:begin -->
| Id | Heading |
|---|---|
| s1 | Introduction |
| s2 | Things |
<!-- outline:end -->

<!-- hard:begin -->
| Hard | Anchor | Section |
|---|---|---|
| H1 | R-HARD-ONE | s2 |
<!-- hard:end -->
~~~

~~~ file base standard/SPRINT.md
# The standard

- [s1 Introduction](sprint/s1.md)
- [s2 Things](sprint/s2.md)
~~~

~~~ file base standard/sprint/s1.md
<a id="s1"></a>
# s1. Introduction

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT",
"RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be
interpreted as described in BCP 14 (RFC 2119, RFC 8174) when, and only when, they appear in
all capitals, as shown here.

<a id="R-ONE"></a>
**NS-1.1 R-ONE.** The tick MUST deal at the width.

<a id="R-OPTION"></a>
**NS-1.2 R-OPTION.** The coordinator MAY ask again.
~~~

~~~ file base standard/sprint/s2.md
<a id="s2"></a>
# s2. Things

Status: pending

Scope: the things.

<a id="R-HARD-ONE"></a>
**NS-2.1 R-HARD-ONE.** (hard) The tick MUST NOT wait.
~~~

~~~ file base standard/rationale/s1.md
<a id="s1"></a>
# s1. Introduction (rationale)

Why.
~~~

~~~ file base standard/rationale/s2.md
<a id="s2"></a>
# s2. Things (rationale)

The owner: "it must never wait".
~~~

~~~ file base standard/pins/s1.tsv
# s1 Introduction
R-ONE	test	./pkg/x	TestXHolds
R-ONE	const	./pkg/x	Width=4
R-ONE	const	./pkg/x	Wait=60m
R-ONE	const	./pkg/x	Name="x"
R-ONE	tla	X	Safe
R-ONE	external	mas-bandwidth/other	./cmd/y TestYHolds
~~~

~~~ file base standard/pins/s2.tsv
# s2 Things
R-HARD-ONE	divergence	fix-card	./pkg/x TestXNeverWaits
~~~

~~~ file base pkg/x/x.go
package x

import "time"

const Width = 4

const (
	Wait = 60 * time.Minute
	Name = "x"
)
~~~

~~~ file base pkg/x/x_test.go
package x

import "testing"

func TestXHolds(t *testing.T) {}
~~~

~~~ file base tla/X.tla
---- MODULE X ----
Safe == TRUE
Unchecked == TRUE
====
~~~

~~~ file base tla/MCX.tla
---- MODULE MCX ----
EXTENDS X
====
~~~

~~~ file base tla/MCX.cfg
\* Unchecked is named only in this comment
INVARIANT Safe
~~~

~~~ file base tla/CASES.tsv
config	module	expected	property	deadlock	group	gate	debt
MCX.cfg	MCX.tla	pass	-	ignore-terminal	x	required	-
~~~

## The witnesses

~~~ file unpinned-must standard/sprint/s2.md
<a id="s2"></a>
# s2. Things

Status: pending

Scope: the things.

<a id="R-HARD-ONE"></a>
**NS-2.1 R-HARD-ONE.** (hard) The tick MUST NOT wait.

<a id="R-LOOSE"></a>
**NS-2.2 R-LOOSE.** The tick MUST deal again.
~~~

~~~ file missing-test standard/pins/s1.tsv
# s1 Introduction
R-ONE	test	./pkg/x	TestGone
~~~

~~~ file wrong-constant standard/pins/s1.tsv
# s1 Introduction
R-ONE	const	./pkg/x	Width=5
~~~

~~~ file divergence-test-exists standard/pins/s2.tsv
# s2 Things
R-HARD-ONE	divergence	fix-card	./pkg/x TestXHolds
~~~

~~~ file heading-off-outline standard/sprint/s2.md
<a id="s2"></a>
# s2. Other things

Status: pending

Scope: the things.

<a id="R-HARD-ONE"></a>
**NS-2.1 R-HARD-ONE.** (hard) The tick MUST NOT wait.
~~~

~~~ file lower-case-must standard/sprint/s1.md
<a id="s1"></a>
# s1. Introduction

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT",
"RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be
interpreted as described in BCP 14 (RFC 2119, RFC 8174) when, and only when, they appear in
all capitals, as shown here.

<a id="R-ONE"></a>
**NS-1.1 R-ONE.** The tick MUST deal at the width, and it must deal again.
~~~

~~~ file tla-undefined standard/pins/s1.tsv
# s1 Introduction
R-ONE	tla	X	Unsafe
~~~

~~~ file tla-not-gated standard/pins/s1.tsv
# s1 Introduction
R-ONE	tla	X	Unchecked
~~~

~~~ file hard-row-removed standard/sprint/s2.md
<a id="s2"></a>
# s2. Things

Status: pending

Scope: the things.
~~~

~~~ file hard-row-removed standard/pins/s2.tsv
# s2 Things
~~~

~~~ file pin-in-wrong-section standard/pins/s2.tsv
# s2 Things
R-HARD-ONE	divergence	fix-card	./pkg/x TestXNeverWaits
R-ONE	test	./pkg/x	TestXHolds
~~~

~~~ file key-word-outside-a-row standard/sprint/s1.md
<a id="s1"></a>
# s1. Introduction

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT",
"RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be
interpreted as described in BCP 14 (RFC 2119, RFC 8174) when, and only when, they appear in
all capitals, as shown here.

The tick MUST deal.

<a id="R-ONE"></a>
**NS-1.1 R-ONE.** The tick MUST deal at the width.
~~~

~~~ file tla-not-extended tla/MCX.tla
---- MODULE MCX ----
\* X is named here only in a comment: EXTENDS X
EXTENDS Naturals
====
~~~

~~~ file tla-bench-only tla/CASES.tsv
config	module	expected	property	deadlock	group	gate	debt
MCX.cfg	MCX.tla	pass	-	ignore-terminal	x	bench	not measured
~~~

~~~ file tla-only-a-constant tla/MCX.cfg
CONSTANTS Safe = TRUE
INVARIANT TypeOK
~~~

~~~ file skipped-test pkg/x/x_test.go
package x

import "testing"

func TestXHolds(t *testing.T) { t.Skip("not yet") }
~~~

~~~ file build-tagged-test pkg/x/x_test.go
//go:build functional

package x

import "testing"

func TestXHolds(t *testing.T) {}
~~~

~~~ file owed-test-elsewhere cmd/y/y_test.go
package y

import "testing"

func TestXNeverWaits(t *testing.T) {}
~~~

