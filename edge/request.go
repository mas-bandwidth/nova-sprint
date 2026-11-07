package edge

import (
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
	Epoch                                   int64
	ID, Kind, Card, Row, Stream, Repo, Base string
	PR                                      bool
	Mode                                    string
	Ledgers                                 []string
	SHA, BaseSHA                            string
	Heads, After                            []string
}

func (r EdgeRequest) Key() string {
	if r.ID != "" {
		return r.ID
	}
	return RequestID(r.Epoch, r.Kind, r.Card, r.SHA)
}

type EdgeResult struct {
	Epoch                                                     int64
	ID, Kind, Card, SHA, Outcome, Failure, BaseTip, StreamTip string
	Landed                                                    []string
	Blamed                                                    string
	Files                                                     []string
	PRNumber                                                  int
	Reason                                                    string
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
	fillEdgeResultMeta_internal(&res, req, key)
	d.mu.Lock()
	call.res = res
	call.err = err
	delete(d.inflight, key)
	close(call.done)
	d.mu.Unlock()
	return res, err
}

func fillEdgeResultMeta_internal(res *EdgeResult, req EdgeRequest, key string) {
	if res.Epoch == 0 {
		res.Epoch = req.Epoch
	}
	if res.ID == "" {
		res.ID = key
	}
	if res.Kind == "" {
		res.Kind = req.Kind
	}
	if res.Card == "" {
		res.Card = req.Card
	}
	if res.SHA == "" {
		res.SHA = req.SHA
	}
}
