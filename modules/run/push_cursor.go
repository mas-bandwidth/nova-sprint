package run

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Long bus names remain addressable without colliding with literal push: keys.
func pushCursorName(recipient string) (string, error) {
	if recipient == "" || len(recipient) > 128 || strings.Trim(recipient, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-") != "" {
		return "", fmt.Errorf("run: recipient name is empty or invalid")
	}
	if len("push:"+recipient) <= 128 {
		return "push:" + recipient, nil
	}
	sum := sha256.Sum256([]byte(recipient))
	return fmt.Sprintf("push-hash:%x", sum), nil
}
