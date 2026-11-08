package dash

import (
	"io"
	"log"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// errNote logs a recurring step's failure (a poll every second) once per
// distinct error and once when it recovers: never silent, never a flood.
type errNote struct {
	mu   sync.Mutex
	last string
}

func (n *errNote) note(what string, err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	switch {
	case err == nil && n.last != "":
		log.Printf("dashboard: %s recovered", what)
		n.last = ""
	case err != nil && err.Error() != n.last:
		n.last = err.Error()
		log.Printf("dashboard: %s failed: %s", what, edge.RedactN(n.last, edge.CommandMax))
	}
}

// respond writes body to a client; a write that fails (a client gone, a
// connection reset) is logged.
func respond(w io.Writer, body []byte) {
	if _, err := w.Write(body); err != nil {
		log.Printf("dashboard: response write failed: %s", edge.Redact(err.Error()))
	}
}
