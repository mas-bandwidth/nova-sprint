package secrets

import (
	"strings"
)

// NameRow is one key name in a seat's file and whether the store keeps it in the clear.
// It holds no value, sealed or clear.
type NameRow struct {
	Name  string
	Clear bool
}

// ParseNameLine reads one line of names' typed output: a NAME row's key name and whether
// the store keeps it in the clear, false when the line is no NAME row. The backup's scan
// (cmd/nova-sprint/backup_out.go) and the doctor's member probe (cmd/nova-sprint/doctor_probe.go)
// both read a seat through it, so neither can drift from the line names prints.
func ParseNameLine(line string) (NameRow, bool) {
	f := strings.Fields(line)
	if len(f) != 4 || f[0] != "SECRETS" || f[1] != "NAME" || !strings.HasPrefix(f[2], "key=") || !strings.HasPrefix(f[3], "clear=") {
		return NameRow{}, false
	}
	return NameRow{Name: strings.TrimPrefix(f[2], "key="), Clear: f[3] == "clear=true"}, true
}
