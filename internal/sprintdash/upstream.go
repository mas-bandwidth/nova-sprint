package sprintdash

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// upstreamLimit bounds an upstream answer: a sprint's copy is tens of kilobytes.
const upstreamLimit = 64 << 20

// Upstream is a Read that takes the sprint from another dashboard's /api/sprint at url
// (server.py's DASHBOARD_UPSTREAM): its data, so a second dashboard on the same machine
// adds no read of the sprint. An answer that cannot be had or read is "upstream
// unreachable"; one with no data (null, or empty) is "upstream has no data". client
// bounds the read (its Timeout).
func Upstream(url string, client *http.Client) func() ([]byte, error) {
	return func() ([]byte, error) {
		resp, err := client.Get(url)
		if err != nil {
			return nil, errors.New("upstream unreachable")
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, upstreamLimit))
		var v struct {
			Data json.RawMessage `json:"data"`
		}
		if err != nil || resp.StatusCode != http.StatusOK || json.Unmarshal(body, &v) != nil {
			return nil, errors.New("upstream unreachable")
		}
		switch string(bytes.TrimSpace(v.Data)) {
		case "", "null", "{}", "[]", `""`, "0", "false":
			return nil, errors.New("upstream has no data")
		}
		return v.Data, nil
	}
}
