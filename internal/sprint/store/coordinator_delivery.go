package store

import (
	"context"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// CoordinatorDeliveries freezes an episode batch before sending, or records its
// successful sends. Expected episode keys fence later status transitions. A
// status shares the causal message, including its pending retry, rather than
// trusting that a cause's mere existence means its notification was delivered.
func (st *Store) CoordinatorDeliveries(ctx context.Context, candidates map[string]sprint.CoordinatorDelivery, sent bool, epoch uint64) (deliveries map[string]sprint.CoordinatorDelivery, err error) {
	_, err = st.Run(ctx, Step{Verb: "coordinator delivery", Actor: sprint.MachineActor, Epoch: &epoch,
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			deliveries = map[string]sprint.CoordinatorDelivery{}
			notes := map[string]sprint.Note{}
			for _, o := range s.Open {
				notes[o.Note.ID] = o.Note
			}
			var p sprint.Plan
			fresh := map[string]bool{}
			migrated := map[string]bool{}
			for id, candidate := range candidates {
				n, ok := notes[id]
				if !ok || sprint.StateEpisodeKey(n) != candidate.Episode {
					continue
				}
				d := candidate
				if n.CoordinatorDelivery != nil {
					d = *n.CoordinatorDelivery
					if !sent && !d.Sent && d.From == "" && candidate.From != "" && d.Token == candidate.Token {
						d.From = candidate.From
						migrated[id] = true
					}
					if sent && d.Token != candidate.Token {
						continue
					}
				} else {
					if sent {
						continue
					}
					fresh[id] = true
				}
				deliveries[id] = d
			}
			// Resolve coverage only for newly prepared status notes. An existing alias
			// keeps its frozen delivery even when its causal note has since closed.
			for id := range fresh {
				d := deliveries[id]
				if d.CoveredBy == "" {
					continue
				}
				owner := d.CoveredBy
				cause, ok := notes[owner]
				shared, found := deliveries[owner]
				if ok && !found && cause.CoordinatorDelivery != nil {
					shared, found = *cause.CoordinatorDelivery, true
				}
				if ok && found && shared.Episode == sprint.StateEpisodeKey(cause) {
					shared.Episode, shared.CoveredBy = d.Episode, owner
					deliveries[id] = shared
				} else {
					d.CoveredBy = ""
					deliveries[id] = d
				}
			}
			for id, d := range deliveries {
				n := notes[id]
				if sent {
					if d.Sent {
						continue
					}
					d.Sent = sent
					deliveries[id] = d
				} else if !fresh[id] && !migrated[id] {
					continue
				}
				n.CoordinatorDelivery = &d
				p.Updates = append(p.Updates, n)
			}
			return p
		}})
	return
}
