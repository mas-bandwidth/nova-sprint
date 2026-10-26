package main

// The store password's sources beside the seat login (docs/SPEC-SPRINT.md, "The seat's
// store login"): NOVA_SPRINT_REDIS_PASSWORD_FILE names a file of this user's alone that
// holds it, and on macOS NOVA_SPRINT_REDIS_PASSWORD_KEYCHAIN names the keychain service
// whose generic password it is. Both variables are pointers and carry no secret; the
// value is read once per process into a secrets.Secret and handed to redisconn through
// the getenv closure storeOptions returns, as the seat login's is, so it is never in
// argv or in any environment.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/redisauth"
	"github.com/mas-bandwidth/nova-tools/pkg/secrets"
)

const (
	// passwordFileEnv names the 0600 file the store password is read from.
	passwordFileEnv = "NOVA_SPRINT_REDIS_PASSWORD_FILE"
	// passwordKeychainEnv names the macOS keychain service the store password is read from.
	passwordKeychainEnv = "NOVA_SPRINT_REDIS_PASSWORD_KEYCHAIN"
	// sourcePassword names the file's or the keychain's password to redisconn: the getenv
	// storeOptions hands it answers this name from the value read in process.
	sourcePassword = "NOVA_SPRINT_SOURCE_PASSWORD"
	// passwordFileMax bounds the read: a password, not a file of any size.
	passwordFileMax = 64 << 10
)

// sourceSecret is the store password the file or the keychain names, read once per
// process: ok is false when neither variable is set. Two sources named at once, or a
// source beside NOVA_SPRINT_REDIS_PASSWORD_ENV, is a refusal naming the variables,
// never a guess at which one is meant.
func (a *app) sourceSecret() (secrets.Secret, bool, error) {
	file, service := a.getenv(passwordFileEnv), a.getenv(passwordKeychainEnv)
	if file == "" && service == "" {
		return secrets.Secret{}, false, nil
	}
	a.sourceMu.Lock()
	defer a.sourceMu.Unlock()
	if a.sourceRead.Loaded() {
		return a.sourceRead, true, nil
	}
	var s secrets.Secret
	var err error
	switch {
	case file != "" && service != "":
		err = fmt.Errorf("%s and %s are both set; name one source of the store password: unset one of them", passwordFileEnv, passwordKeychainEnv)
	case a.getenv(redisauth.PasswordEnvEnv) != "":
		err = fmt.Errorf("%s and a password source (%s or %s) are both set; name one source of the store password: unset %s", redisauth.PasswordEnvEnv, passwordFileEnv, passwordKeychainEnv, redisauth.PasswordEnvEnv)
	case file != "":
		s, err = readPasswordFile(file)
	default:
		user := a.getenv(redisauth.UserEnv)
		if user == "" {
			err = fmt.Errorf("%s=%s needs %s, the ACL user whose password it holds; set %s", passwordKeychainEnv, service, redisauth.UserEnv, redisauth.UserEnv)
			break
		}
		read := a.keychain
		if read == nil {
			read = keychainPassword
		}
		s, err = read(service, user)
	}
	if err != nil {
		return secrets.Secret{}, false, err
	}
	a.sourceRead = s
	return s, true, nil
}

// readPasswordFile is the password in the file at path, one trailing newline cut. The
// file must be a regular file (not a link), owned by this user, that neither group nor
// others can read or write; anything else is refused naming the path and the remedy,
// never the content.
func readPasswordFile(path string) (secrets.Secret, error) {
	if !filepath.IsAbs(path) {
		return secrets.Secret{}, fmt.Errorf("%s=%s is not an absolute path; name the file whole", passwordFileEnv, path)
	}
	remedy := "; run: chmod 600 " + path + " (a regular file of this user's, not a link)"
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ELOOP) {
		return secrets.Secret{}, fmt.Errorf("the password file %s is a symbolic link, which is refused; name the file itself%s", path, remedy)
	}
	if err != nil {
		return secrets.Secret{}, fmt.Errorf("the password file %s cannot be opened: %v", path, pathless(err))
	}
	defer func() { _ = f.Close() }() // ignored: read only, and read whole below
	fi, err := f.Stat()
	if err != nil {
		return secrets.Secret{}, fmt.Errorf("the password file %s cannot be read: %v", path, pathless(err))
	}
	if !fi.Mode().IsRegular() {
		return secrets.Secret{}, fmt.Errorf("the password file %s is not a regular file (%s)%s", path, fi.Mode().Type(), remedy)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Getuid() {
		return secrets.Secret{}, fmt.Errorf("the password file %s is not owned by this user (uid %d)%s", path, os.Getuid(), remedy)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return secrets.Secret{}, fmt.Errorf("the password file %s has mode %04o, which lets others than its owner at it%s", path, fi.Mode().Perm(), remedy)
	}
	b, err := io.ReadAll(io.LimitReader(f, passwordFileMax+1))
	if err != nil {
		return secrets.Secret{}, fmt.Errorf("the password file %s cannot be read: %v", path, pathless(err))
	}
	if len(b) > passwordFileMax {
		return secrets.Secret{}, fmt.Errorf("the password file %s is larger than %d bytes, which is no password; name the file that holds the password alone", path, passwordFileMax)
	}
	v := strings.TrimSuffix(string(b), "\n")
	if v == "" {
		return secrets.Secret{}, fmt.Errorf("the password file %s is empty; write the password into it (one line)", path)
	}
	return secrets.NewSecret(v), nil
}

// pathless is err's cause without the path an *os.PathError repeats.
func pathless(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
