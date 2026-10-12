package swarm

import (
	"encoding/hex"
)

// LispCacheDir is the shared ASDF output directory under a swarm root.

// JobLispCacheDir is the private writable overlay for one card. It lives beside the
// card's checkout, inside the already-owned job directory, and is reaped with that job.

// JobASDFOutputTranslations sends a card's writes to its own overlay. The overlay is
// seeded from the exact-tip prewarm cache before launch; cards never write that seed or
// another card's compiled output.

// PrepareLispJobCache makes a private copy-on-write seed for one card. A missing prewarm
// seed is valid and produces an empty overlay; the card will compile there without
// contaminating any sibling.

// ignored: a deferred cleanup of this call's own temp directory, below its parent

func fullHexSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
