package main

// The startup scrub (docs/SPEC-SPRINT.md, "The seat's store login"): main's first act. A
// store password a launcher put in the environment (nova-secrets exec --only <NAME>) is
// read into the process, the variable is unset, so no child the process starts inherits
// it, and the app's getenv answers the name from the value held. os.Unsetenv does not
// clear the environment block the kernel keeps from exec, which ps -E and
// /proc/<pid>/environ read, so a long-running verb that removed a value re-execs itself
// once with the scrubbed environment, handing the values across on an inherited pipe
// whose descriptor NOVA_SPRINT_SECRET_FD names (a number, never a value).

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/redisauth"
	"github.com/mas-bandwidth/nova-tools/pkg/secrets"
)

const (
	// secretFDEnv names the descriptor a re-exec'd process reads its held values from.
	secretFDEnv = "NOVA_SPRINT_SECRET_FD"
	// coordinatorPasswordEnv is the coordinator login's password variable.
	coordinatorPasswordEnv = "NOVA_REDIS_COORDINATOR_PASSWORD"
	// heldMax bounds what crosses the pipe: it is filled before the exec with no reader,
	// so it must fit the pipe's buffer (16 KiB at the least on Linux and macOS).
	heldMax = 4096
)

// heldSecrets is the password-bearing variables the scrub removed, by name.
type heldSecrets map[string]secrets.Secret

// processHeld is what this process's startup scrub removed: set once by startupScrub,
// main's first act and before any goroutine, and read by the children that open the
// store with this process's environment login (childStoreEnv).
var processHeld heldSecrets

// childStoreEnv is environ for a nova-sprint child that opens the store with this
// process's environment login (NOVA_SPRINT_REDIS_USER / NOVA_SPRINT_REDIS_PASSWORD_ENV)
// after the scrub took its password out of the environment (docs/SPEC-SPRINT.md, "The
// seat's store login"): the held password is written to a 0600 file in a fresh 0700
// directory under tmp ("" is the system's), and the child is pointed at it with
// NOVA_SPRINT_REDIS_PASSWORD_FILE in place of NOVA_SPRINT_REDIS_PASSWORD_ENV, so the
// value is in neither the child's environment nor its argv. done removes the directory;
// call it once the child has exited. Any other login (a password file or keychain
// already named, the seat login, the default user, a password not held) is environ
// unchanged, as the child reads that login itself.
func childStoreEnv(environ []string, held heldSecrets, tmp string) (env []string, done func(), err error) {
	done = func() {}
	get := func(name string) string {
		v := ""
		for _, kv := range environ {
			if k, x, ok := strings.Cut(kv, "="); ok && k == name {
				v = x
			}
		}
		return v
	}
	if get(passwordFileEnv) != "" || get(passwordKeychainEnv) != "" {
		return environ, done, nil
	}
	named, user := get(redisauth.PasswordEnvEnv), get(redisauth.UserEnv)
	if named == "" && user == "" {
		return environ, done, nil
	}
	name := named
	if name == "" {
		name = redisauth.DefaultPasswordEnv // storeOptions: a user with no password name
	}
	s, ok := held[name]
	if !ok {
		return environ, done, nil
	}
	dir, err := os.MkdirTemp(tmp, "nova-sprint-login-") // mode 0700
	if err != nil {
		return nil, done, fmt.Errorf("the child's store password file has no directory: %v", pathless(err))
	}
	path := filepath.Join(dir, "store-password")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		err = s.Use(func(p string) error { _, werr := f.WriteString(p + "\n"); return werr })
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		_ = os.RemoveAll(dir) // ignored: the write's error is the one returned
		return nil, done, fmt.Errorf("the child's store password file %s could not be written: %v", path, pathless(err))
	}
	for _, kv := range environ {
		if k, _, _ := strings.Cut(kv, "="); k == redisauth.PasswordEnvEnv || k == passwordFileEnv {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, passwordFileEnv+"="+path)
	return env, func() { _ = os.RemoveAll(dir) }, nil // ignored: a temporary directory of ours
}

// passwordName says the variable name bears a password value: the one named holds
// (the variable NOVA_SPRINT_REDIS_PASSWORD_ENV names), the bench and coordinator
// password variables, and any NOVA_*_PASSWORD. The pointers (…_PASSWORD_ENV,
// …_PASSWORD_FILE, …_PASSWORD_KEYCHAIN) carry no secret and are not.
func passwordName(name, named string) bool {
	switch {
	case name == "":
		return false
	case name == named, name == redisauth.DefaultPasswordEnv, name == coordinatorPasswordEnv:
		return true
	}
	return strings.HasPrefix(name, "NOVA_") && strings.HasSuffix(name, "_PASSWORD")
}

// scrubEnv reads every password-bearing variable of environ (NAME=value pairs) into the
// holder and unsets it with unset. A variable set empty is unset and not held.
func scrubEnv(environ []string, unset func(string) error) (heldSecrets, error) {
	named := ""
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok && k == redisauth.PasswordEnvEnv {
			named = v
		}
	}
	held := heldSecrets{}
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !passwordName(k, named) {
			continue
		}
		if err := unset(k); err != nil {
			return nil, fmt.Errorf("the variable %s could not be unset: %v", k, err)
		}
		if v != "" {
			held[k] = secrets.NewSecret(v)
		}
	}
	return held, nil
}

