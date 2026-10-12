package decide

// The few-shot calibration of the grade (SPEC-NOVA-DECIDE section 11): ten landed cards per
// class, drawn from the sprint record by the caller's examples file, ahead of the card under
// grade. The library reads no store: the examples are an input.

// GradeExamplesPerClass is how many examples of each class the prompt carries.
const GradeExamplesPerClass = 10

// GradeHeavy is the label of a card that needed the heavy tier; the grade's own options stay
// script, flash and pro, and the examples teach the line between them.
const GradeHeavy = "heavy"

// GradeExampleClasses are the example labels in prompt order: landed on flash within two
// attempts, needed pro, needed heavy.
var GradeExampleClasses = []string{GradeFlash, GradePro, GradeHeavy}

// Example is one landed card of the record: its brief heading, its PATHS line, its KIND and the
// label of its outcome. Never the brief's whole text.
type Example struct {
	Card    string `json:"card"`
	Heading string `json:"heading"`
	Paths   string `json:"paths"`
	Kind    string `json:"kind"`
	Label   string `json:"label"`
}
