package config

import (
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// The typed lines nova-config prints (docs/SPEC-CONFIG.md, "Lines"). Every
// line is one event, first token the record, then key=value fields whose
// values go through oneline.Field, so a whitespace-splitting scanner sees
// exactly the fields the tool wrote; an empty value prints as "-".

// Value renders one field value.
func Value(s string) string {
	if s == "" {
		return "-"
	}
	return oneline.Field(s)
}
