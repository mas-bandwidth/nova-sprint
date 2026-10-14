package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/verbflag"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// seatEnvironment is data, never shell code or an identity assertion. Login
// names a secret reference; the existing login reader resolves it in process.
type seatEnvironment struct {
	Version       int               `json:"version"`
	Environment   map[string]string `json:"environment,omitempty"`
	Login         *storeLogin       `json:"login,omitempty"`
	ForgeIdentity string            `json:"forge_identity,omitempty"`
}

func (a *app) seatEnvironmentPath() (string, error) {
	if x := a.getenv("XDG_CONFIG_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "nova-sprint", "seat-environment.json"), nil
	}
	h, err := a.home()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(h) {
		return "", errors.New("seat environment has no absolute home directory")
	}
	return filepath.Join(h, ".config", "nova-sprint", "seat-environment.json"), nil
}

func (a *app) loadSeatEnvironment() error {
	path, err := a.seatEnvironmentPath()
	if err != nil {
		return nil // No usable optional configuration location; explicit settings still work.
	}
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !seatFileOwned(fi, os.Getuid()) {
		return errors.New("unsafe or unowned seat file")
	}
	dir, err := os.Lstat(filepath.Dir(path))
	if err != nil || !seatDirectoryOwned(dir, os.Getuid()) {
		return errors.New("unsafe or unowned seat directory")
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(fi, opened) {
		return errors.New("seat file changed")
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return errors.New("seat file unreadable")
	}
	if err := uniqueJSONKeys(b); err != nil {
		return safeSeatJSONError(err)
	}
	if err := seatJSONFields(b); err != nil {
		return err
	}
	var s seatEnvironment
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return errors.New("seat data has an invalid field type")
	}
	if s.Version != 1 {
		return errors.New("unsupported seat version")
	}
	for k, v := range s.Environment {
		switch k {
		case "NOVA_SPRINT_REDIS", "NOVA_SPRINT_SERVER", "NOVA_BUS_REDIS":
		default:
			return errors.New("unsupported seat default")
		}
		if !seatEndpoint(v) {
			return errors.New("invalid seat endpoint")
		}
	}
	if s.Login != nil {
		if !seatEndpoint(s.Login.Redis) || s.Login.User == "" || s.Login.secrets().Missing() != "" {
			return errors.New("incomplete seat login")
		}
		for _, v := range []string{s.Login.User, s.Login.Store, s.Login.As, s.Login.Key, s.Login.Sops, s.Login.Secret} {
			if strings.ContainsAny(v, "\x00\r\n") {
				return errors.New("invalid seat reference")
			}
		}
	}
	if s.ForgeIdentity != "" {
		if !filepath.IsAbs(s.ForgeIdentity) {
			return errors.New("forge identity must be absolute")
		}
		info, err := os.Stat(s.ForgeIdentity)
		if err != nil || !info.IsDir() {
			return errors.New("forge identity directory missing")
		}
	}
	a.seatEnvironment = &s
	env := a.getenv
	a.getenv = func(k string) string {
		if v := env(k); v != "" {
			return v
		}
		if k == "NOVA_SPRINT_REDIS" {
			if v := env("NOVA_REDIS_ADDR"); v != "" {
				return v
			}
		}
		return s.Environment[k]
	}
	return nil
}

// Keep these rules identical to tools pkg/seatcred.ValidSeatEndpoint.
func seatEndpoint(value string) bool {
	if strings.HasPrefix(value, "mem:") || strings.Contains(value, "://") || strings.ContainsAny(value, "@/?#") {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f || r == '\'' || r == '"' || r == ' ' {
			return false
		}
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil || host == "" {
		return false
	}
	n, err := strconv.ParseUint(port, 10, 16)
	return err == nil && n > 0
}

func safeSeatJSONError(err error) error {
	switch err.Error() {
	case "duplicate key", "trailing data", "invalid JSON":
		return err
	}
	return errors.New("seat data has malformed JSON")
}

// Walk tokens rather than unmarshalling a map, which silently accepts duplicate
// keys. Diagnostics deliberately never include input values.
func uniqueJSONKeys(b []byte) error {
	d := json.NewDecoder(bytes.NewReader(b))
	var value func() error
	value = func() error {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				k, ok := key.(string)
				if !ok || seen[k] {
					return errors.New("duplicate key")
				}
				seen[k] = true
				if err := value(); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := value(); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid JSON")
		}
		_, err = d.Token()
		return err
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing data")
	}
	return nil
}

func seatEnvironmentNeeded(args []string) bool {
	if len(args) == 0 || args[0] == "help" || args[0] == "version" || args[0] == "--version" || verbflag.IsHelp(args[0]) {
		return false
	}
	v := readVerb(args)
	return v.words != 0 && v.err == nil && !v.help
}

func seatFileOwned(fi os.FileInfo, uid int) bool {
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0077 != 0 {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}

func seatJSONFields(b []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	for key, raw := range fields {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.New("null field")
		}
		switch key {
		case "version", "environment", "forge_identity":
		case "login":
			var login map[string]json.RawMessage
			if err := json.Unmarshal(raw, &login); err != nil {
				return err
			}
			for k := range login {
				switch k {
				case "redis", "user", "store", "as", "key", "sops", "secret":
				default:
					return errors.New("unknown login field")
				}
			}
		default:
			return errors.New("unknown field")
		}
	}
	return nil
}

func seatDirectoryOwned(fi os.FileInfo, uid int) bool {
	if !fi.IsDir() || fi.Mode().Perm()&0022 != 0 {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}
