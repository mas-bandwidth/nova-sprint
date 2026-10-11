package swarm

import (
	"bytes"
	"fmt"
	"strings"
)

// quoteDepends names each refused entry, so the drift says which token it refused.
func quoteDepends(ids []string) string {
	q := make([]string, len(ids))
	for i, id := range ids {
		q[i] = fmt.Sprintf("%q", id)
	}
	return strings.Join(q, ", ")
}

// firstLine is a text's first line, without its line ending: the contract line the
// card's own id and base are read from.
func firstLine(raw []byte) string {
	line, _, _ := bytes.Cut(raw, []byte("\n"))
	return strings.TrimRight(string(line), "\r")
}
