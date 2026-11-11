package run

import (
	"crypto/rand"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// kinLife is v1's challenge, brought back for kin rows (v1.6.9 nova-hook
// serve: a challenge each window that only the model answers, two missed
// and the row is down). A kin row's bus hook can stay on the bus with no
// session behind it, so the bus says nothing about the model: the run
// challenges each hooked kin row on the bus once every
// steps.Engine.ChallengeEvery with a fresh nonce, and only a pong carrying
// one of the row's last two nonces is evidence of life. The steps read the
// evidence (Inputs.Alive) and put a row with none for two windows down,
// its hand returned; raiseSeen brings a kin row up only on a pong since it
// went down, never on presence. A bench's hook is a child of its worker
// program, so a bench keeps the presence rule. It lives in memory: a
// restarted run counts each row's silence afresh from when it first sees
// the row's session. Model: tla/KinAlive.tla.
type kinLife struct {
	nonce      func() string        // a fresh nonce; crypto/rand when nil
	issued     map[string][]string  // row -> its last two nonces, newest last
	challenged map[string]time.Time // row -> when its newest challenge was issued
	pong       map[string]time.Time // row -> its last counted pong
	alive      map[string]time.Time // row -> its last evidence of life: a pong, or its session first seen
	session    map[string]string    // row -> the session alive was last baselined for
}

func (k *kinLife) init() {
	if k.issued == nil {
		k.issued, k.challenged, k.pong = map[string][]string{}, map[string]time.Time{}, map[string]time.Time{}
		k.alive, k.session = map[string]time.Time{}, map[string]string{}
	}
}

// fold counts a pong event: the row's pong to a nonce it was issued.
func (k *kinLife) fold(e store.Event) {
	if e.Kind != "pong" || !e.Accepted {
		return
	}
	k.init()
	for _, n := range k.issued[e.Actor] {
		if e.Op == steps.PongOpID(e.Epoch, e.Actor, n) {
			k.pong[e.Actor], k.alive[e.Actor] = e.At, e.At
			return
		}
	}
}

// observe baselines each hooked kin row's evidence at the first sight of its
// session (a new hook, or a restarted run), and returns this tick's
// challenges: one to each hooked kin row on the bus whose last challenge is
// a window old.
func (k *kinLife) observe(rows map[string]store.Row, ro roster.Roster, present []string, now time.Time) []steps.Nudge {
	k.init()
	every := time.Duration(steps.Engine.ChallengeEvery) * time.Second
	var out []steps.Nudge
	for _, n := range slices.Sorted(maps.Keys(rows)) {
		r := rows[n]
		if _, isKin := ro.Kin[n]; !isKin || !steps.Hooked(r) {
			continue
		}
		if k.session[n] != r.Session {
			k.session[n], k.alive[n] = r.Session, now
		}
		if !slices.Contains(present, n) || now.Sub(k.challenged[n]) < every && !k.challenged[n].IsZero() {
			continue
		}
		nonce := k.fresh()
		last := append(k.issued[n], nonce)
		k.issued[n] = last[max(0, len(last)-2):]
		k.challenged[n] = now
		out = append(out, steps.Nudge{Row: n, Card: "challenge", Subject: "CHALLENGE", Line: fmt.Sprintf(
			"CHALLENGE answer now, or after %s with no answer your row goes down and your hand returns to the queue; run: nova-sprint pong --nonce %s --as %s",
			2*every, nonce, n)})
	}
	return out
}

// failed is a push to row that did not go: its challenge, if this tick
// issued one, is issued again by the next tick.
func (k *kinLife) failed(row string) { delete(k.challenged, row) }

// evidence is each kin row's last evidence of life, the caller's to keep.
func (k *kinLife) evidence() map[string]time.Time { k.init(); return maps.Clone(k.alive) }

// answeredSince says whether row's last counted pong is after at and still
// within two windows of now: a stale answer would raise a row the steps put
// straight down again (KinAlive.tla RaiseDue).
func (k *kinLife) answeredSince(row string, at, now time.Time) bool {
	p, ok := k.pong[row]
	return ok && p.After(at) && now.Sub(p) < 2*time.Duration(steps.Engine.ChallengeEvery)*time.Second
}

func (k *kinLife) fresh() string {
	if k.nonce != nil {
		return k.nonce()
	}
	return rand.Text() // 26 base32 characters from crypto/rand; it has no error to handle (it crashes the program rather than return weak bytes)
}
