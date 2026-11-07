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
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("dashboard: invalid pull address")
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
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		p.failure()
		return fmt.Errorf("dashboard: pull failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		p.failure()
		return fmt.Errorf("dashboard: pull returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, pullBodyLimit+1))
	if err != nil || len(raw) > pullBodyLimit {
		p.failure()
		return fmt.Errorf("dashboard: pull body unavailable or too large")
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
		_ = p.renderLocked()
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
	_ = p.Advance(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_ = p.Advance(ctx)
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

// parsePullEnvelope rejects malformed upstream data before refreshing its age.
func parsePullEnvelope(raw []byte) (map[string]json.RawMessage, float64, error) {
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return nil, 0, fmt.Errorf("dashboard: invalid pull envelope")
	}
	var ok bool
	if string(value["ok"]) == "null" || json.Unmarshal(value["ok"], &ok) != nil {
		return nil, 0, fmt.Errorf("dashboard: invalid pull status")
	}
	var age float64
	if string(value["dataAgeSeconds"]) == "null" || json.Unmarshal(value["dataAgeSeconds"], &age) != nil || age < 0 {
		return nil, 0, fmt.Errorf("dashboard: invalid pull age")
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(value["data"], &data) != nil || data == nil {
		return nil, 0, fmt.Errorf("dashboard: invalid pull data")
	}
	var at string
	if json.Unmarshal(data["at"], &at) != nil {
		return nil, 0, fmt.Errorf("dashboard: invalid pull timestamp")
	}
	if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
		return nil, 0, fmt.Errorf("dashboard: invalid pull timestamp")
	}
	var tables map[string]json.RawMessage
	if json.Unmarshal(data["tables"], &tables) != nil || tables == nil {
		return nil, 0, fmt.Errorf("dashboard: invalid pull tables")
	}
	for _, key := range []string{"fleet", "friends", "merge", "readers", "work"} {
		var table map[string]json.RawMessage
		if json.Unmarshal(tables[key], &table) != nil || table == nil {
			return nil, 0, fmt.Errorf("dashboard: invalid pull table %s", key)
		}
	}
	var streams []json.RawMessage
	if json.Unmarshal(data["streams"], &streams) != nil || streams == nil {
		return nil, 0, fmt.Errorf("dashboard: invalid pull streams")
	}
	var epoch int
	if json.Unmarshal(data["epoch"], &epoch) != nil || epoch < 1 {
		return nil, 0, fmt.Errorf("dashboard: invalid pull epoch")
	}
	return value, age, nil
}
