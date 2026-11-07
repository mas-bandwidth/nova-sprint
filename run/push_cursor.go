package run

import (
	"context"
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
func (p *Pusher) resolve(ctx context.Context, recipient string, item pushPending) error {
	r, found, err := p.store.Replay(ctx, item.op.ID)
	if err != nil {
		return fmt.Errorf("run: replay push cursor: %w", err)
	}
	if !found {
		r, err = p.store.Apply(ctx, item.op)
		if err != nil {
			return fmt.Errorf("run: save push cursor: %w", err)
		}
	}
	delete(p.pending, recipient)
	if !r.Accepted {
		delete(p.views, recipient)
		return fmt.Errorf("run: push cursor refused: %s", r.Reason)
	}
	p.views[recipient] = item.position
	return nil
}

func validDeliveryID(id string) bool {
	a, b, ok := strings.Cut(id, "-")
	digits := func(s string) bool { return s != "" && strings.Trim(s, "0123456789") == "" }
	return ok && digits(a) && digits(b)
}
