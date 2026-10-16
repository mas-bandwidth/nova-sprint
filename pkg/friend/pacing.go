package friend

import (
	"time"
)

// WindowUse is one subscription window as the harness last reported it:
// its name, the fraction used (0 when the report named none: under the
// harness's warning threshold), the harness's status (allowed,
// allowed_warning, rejected), when it resets (zero: not said) and when it
// was read.
type WindowUse struct {
	Window string    `json:"window"`
	Used   float64   `json:"used"`
	Status string    `json:"status,omitempty"`
	Resets time.Time `json:"resets,omitzero"`
	At     time.Time `json:"at,omitzero"`
}
