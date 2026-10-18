package store

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/redis/go-redis/v9"
)

// openCache is the decoded notes of the open index, kept between openOf calls
// (one per client and notes key, openCaches), each with the raw JSON it was
// decoded from. The fault of 2026-10-17: every twin read (ReadView) and every
// Store.Load (OpenNotes) json-decoded every open and acknowledged note again,
// at least two per idle tick and one per part step, besides the inbox, the
// land loop, the decide lane and the dashboard. A note is decoded again only
// when its stored text differs from the text the cache holds (SetReview and a
// commit rewrite a note in place), so a read gives what a fresh decode gives;
// the round trips are unchanged. Each cache holds only the notes its last read
// named, so it is bounded by the open index; the caches are bounded by
// openCachesMax (openCacheFor).
type openCache struct {
	mu    sync.Mutex
	notes map[string]openCached // by note id
}

// openCached is one note as decoded, and the stored text it was decoded from.
type openCached struct {
	raw  string
	note sprint.Note
}

// openCacheKey names a cache: the client and the notes hash's key (it carries
// the epoch), so two stores on different servers never share one.
type openCacheKey struct {
	client redis.UniversalClient
	key    string
}

// openCaches is the caches by client and notes key: at most openCachesMax, so
// the keys of past epochs (a clear moves the notes key) and the clients of
// stores gone are not held for the life of the process.
var openCaches struct {
	mu sync.Mutex
	m  map[openCacheKey]*openCache
}

// openCachesMax bounds openCaches: a process reads the epoch it is pinned to
// and, now and then, one before it.
const openCachesMax = 4

// openCacheFor is the cache of the client and notes key, made on first use;
// making one past openCachesMax drops the others first.
func openCacheFor(client redis.UniversalClient, key string) *openCache {
	k := openCacheKey{client, key}
	openCaches.mu.Lock()
	defer openCaches.mu.Unlock()
	if c := openCaches.m[k]; c != nil {
		return c
	}
	if openCaches.m == nil || len(openCaches.m) >= openCachesMax {
		openCaches.m = map[openCacheKey]*openCache{}
	}
	c := &openCache{}
	openCaches.m[k] = c
	return c
}

// decodeOpenNote is one stored note decoded.
func decodeOpenNote(raw string) (sprint.Note, error) {
	var n sprint.Note
	err := json.Unmarshal([]byte(raw), &n)
	return n, err
}

// notesOf is the notes of the HMGET answer vals for ids (a value that is not a
// string is a note not found, left out), each decoded by decode only when its
// text is not the one the cache holds. Every note given is a copy: nothing a
// caller does to it reaches the cache. The decodes run outside the cache's lock
// (a read never changes a kept map, it puts a new one), so concurrent readers
// decode side by side, as before the cache; the last to finish is kept.
func (c *openCache) notesOf(ids []string, vals []any, decode func(string) (sprint.Note, error)) (map[string]sprint.Note, error) {
	c.mu.Lock()
	held := c.notes
	c.mu.Unlock()
	kept := make(map[string]openCached, len(ids))
	out := make(map[string]sprint.Note, len(ids))
	for i, v := range vals {
		s, ok := v.(string)
		if !ok {
			continue
		}
		e, hit := held[ids[i]]
		if !hit || e.raw != s {
			n, err := decode(s)
			if err != nil {
				return nil, fmt.Errorf("notification %s: %w", ids[i], err)
			}
			e = openCached{raw: s, note: n}
		}
		kept[ids[i]] = e
		out[ids[i]] = cloneNote(e.note)
	}
	c.mu.Lock()
	c.notes = kept
	c.mu.Unlock()
	return out, nil
}

// cloneNote is a copy of n sharing no slice or pointer with it.
func cloneNote(n sprint.Note) sprint.Note {
	n.Primaries = slices.Clone(n.Primaries)
	n.Decisions = slices.Clone(n.Decisions)
	n.Suspects = slices.Clone(n.Suspects)
	n.Needs = slices.Clone(n.Needs)
	if n.StreamTransition != nil {
		t := *n.StreamTransition
		t.Owners = slices.Clone(t.Owners)
		n.StreamTransition = &t
	}
	if n.DecisionDelivery != nil {
		d := *n.DecisionDelivery
		d.Reports = slices.Clone(d.Reports)
		n.DecisionDelivery = &d
	}
	if n.CoordinatorDelivery != nil {
		d := *n.CoordinatorDelivery
		n.CoordinatorDelivery = &d
	}
	return n
}
