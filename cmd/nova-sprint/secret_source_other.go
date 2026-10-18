//go:build !darwin

package main

import (
	"fmt"

	"github.com/mas-bandwidth/nova-tools/pkg/secrets"
)

// keychainPassword is refused off macOS: there is no keychain to read, and the file
// source is the one to name.
func keychainPassword(service, account string) (secrets.Secret, error) {
	return secrets.Secret{}, fmt.Errorf("%s=%s names a macOS keychain item, and this system has no keychain; name a 0600 file with %s instead", passwordKeychainEnv, service, passwordFileEnv)
}
