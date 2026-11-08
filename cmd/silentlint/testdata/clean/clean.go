package clean

import "fmt"

func f(g func() error) error {
	if err := g(); err != nil {
		return fmt.Errorf("g: %w", err)
	}
	return nil
}
