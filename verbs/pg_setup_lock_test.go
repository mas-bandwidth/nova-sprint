package verbs

import (
	"context"
	"errors"
	"testing"
)

func TestSetupLockMissingDSNRefusesWithoutConnecting(t *testing.T) {
	_, err := (PGSetupLock{}).Acquire(context.Background())
	var op *OperationError
	if !errors.As(err, &op) || op.Problem != "NOVA_PG_DSN is missing" || op.Fix != "nova-sprint check" {
		t.Fatal(err)
	}
}