// getenv is base with every held name answered from its value.
func (h heldSecrets) getenv(base func(string) string) func(string) string {
	if len(h) == 0 {
		return base
	}
	return func(k string) string {
		s, ok := h[k]
		if !ok {
			return base(k)
		}
		v := ""
		_ = s.Use(func(p string) error { v = p; return nil }) // ignored: the function never fails
		return v
	}
}

// encode is the held values as NAME=value pairs, each ended by a NUL (no value of an
// environment can hold one), in name order.
func (h heldSecrets) encode() []byte {
	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, k)
	}
	sort.Strings(names)
	var b bytes.Buffer
	for _, k := range names {
		_ = h[k].Use(func(v string) error { // ignored: the function never fails
			b.WriteString(k + "=" + v + "\x00")
			return nil
		})
	}
	return b.Bytes()
}

// decodeHeld is encode's inverse.
func decodeHeld(b []byte) (heldSecrets, error) {
	h := heldSecrets{}
	for _, kv := range strings.Split(string(b), "\x00") {
		if kv == "" {
			continue
		}
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, errors.New("the handed values are not NAME=value pairs")
		}
		h[k] = secrets.NewSecret(v)
	}
	return h, nil
}

// longRunning says the verb words run for long: run, dashboard, watch, wait and play, and
// any verb given --watch, --wait or --every (the loops: answer, friend sync, promote,
// snapshot, where). A short-lived verb needs no re-exec: it ends before the kernel's copy
// matters.
func longRunning(args []string) bool {
	if len(args) > 0 {
		switch args[0] {
		case "run", "dashboard", "watch", "wait", "play":
			return true
		}
	}
	for _, w := range args {
		if w == "--" {
			return false
		}
		n := strings.TrimLeft(w, "-")
		n, _, _ = strings.Cut(n, "=")
		if w != n && (n == "watch" || n == "wait" || n == "every") {
			return true
		}
	}
	return false
}

// startupScrub is main's first act: the values a re-exec handed over (secretFDEnv), else
// the environment scrubbed, and for a long-running verb that removed a value the one
// re-exec, which does not return when it works. It returns the getenv the app reads.
func startupScrub(args []string) (func(string) string, error) {
	if fd := os.Getenv(secretFDEnv); fd != "" {
		h, err := readHanded(fd)
		_ = os.Unsetenv(secretFDEnv) // ignored: Unsetenv fails only on a bad name
		if err != nil {
			return nil, err
		}
		rest, err := scrubEnv(os.Environ(), os.Unsetenv)
		if err != nil {
			return nil, err
		}
		for k, v := range rest {
			h[k] = v
		}
		processHeld = h
		return h.getenv(os.Getenv), nil
	}
	h, err := scrubEnv(os.Environ(), os.Unsetenv)
	if err != nil {
		return nil, err
	}
	if len(h) > 0 && longRunning(args) {
		return nil, reexecHolding(h)
	}
	processHeld = h
	return h.getenv(os.Getenv), nil
}

// readHanded reads and closes the descriptor fd names, the values the process before
// the re-exec held.
func readHanded(fd string) (heldSecrets, error) {
	n, err := strconv.Atoi(fd)
	if err != nil || n < 3 {
		return nil, fmt.Errorf("%s=%s is not a descriptor a re-exec hands over; unset it", secretFDEnv, fd)
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(n, &st); err != nil || st.Mode&syscall.S_IFMT != syscall.S_IFIFO {
		return nil, fmt.Errorf("%s=%s is not the pipe a re-exec hands over; unset it", secretFDEnv, fd)
	}
	f := os.NewFile(uintptr(n), "handed-secrets")
	defer func() { _ = f.Close() }() // ignored: read whole below
	b, err := io.ReadAll(io.LimitReader(f, heldMax+1))
	if err != nil {
		return nil, fmt.Errorf("the values handed on descriptor %d could not be read: %v", n, err)
	}
	return decodeHeld(b)
}

// reexecHolding execs this binary again with the same argv and the scrubbed environment,
// the held values on a pipe it inherits: the write end is filled and closed first, so
// the new process reads them whole with no writer left. It returns only on a failure.
func reexecHolding(h heldSecrets) error {
	b := h.encode()
	if len(b) > heldMax {
		return fmt.Errorf("the store passwords in the environment are larger than %d bytes, which no re-exec hands over; name a 0600 file with %s instead", heldMax, passwordFileEnv)
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("the re-exec that clears the store password from this process's environment has no binary: %v; name a 0600 file with %s instead", err, passwordFileEnv)
	}
	var p [2]int
	if err := syscall.Pipe(p[:]); err != nil { // no close-on-exec: the read end is inherited
		return fmt.Errorf("the re-exec's pipe could not be made: %v", err)
	}
	syscall.CloseOnExec(p[1])
	w := os.NewFile(uintptr(p[1]), "handed-secrets-w")
	_, werr := w.Write(b)
	if cerr := w.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = syscall.Close(p[0]) // ignored: the write's error is the one returned
		return fmt.Errorf("the re-exec's pipe could not be filled: %v", werr)
	}
	env := append(os.Environ(), secretFDEnv+"="+strconv.Itoa(p[0]))
	err = syscall.Exec(exe, os.Args, env)
	_ = syscall.Close(p[0]) // ignored: the exec's error is the one returned
	return fmt.Errorf("the re-exec that clears the store password from this process's environment failed: %v; name a 0600 file with %s instead", err, passwordFileEnv)
}
