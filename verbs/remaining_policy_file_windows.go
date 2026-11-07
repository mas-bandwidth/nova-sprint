//go:build windows

package verbs

import (
	"context"
	"fmt"
)

// LocalPolicyFiles fails closed until a bounded Windows file reader is provided.
type LocalPolicyFiles struct{}

func (LocalPolicyFiles) ReadPolicy(context.Context, string, int64) ([]byte, error) {
	return nil, fmt.Errorf("bounded policy reader unavailable")
}
