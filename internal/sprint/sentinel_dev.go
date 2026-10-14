package sprint

import (
	"reflect"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// SentinelInput pins the store side of SentinelRelease.tla ProbeProducer and
// AutoRelease. Forge observations authorize only this epoch and producer set.
type SentinelInput struct {
	Epoch     uint64
	Sentinel  SentinelCard
	Producers []SentinelCard
	Targets   map[string]string
}

// SentinelCard is the identity and source observed before an outside read.
type SentinelCard struct {
	ID, Stream, State, Head, Brief, Outcome, Needs, Kind, ReleaseKind string
	Score                                                             float64
	Revision                                                          uint64
	Placed                                                            bool
}

func SentinelCardOf(c *Card) SentinelCard {
	return SentinelCard{ID: c.ID, Placed: c.Placed(), Kind: c.F("kind"), ReleaseKind: c.F("release_kind"), Stream: c.Row, State: c.Col, Head: c.F("head"), Brief: c.F("brief"), Outcome: c.F("outcome"), Needs: c.F("needs"), Score: c.Score, Revision: c.Rev}
}

// SentinelInputOf includes named producers and every primary before the
// sentinel in its stream. A dropped named producer is retained as completed.
func SentinelInputOf(s *Snapshot, c *Card) SentinelInput {
	in := SentinelInput{Epoch: s.Epoch, Sentinel: SentinelCardOf(c), Targets: map[string]string{c.Row: StreamBranch(s, c.Row) + "@" + Trunk(s, c.Row)}}
	ids := Split(c.F("needs"))
	for _, producer := range streamLine(s, c.Row) {
		if producer.Score < c.Score && !IsSentinel(producer) {
			ids = append(ids, producer.ID)
		}
	}
	seen := map[string]bool{c.ID: true}
	var visit func(string)
	visit = func(id string) {
		if id == c.ID {
			in.Producers = append(in.Producers, SentinelCardOf(c))
			return
		}
		if seen[id] {
			return
		}
		seen[id] = true
		producer := s.Work.Card(id)
		if producer == nil {
			in.Producers = append(in.Producers, SentinelCard{ID: id})
			return
		}
		in.Producers = append(in.Producers, SentinelCardOf(producer))
		in.Targets[producer.Row] = StreamBranch(s, producer.Row) + "@" + Trunk(s, producer.Row)
		if IsSentinel(producer) && !sentinelDropped(SentinelCardOf(producer)) && producer.F("release_kind") != "early override" {
			nested := Split(producer.F("needs"))
			for _, p := range streamLine(s, producer.Row) {
				if p.Score < producer.Score && !IsSentinel(p) {
					nested = append(nested, p.ID)
				}
			}
			for _, id := range nested {
				visit(id)
			}
		}
	}
	for _, id := range ids {
		visit(id)
	}
	slices.SortFunc(in.Producers, func(a, b SentinelCard) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return in
}

// DevInclusion is a fresh fetched ancestry result, never a claimed PR merge.
// Containing is the actual observed tip of the recorded main/dev trunk.
type DevInclusion struct {
	Producer, Repo, Stream, Trunk, Head, Containing string
	Included                                        bool
}

// SentinelObservation binds actual forge evidence to its exact store input.
type SentinelObservation struct {
	Input      SentinelInput
	Inclusions []DevInclusion
}

// SentinelDevReason implements SentinelRelease.tla AutoRelease's observed
// boundary. Remote changes after the observation are not a joint transaction.
func SentinelDevReason(s *Snapshot, c *Card, observations []SentinelObservation) (string, []DevInclusion, bool) {
	input := SentinelInputOf(s, c)
	live := 0
	dropped := 0
	for _, producer := range input.Producers {
		if sentinelDropped(producer) {
			dropped++
			continue
		}
		if producer.State != string(Landed) {
			return "", nil, false
		}
		if producer.Kind != "sentinel" {
			live++
		}
	}
	if len(input.Producers) == 0 {
		return "no producers", nil, true
	}
	if live == 0 {
		if dropped > 0 {
			return "all producers dropped", nil, true
		}
		return "no producers", nil, true
	}
	for _, observation := range observations {
		if !reflect.DeepEqual(input, observation.Input) {
			continue
		}
		var merges []DevInclusion
		for _, producer := range input.Producers {
			if sentinelDropped(producer) || producer.Kind == "sentinel" {
				continue
			}
			repo := swarm.ReadCardBase([]byte(producer.Brief)).Repo
			found := false
			for _, proof := range observation.Inclusions {
				if proof.Producer == producer.ID && proof.Stream == producer.Stream && proof.Repo == repo && repo != "" && proof.Trunk == Trunk(s, producer.Stream) && producer.Head != "" && proof.Head == producer.Head && proof.Containing != "" && proof.Included {
					merges = append(merges, proof)
					found = true
					break
				}
			}
			if !found {
				return "", nil, false
			}
		}
		return "every producer is complete and its head is in the observed trunk", merges, true
	}
	return "", nil, false
}

// ConsumerMerge binds a successful actual stream merge to one observed trunk.
// A failed merge produces no receipt and cannot authorize ready or working.
type ConsumerBarrierInput struct {
	Sentinels []SentinelCard
	Targets   map[string]string
}

// ConsumerBarriersOf pins every released named or positional barrier and its
// recorded target. A new or reworked barrier invalidates an outside receipt.
func ConsumerBarriersOf(s *Snapshot, c *Card) ConsumerBarrierInput {
	in := ConsumerBarrierInput{Targets: map[string]string{}}
	seen := map[string]bool{}
	add := func(stop *Card) {
		if stop == nil || seen[stop.ID] || !IsSentinel(stop) || inState(stop, Landed) != "" {
			return
		}
		seen[stop.ID] = true
		in.Sentinels = append(in.Sentinels, SentinelCardOf(stop))
		in.Targets[stop.Row] = StreamBranch(s, stop.Row) + "@" + Trunk(s, stop.Row)
	}
	for _, id := range Split(c.F("needs")) {
		add(s.Work.Card(id))
	}
	for _, stop := range s.Work.releasedBefore(c.Row, c.Score) {
		if stop.Score < c.Score {
			add(stop)
		}
	}
	slices.SortFunc(in.Sentinels, func(a, b SentinelCard) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return in
}

type ConsumerMerge struct {
	Barriers                              ConsumerBarrierInput
	Epoch                                 uint64
	Card                                  SentinelCard
	Repo, Branch, Trunk, Observed, Merged string
	Success, ContainsObserved             bool
}

// ConsumerNeedsTrunk is deliberately limited to waves crossing released
// sentinels; ordinary initial work does not acquire a new Git prerequisite.
func ConsumerNeedsTrunk(s *Snapshot, c *Card) bool {
	if c == nil || IsSentinel(c) {
		return false
	}
	if needs := c.F("needs"); needs != "" {
		for _, id := range Split(needs) {
			if stop := s.Work.Card(id); stop != nil && IsSentinel(stop) && inState(stop, Landed) == "" {
				return true
			}
		}
	}
	return len(s.Work.releasedBefore(c.Row, c.Score)) > 0
}

// tla/SentinelConsumerFreshness.tla Deal and DealsUseCurrentBarrierVector:
// recompute released barriers at the fenced ready/deal boundary.
func ConsumerTrunkReady(s *Snapshot, c *Card) bool {
	if !ConsumerNeedsTrunk(s, c) {
		return true
	}
	if len(s.ConsumerMerges) == 0 {
		return false
	}
	repo := swarm.ReadCardBase([]byte(c.F("brief"))).Repo
	barriers := ConsumerBarriersOf(s, c)
	for _, proof := range s.ConsumerMerges {
		if proof.Success && proof.ContainsObserved && proof.Epoch == s.Epoch && reflect.DeepEqual(proof.Card, SentinelCardOf(c)) && reflect.DeepEqual(proof.Barriers, barriers) && repo != "" && proof.Repo == repo && proof.Branch == StreamBranch(s, c.Row) && proof.Trunk == Trunk(s, c.Row) && proof.Observed != "" && proof.Merged != "" {
			return true
		}
	}
	return false
}

func withConsumerMerges(s *Snapshot, proofs []ConsumerMerge) *Snapshot {
	copy := *s
	copy.ConsumerMerges = proofs
	return &copy
}

func consumerCrossesRelease(s *Snapshot, c *Card, landing map[string]bool) bool {
	if IsSentinel(c) {
		return false
	}
	for id := range landing {
		stop := s.Work.Card(id)
		if stop != nil && IsSentinel(stop) && ((stop.Row == c.Row && stop.Score < c.Score) || contains(Split(c.F("needs")), id)) {
			return true
		}
	}
	return false
}

func sentinelDropped(c SentinelCard) bool { return c.Outcome == "dropped" && !c.Placed }
