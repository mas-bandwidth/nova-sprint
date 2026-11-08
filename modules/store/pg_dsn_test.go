package store

import (
	"context"
	"strings"
	"testing"
)

// A DSN that does not parse says why, never quoting its password.
func TestOpenNamesWhyTheDSNDidNotParse(t *testing.T) {
	t.Parallel()
	for _, dsn := range []string{"postgres://u:hunter2@host:notaport/db", "host=h password=hunter2 port=x"} {
		_, err := Open(context.Background(), dsn, "")
		if err == nil || !strings.HasPrefix(err.Error(), "store: the postgres dsn could not be parsed: ") || strings.Contains(err.Error(), "hunter2") {
			t.Fatalf("%q: %v", dsn, err)
		}
		_, err = CheckSchema(context.Background(), dsn)
		if err == nil || strings.Contains(err.Error(), "hunter2") || !strings.Contains(err.Error(), "could not be parsed: ") {
			t.Fatalf("%q: %v", dsn, err)
		}
	}
}
