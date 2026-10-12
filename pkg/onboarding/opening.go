package onboarding

import (
	"regexp"
)

// The opening of a banner answers the three questions a stranger brings to a
// tool before the usage lines do (docs/ONBOARDING.md point 6): what does it do,
// how does it work, and how do I use it. Line 1 is the first answer, one
// sentence; a paragraph labelled HowItWorksLabel near the top is the second;
// the `example:` block, three or more command lines run in order, is the third.
// The functions here read those three parts; the caller's test decides.

// HowItWorksLabel opens the paragraph that names a tool's nouns and where its
// state lives.
const HowItWorksLabel = "how it works:"

// HowItWorksWithin is how many lines from the top the paragraph must start in:
// it is read before the usage lines, never found under them.
const HowItWorksWithin = 15

// HowItWorksMaxLines is the most lines the how-it-works paragraph takes: the
// tool's nouns and where its state lives, not a second manual.
const HowItWorksMaxLines = 5

// MinExampleCommands is the fewest command lines an `example:` block holds: a
// first run is a sitting, and one line is a single call rather than a sitting.
const MinExampleCommands = 3

// sentenceBreak is a sentence ending inside the line: a full stop, a semicolon,
// a question or an exclamation mark with more words after it.
var sentenceBreak = regexp.MustCompile(`[.;?!] `)

