package dash

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type pullTransport struct {
	fail  bool
	calls int
}

func (p *pullTransport) RoundTrip(*http.Request) (*http.Response, error) {
	p.calls++
	if p.fail {
		return nil, errors.New("offline")
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"data":{"at":"1970-01-01T00:16:40Z","tables":{"fleet":{},"friends":{},"merge":{},"readers":{},"work":{}},"streams":[],"epoch":1},"dataAgeSeconds":0.5}`)), Header: http.Header{}}, nil
}
func TestPullCacheHoldsViewAndAgesOnFailure(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0)
	transport := &pullTransport{}
	p, err := NewPullCache("http://tailnet:8080", &http.Client{Transport: transport}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Advance(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := p.Handler()
	req := httptest.NewRequest("GET", "/api/sprint", nil)
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=1" {
		t.Fatalf("status=%d headers=%v", rec.Code, rec.Header())
	}
	etag := rec.Header().Get("ETag")
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 304 {
		t.Fatalf("etag status=%d", rec.Code)
	}
	now = now.Add(3 * time.Second)
	transport.fail = true
	if err = p.Advance(context.Background()); err == nil {
		t.Fatal("expected pull error")
	}
	req.Header.Del("If-None-Match")
	req.Header.Set("Accept-Encoding", "gzip")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	z, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	_ = z.Close()
	var envelope map[string]any
	if err = json.Unmarshal(decoded, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["ok"] != false || envelope["dataAgeSeconds"] != 3.5 || envelope["data"] == nil || transport.calls != 2 {
		t.Fatalf("envelope=%v calls=%d", envelope, transport.calls)
	}
	now = now.Add(30 * time.Second)
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest("GET", "/healthz", nil))
	if health.Code != 503 {
		t.Fatalf("health=%d", health.Code)
	}
}
func TestPullCacheRejectsWritesAndLimitsAddress(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0)
	p, err := NewPullCache("http://tailnet", &http.Client{Transport: &pullTransport{}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Advance(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := p.Handler()
	request := httptest.NewRequest("POST", "/api/sprint", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request)
	if rec.Code != 405 {
		t.Fatalf("post=%d", rec.Code)
	}
	for i := 0; i < addressLimit+1; i++ {
		request = httptest.NewRequest("GET", "/api/sprint", nil)
		request.RemoteAddr = "127.0.0.1:1"
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, request)
	}
	if rec.Code != 429 {
		t.Fatalf("rate=%d", rec.Code)
	}
	now = now.Add(time.Second)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, request)
	if rec.Code != 200 {
		t.Fatalf("reset=%d", rec.Code)
	}
}

func TestPullRejectsMalformedEnvelopes(t *testing.T) {
	t.Parallel()
	cases := []string{
		`{"ok":true,"data":null,"dataAgeSeconds":0}`,
		`{"ok":true,"data":{"at":"bad","tables":{"fleet":{},"friends":{},"merge":{},"readers":{},"work":{}},"streams":[],"epoch":1},"dataAgeSeconds":0}`,
		`{"ok":true,"data":{"at":"2026-01-01T00:00:00Z","tables":null},"dataAgeSeconds":0}`,
		`{"ok":true,"data":{"at":"2026-01-01T00:00:00Z","tables":{},"streams":[],"epoch":1},"dataAgeSeconds":-1}`,
		`{"ok":"yes","data":{"at":"2026-01-01T00:00:00Z","tables":{},"streams":[],"epoch":1},"dataAgeSeconds":0}`,
		`null`,
	}
	for _, body := range cases {
		if _, _, err := parsePullEnvelope([]byte(body)); err == nil {
			t.Errorf("accepted malformed %s", body)
		}
	}
}
func TestAcceptsGzipHonorsTokenAndQuality(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		header string
		want   bool
	}{{"gzip", true}, {"br, gzip;q=0.5", true}, {"gzip;q=0", false}, {"xgzip", false}, {"gzip;q=NaN", false}, {"gzip;q=bogus", false}, {"br", false}} {
		if got := acceptsGzip([]string{tc.header}); got != tc.want {
			t.Errorf("%q = %v", tc.header, got)
		}
	}
}
