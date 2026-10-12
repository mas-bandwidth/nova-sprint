package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// CheckInvariant1 checks .sops.yaml syntax and shape against the declared recovery key.
func CheckInvariant1(storeDir string, sopsCfg *SopsConfig, recoveryKey string) []CheckFailure {
	var failures []CheckFailure
	if sopsCfg == nil || len(sopsCfg.CreationRules) == 0 {
		failures = append(failures, CheckFailure{
			Kind:   "rule-shape",
			File:   ".sops.yaml",
			Reason: "carries no creation_rules",
		})
		return failures
	}

	for _, rule := range sopsCfg.CreationRules {
		label := rule.PathRegex
		if label == "" {
			label = "unnamed"
		}

		if !strings.HasPrefix(rule.PathRegex, "^") || !strings.HasSuffix(rule.PathRegex, "$") {
			failures = append(failures, CheckFailure{
				Kind:   "rule-shape",
				File:   ".sops.yaml",
				Reason: fmt.Sprintf("rule path_regex %q is not anchored at both ends (^...$)", rule.PathRegex),
			})
		} else if _, err := regexp.Compile(rule.PathRegex); err != nil {
			failures = append(failures, CheckFailure{
				Kind:   "rule-shape",
				File:   ".sops.yaml",
				Reason: fmt.Sprintf("rule path_regex %q is not a valid regular expression: %v", rule.PathRegex, err),
			})
		}

		// Check non-age recipients
		for _, nonAge := range rule.NonAgeRecipients {
			failures = append(failures, CheckFailure{
				Kind:   "rule-shape",
				File:   ".sops.yaml",
				Reason: fmt.Sprintf("rule for %s carries non-age recipient key %q; only age recipients are permitted", label, nonAge),
			})
		}

		if problem := ruleRecipientsProblem(rule.Recipients, recoveryKey); problem != "" {
			failures = append(failures, CheckFailure{
				Kind:   "rule-shape",
				File:   ".sops.yaml",
				Reason: fmt.Sprintf("rule for %s %s", label, problem),
			})
		}
	}

	return failures
}

// ruleRecipientsProblem is the one judgement of a seat's recipients: exactly two valid
// age public keys, distinct, one of them the declared recovery key, so the other is the
// seat's own key.
// check (CheckInvariant1) and the store's gate (RunGate) hold a creation rule's
// recipients to it, seat inject (seatInjectTarget) the seat file's own, and seat add
// (RunSeatAdd) the rule it is about to write. It returns why they are not, worded to
// follow its subject ("rule ...", "seat file ..."), or "" when they are. An empty
// recoveryKey is one check could not read (recovery.pub is reported on its own), and
// the recovery key is then not looked for.
func ruleRecipientsProblem(recipients []string, recoveryKey string) string {
	if len(recipients) != 2 {
		return fmt.Sprintf("has %d recipients; expected exactly 2 (one seat key and declared recovery key)", len(recipients))
	}
	for _, r := range recipients {
		if !IsValidAgePublicKey(r) {
			return fmt.Sprintf("recipient %q is not a valid age public key", r)
		}
	}
	if recipients[0] == recipients[1] {
		return fmt.Sprintf("has duplicate recipient %s; a seat is one seat key and the declared recovery key, distinct", recipients[0])
	}
	if recoveryKey != "" && !slices.Contains(recipients, recoveryKey) {
		return fmt.Sprintf("does not contain declared recovery key %s (recovery.pub)", recoveryKey)
	}
	return ""
}

// CheckInvariant6 verifies that the key file mode is 0600 and directory is 0700.
func CheckInvariant6(keyPath string) error {
	fi, err := os.Stat(keyPath)
	if err != nil {
		return fmt.Errorf("key file %s: %w", keyPath, err)
	}
	if fi.Mode().Perm() != 0600 {
		return fmt.Errorf("key file %s mode is %04o; expected 0600; run: chmod 600 %s", keyPath, fi.Mode().Perm(), keyPath)
	}

	dir := filepath.Dir(keyPath)
	dirFi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("key directory %s: %w", dir, err)
	}
	if dirFi.Mode().Perm() != 0700 {
		return fmt.Errorf("key directory %s mode is %04o; expected 0700; run: chmod 700 %s", dir, dirFi.Mode().Perm(), dir)
	}
	return nil
}
