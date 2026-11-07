package edge

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"sync"
)

const (
	FactCI     = "ci"
	FactLand   = "land"
	FactRunner = "runner"
)

func RequestID(epoch int64, kind, card, sha string) string {
	sum := sha256.Sum256([]byte(strconv.FormatInt(epoch, 10) + "\x00" + kind + "\x00" + card + "\x00" + sha))
	return "req-" + hex.EncodeToString(sum[:12])
}

type EdgeRequest struct {
	Epoch                                              int64
	ID, Kind, Card, Row, Stream, Repo, Base, Mode, SHA string
	BaseSHA                                            string
	PR                                                 bool
	Ledgers, Heads, After                              []string
}

func (r EdgeRequest) Key() string { return cmp.Or(r.ID, RequestID(r.Epoch, r.Kind, r.Card, r.SHA)) }

type EdgeResult struct {
	Epoch                                                                     int64
	ID, Kind, Card, SHA, Outcome, Failure, BaseTip, StreamTip, Blamed, Reason string
	Landed, Files                                                             []string
	PRNumber                                                                  int
}
type inflightCall_internal struct {
	done chan struct{}
	res  EdgeResult
	err  error
}
type Deduplicator struct {
	mu       sync.Mutex
	inflight map[string]*inflightCall_internal
}

func NewDeduplicator() *Deduplicator {
	return &Deduplicator{inflight: make(map[string]*inflightCall_internal)}
}
func (d *Deduplicator) Do(ctx context.Context, req EdgeRequest, fn func(context.Context, EdgeRequest) (EdgeResult, error)) (EdgeResult, error) {
	key := req.Key()
	req.ID = key
	d.mu.Lock()
	if call, ok := d.inflight[key]; ok {
		d.mu.Unlock()
		select {
		case <-call.done:
			return call.res, call.err
		case <-ctx.Done():
			return EdgeResult{Epoch: req.Epoch, ID: key, Kind: req.Kind, Card: req.Card, SHA: req.SHA}, ctx.Err()
		}
	}
	call := &inflightCall_internal{done: make(chan struct{})}
	d.inflight[key] = call
	d.mu.Unlock()
	res, err := fn(ctx, req)
	res.Epoch, res.ID, res.Kind = cmp.Or(res.Epoch, req.Epoch), cmp.Or(res.ID, key), cmp.Or(res.Kind, req.Kind)
	res.Card, res.SHA = cmp.Or(res.Card, req.Card), cmp.Or(res.SHA, req.SHA)
	d.mu.Lock()
	call.res, call.err = res, err
	delete(d.inflight, key)
	close(call.done)
	d.mu.Unlock()
	return res, err
}
