package silent

import "errors"

func f(g func() error) error {
	if err := g(); err != nil {
		return errors.New("failed")
	}
	return nil
}
