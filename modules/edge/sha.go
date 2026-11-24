package edge

import "strings"

// IsFullSHA reports whether a Git object name contains exactly 40 hexadecimal digits.
func IsFullSHA(s string) bool {
	return len(s) == 40 && strings.Trim(s, "0123456789abcdefABCDEF") == ""
}
