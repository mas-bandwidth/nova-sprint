//go:build darwin

package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mas-bandwidth/nova-tools/pkg/secrets"
)

// securityTool is macOS's keychain command; its absolute path, so no PATH entry stands in.
const securityTool = "/usr/bin/security"

// keychainPassword is the generic password of (service, account) in the login keychain,
// read through `security find-generic-password -w`: its stdout is captured in process
// and its stderr is never shown, so no value reaches any output. A missing item is a
// refusal naming the service, the account and the remedy.
func keychainPassword(service, account string) (secrets.Secret, error) {
	cmd := exec.Command(securityTool, "find-generic-password", "-s", service, "-a", account, "-w")
	var out, discard bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &discard
	if err := cmd.Run(); err != nil {
		return secrets.Secret{}, fmt.Errorf("%s=%s: the keychain gave no generic password for service %s and account %s (%s: %v); run: security add-generic-password -s %s -a %s -w",
			passwordKeychainEnv, service, service, account, securityTool, err, service, account)
	}
	v := strings.TrimSuffix(out.String(), "\n")
	if v == "" {
		return secrets.Secret{}, fmt.Errorf("%s=%s: the keychain item for account %s is empty; run: security add-generic-password -U -s %s -a %s -w", passwordKeychainEnv, service, account, service, account)
	}
	return secrets.NewSecret(v), nil
}
