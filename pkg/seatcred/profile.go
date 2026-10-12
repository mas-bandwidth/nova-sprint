package seatcred

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/secrets"
)

// A seat profile is one row of a tool's seats.tsv: the seat
// is a fact of the machine, not of the shell, so the fleet play writes where
// its Redis is, the ACL user, which key of the seat's file holds that user's
// password, and the store and age key that open the file. A tool given
// --seat <name> reads the row and needs no wrapper script to reach Redis.

// ProfileFile is the profile's name under $XDG_CONFIG_HOME/<tool>/.
const ProfileFile = "seats.tsv"

// ProfilePath is $XDG_CONFIG_HOME/<tool>/seats.tsv, else
// $HOME/.config/<tool>/seats.tsv (the XDG default).
func ProfilePath(tool string, getenv func(string) string) (string, error) {
	if x := getenv("XDG_CONFIG_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, tool, ProfileFile), nil
	}
	home := getenv("HOME")
	if home == "" {
		return "", fmt.Errorf("seat profile: HOME and XDG_CONFIG_HOME are unset, so %s/%s has no place", tool, ProfileFile)
	}
	return filepath.Join(home, ".config", tool, ProfileFile), nil
}

var secretEnvName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// ErrNoProfileRow is wrapped by LoadConfigProfile's refusal when the file has
// no row for the seat (or there is no file).
var ErrNoProfileRow = errors.New("no row")

// ConfigProfileColumns names the tab-separated columns for a nova-config seat profile.
const ConfigProfileColumns = "name, dsn, password env"

// ConfigProfile is one seats.tsv row for nova-config: the seat names where its
// Postgres store is, and the name of the variable holding its password.
type ConfigProfile struct {
	Name        string // the seat --seat names
	DSN         string // the PostgreSQL DSN, postgres://user@host:port/db
	PasswordEnv string // the name of the variable that holds the password
}

// LoadConfigProfile reads path and returns the row named seat. A missing file or
// row is an error wrapping ErrNoProfileRow. When seat is not found in the file,
// the error names the known seats. Blank lines and lines starting with # are
// skipped.
func LoadConfigProfile(path, seat string) (ConfigProfile, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return ConfigProfile{}, fmt.Errorf("seat %s: %w: %s does not exist; the fleet play writes it, one tab-separated row per seat (%s)", seat, ErrNoProfileRow, path, ConfigProfileColumns)
	}
	if err != nil {
		return ConfigProfile{}, fmt.Errorf("seat %s: cannot read %s: %v", seat, path, err)
	}
	defer f.Close() // ignored: a read-only file, nothing was written through it
	var found *ConfigProfile
	var known []string
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		p, err := parseConfigProfileRow(line)
		if err != nil {
			return ConfigProfile{}, fmt.Errorf("seat profile %s:%d: %v; a row is tab-separated columns (%s)", path, n, err, ConfigProfileColumns)
		}
		known = append(known, p.Name)
		if p.Name == seat {
			if found != nil {
				return ConfigProfile{}, fmt.Errorf("seat profile %s:%d: a second row for seat %s; keep one", path, n, seat)
			}
			found = &p
		}
	}
	if err := sc.Err(); err != nil {
		return ConfigProfile{}, fmt.Errorf("seat %s: cannot read %s: %v", seat, path, err)
	}
	if found == nil {
		if len(known) > 0 {
			return ConfigProfile{}, fmt.Errorf("unknown seat %s; known seats: %s", seat, strings.Join(known, ", "))
		}
		return ConfigProfile{}, fmt.Errorf("seat %s: %w for it in %s; add one tab-separated row (%s)", seat, ErrNoProfileRow, path, ConfigProfileColumns)
	}
	return *found, nil
}

func parseConfigProfileRow(line string) (ConfigProfile, error) {
	cols := strings.Split(line, "\t")
	if len(cols) != 2 && len(cols) != 3 {
		return ConfigProfile{}, fmt.Errorf("%d columns, want 2 or 3", len(cols))
	}
	for i := range cols {
		cols[i] = strings.TrimSpace(cols[i])
	}
	p := ConfigProfile{Name: cols[0], DSN: cols[1]}
	if len(cols) == 3 {
		p.PasswordEnv = cols[2]
		if p.PasswordEnv != "" && !secretEnvName.MatchString(p.PasswordEnv) {
			return ConfigProfile{}, fmt.Errorf("password env %q must match [A-Z_][A-Z0-9_]*", p.PasswordEnv)
		}
	}
	if !secrets.IsValidAsName(p.Name) {
		return ConfigProfile{}, fmt.Errorf("name %q must match [A-Za-z0-9_-]+", p.Name)
	}
	if p.DSN == "" {
		return ConfigProfile{}, errors.New("dsn is empty")
	}
	return p, nil
}
