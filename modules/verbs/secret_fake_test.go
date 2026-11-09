package verbs

// sinkFake is a secret store for the role and store tests.

import (
	"context"
	"errors"
	"io"
)

type sinkFake struct {
	saved bool
	fail  bool
	value string
}

func (s *sinkFake) Has(context.Context, string) (bool, error) { return s.saved, nil }
func (s *sinkFake) Set(_ context.Context, _ string, r io.Reader) error {
	if s.fail {
		return errors.New("failed")
	}
	b, _ := io.ReadAll(r)
	s.value = string(b)
	s.saved = true
	return nil
}
