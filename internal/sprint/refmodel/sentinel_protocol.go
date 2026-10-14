package refmodel

import (
	"maps"
	"reflect"
	"slices"
)

// ProtocolInput supplies outside observations explicitly. The table-only model
// does not execute Git. Heads are symbolic attempt identities, not Git SHAs;
// inclusion is an asserted observation from the caller's fake forge. Nil input
// supplies no ancestry or successful consumer merge. See SentinelRelease.tla and
// SentinelConsumerFreshness.tla for the bounded observation contract.
type ProtocolInput struct {
	Producers map[string]ProducerTrunkFact
	Consumers map[string]ConsumerTrunkFact
}

type ProtocolTarget struct{ Branch, Trunk string }

// ProtocolScope binds a fact to the epoch, current cards and recorded targets.
// Full Primary values include state, score, needs, head and early provenance.
type ProtocolScope struct {
	Epoch   uint64
	Cards   map[string]Primary
	Targets map[string]ProtocolTarget
}

type ProducerTrunkFact struct {
	Scope         ProtocolScope
	Observed      string
	IncludedHeads map[string]int
}

type ConsumerTrunkFact struct {
	Scope            ProtocolScope
	Observed, Merged string
	ContainsObserved bool
}

func (s State) target(stream string) ProtocolTarget {
	st := s.Streams[stream]
	b, t := st.Branch, st.Trunk
	if b == "" {
		b = "sprint/" + stream
	}
	if t == "" {
		t = "dev"
	}
	return ProtocolTarget{b, t}
}

// protocolScope owns its copies, so a later model mutation cannot rewrite a
// previously gathered outside fact.
func (s State) protocolScope(ids []string) ProtocolScope {
	copy := s.Clone()
	out := ProtocolScope{Epoch: s.Epoch, Cards: map[string]Primary{}, Targets: map[string]ProtocolTarget{}}
	for _, id := range ids {
		p := copy.Primaries[id]
		out.Cards[id] = p
		out.Targets[p.Stream] = s.target(p.Stream)
	}
	return out
}

// SentinelProtocolScope includes transitive ordinary producers and each named
// automatic sentinel's identity. An explicit early override stops expansion.
func (s State) SentinelProtocolScope(id string) ProtocolScope {
	seen := map[string]bool{}
	var visit func(string)
	visit = func(p string) {
		if seen[p] {
			return
		}
		seen[p] = true
		pr, ok := s.Primaries[p]
		if !ok || pr.Kind != KindSentinel || pr.State == Off || pr.ReleasedEarly {
			return
		}
		for _, need := range pr.Needs {
			visit(need)
		}
		for _, q := range s.StreamOrder(pr.Stream) {
			if q != p && s.Primaries[q].Kind != KindSentinel && s.Primaries[q].Score < pr.Score {
				visit(q)
			}
		}
	}
	visit(id)
	return s.protocolScope(Keys(seen))
}

func (s State) sentinelDone(id string) bool {
	scope := s.SentinelProtocolScope(id)
	for q, p := range scope.Cards {
		if q != id && p.State != Off && p.State != Landed {
			return false
		}
	}
	return true
}

func (s State) sentinelTrunkReady(id string) bool {
	scope := s.SentinelProtocolScope(id)
	var required []string
	for q, p := range scope.Cards {
		if q != id && p.Kind != KindSentinel && p.State != Off {
			required = append(required, q)
		}
	}
	if len(required) == 0 {
		return true
	}
	if s.protocol == nil {
		return false
	}
	fact, ok := s.protocol.Producers[id]
	if !ok || fact.Observed == "" || !reflect.DeepEqual(fact.Scope, scope) {
		return false
	}
	for _, q := range required {
		p := scope.Cards[q]
		if p.Repo == "" || p.Head <= 0 || fact.IncludedHeads[q] != p.Head {
			return false
		}
	}
	return true
}

// ConsumerProtocolScope captures the card plus every released named/positional
// barrier. A new barrier with an unchanged consumer invalidates the old fact.
func (s State) ConsumerProtocolScope(id string) ProtocolScope {
	p := s.Primaries[id]
	ids := []string{id}
	for q, barrier := range s.Primaries {
		if barrier.Kind != KindSentinel || barrier.State != Landed {
			continue
		}
		if contains(p.Needs, q) || barrier.Stream == p.Stream && barrier.Score < p.Score {
			ids = append(ids, q)
		}
	}
	return s.protocolScope(ids)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func (s State) consumerTrunkReady(id string) bool {
	scope := s.ConsumerProtocolScope(id)
	if len(scope.Cards) == 1 {
		return true
	}
	if s.protocol == nil || s.Primaries[id].Repo == "" {
		return false
	}
	fact, ok := s.protocol.Consumers[id]
	return ok && fact.Observed != "" && fact.Merged != "" && fact.ContainsObserved && reflect.DeepEqual(fact.Scope, scope)
}

// releaseSentinels models the tick's automatic release wave; ordinary verbs
// only update reached progress. All predicates are evaluated before any wave
// member moves, matching outside observation and same-wave consumer fencing.
func (s *State) releaseSentinels() {
	var release []string
	for _, id := range Keys(s.Primaries) {
		p := s.Primaries[id]
		if p.Kind == KindSentinel && p.State == Waiting && s.sentinelDone(id) && s.sentinelTrunkReady(id) {
			release = append(release, id)
		}
	}
	for _, id := range release {
		p := s.Primaries[id]
		st := s.Streams[p.Stream]
		st.State = s.streamAfter(p.Stream, st.State, []string{id}, nil)
		s.Streams[p.Stream] = st
		p.State = Landed
		s.Primaries[id] = p
		s.closeOn(id)
	}
}

func cloneProtocolScope(s ProtocolScope) ProtocolScope {
	out := s
	out.Cards = maps.Clone(s.Cards)
	for id, p := range out.Cards {
		p.Needs = slices.Clone(p.Needs)
		p.Waived = slices.Clone(p.Waived)
		p.Pair = slices.Clone(p.Pair)
		p.RefusedWays = slices.Clone(p.RefusedWays)
		out.Cards[id] = p
	}
	out.Targets = maps.Clone(s.Targets)
	return out
}

func (p *ProtocolInput) clone() *ProtocolInput {
	if p == nil {
		return nil
	}
	out := &ProtocolInput{Producers: maps.Clone(p.Producers), Consumers: maps.Clone(p.Consumers)}
	for id, fact := range out.Producers {
		fact.Scope = cloneProtocolScope(fact.Scope)
		fact.IncludedHeads = maps.Clone(fact.IncludedHeads)
		out.Producers[id] = fact
	}
	for id, fact := range out.Consumers {
		fact.Scope = cloneProtocolScope(fact.Scope)
		out.Consumers[id] = fact
	}
	return out
}
