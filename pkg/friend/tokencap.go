package friend

import "sync"

// Tokens is a card's tokens as the harness's own usage record counts them.
type Tokens struct {
	Input, CacheRead, CacheWrite, Output, Reasoning int64
}

// cardRuns is each card's usage over its finished runs, so a card's cap counts every
// run the lane gave it. Keyed by the card's outbox.
type cardRuns struct {
	mu   sync.Mutex
	runs map[string]Tokens
}
