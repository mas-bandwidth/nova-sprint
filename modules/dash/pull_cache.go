package dash

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

const pullBodyLimit = 8 << 20

// PullCache holds one last-good public view; it has no store credentials.
type PullCache struct {
	mu               sync.RWMutex
	client           *http.Client
	url              string
	now              func() time.Time
	base             map[string]json.RawMessage
	baseAge          float64
	fetched          time.Time
	body, compressed []byte
	etag             string
	failed           bool
	alarm            bool
	limiter          *addressLimiter
}

func NewPullCache(raw string, client *http.Client, now func() time.Time) (*PullCache, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("dashboard: invalid pull address: %s", edge.Redact(err.Error()))
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("dashboard: invalid pull address: want http(s)://host[:port]/path with no user, query or fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/sprint"
	if client == nil {
		client = &http.Client{}
	}
	if now == nil {
		now = time.Now
	}
	return &PullCache{client: client, url: u.String(), now: now, limiter: newAddressLimiter()}, nil
}

// Advance fetches once and updates one shared response, including increasing
// age on failure; viewers never trigger upstream reads.
func (p *PullCache) Advance(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return fmt.Errorf("dashboard: pull request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		p.failure()
		return fmt.Errorf("dashboard: pull failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		p.failure()
		return fmt.Errorf("dashboard: pull returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, pullBodyLimit+1))
	if err != nil {
		p.failure()
		return fmt.Errorf("dashboard: pull body unavailable: %w", err)
	}
	if len(raw) > pullBodyLimit {
		p.failure()
		return fmt.Errorf("dashboard: pull body over %d bytes", pullBodyLimit)
	}
	value, age, err := parsePullEnvelope(raw)
	if err != nil {
		p.failure()
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.base = value
	p.baseAge = age
	p.fetched = p.now()
	p.failed = false
	p.checkAlarmLocked()
	return p.renderLocked()
}
func (p *PullCache) failure() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failed = true
	if p.base != nil {
		p.checkAlarmLocked()
		if err := p.renderLocked(); err != nil {
			log.Printf("dashboard: rendering the stale page after a failed pull failed: %s", edge.Redact(err.Error()))
		}
	}
}
func (p *PullCache) renderLocked() error {
	elapsed := p.now().Sub(p.fetched).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	age, _ := json.Marshal(p.baseAge + elapsed)
	p.base["dataAgeSeconds"] = age
	if p.failed {
		p.base["ok"] = json.RawMessage("false")
		p.base["error"] = json.RawMessage(`"upstream pull failed"`)
	}
	body, err := json.Marshal(p.base)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	z := gzip.NewWriter(&out)
	if _, err = z.Write(body); err != nil {
		return err
	}
	if err = z.Close(); err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	p.body = body
	p.compressed = out.Bytes()
	p.etag = fmt.Sprintf("\"%x\"", sum[:])
	return nil
}

// Run polls once a second regardless of viewer count.
func (p *PullCache) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var pull errNote
	pull.note("the pull", p.Advance(ctx))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			pull.note("the pull", p.Advance(ctx))
		}
	}
}

func (p *PullCache) checkAlarmLocked() {
	age := p.baseAge + p.now().Sub(p.fetched).Seconds()
	if age >= 32 && !p.alarm {
		log.Printf("ALARM stale: public dashboard data is %.1fs old", age)
		p.alarm = true
	}
	if age < 32 {
		p.alarm = false
	}
}

// parsePullEnvelope decodes upstream data as the board's own PageEnvelope
// and refuses it before refreshing its age unless it carries a board (data
// with its time and an epoch of at least 1) and an age that is not negative;
// a refusal names the parse's byte and the first bytes it saw.
func parsePullEnvelope(raw []byte) (map[string]json.RawMessage, float64, error) {
	var env PageEnvelope
	if err := pullField(raw, &env, "envelope"); err != nil {
		return nil, 0, err
	}
	var value map[string]json.RawMessage
	if err := pullField(raw, &value, "envelope"); err != nil {
		return nil, 0, err
	}
	switch {
	case value == nil || env.Data.At.IsZero() || env.Data.Epoch < 1:
		return nil, 0, pullRefused("data", "no board: want data with its at and an epoch of at least 1", raw)
	case env.DataAgeSeconds < 0:
		return nil, 0, pullRefused("age", fmt.Sprintf("dataAgeSeconds %g is negative", env.DataAgeSeconds), value["dataAgeSeconds"])
	}
	return value, env.DataAgeSeconds, nil
}

// pullField decodes raw into v; a failure names what and where it failed.
func pullField(raw json.RawMessage, v any, what string) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("dashboard: invalid pull %s: %s", what, edge.ParseFailure(err, raw))
	}
	return nil
}

// pullRefused is a field that decoded but is not acceptable.
func pullRefused(what, why string, raw []byte) error {
	return fmt.Errorf("dashboard: invalid pull %s: %s", what, edge.ParseRefused(why, 0, raw))
}
