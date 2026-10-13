package main

import (
	"context"
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// bindSentinelProtocol uses the same private clones, identity and cross-process
// land locks as landing. Shadow ticks never bind this writer.
func (a *app) bindSentinelProtocol(st *store.Store) {
	st.SentinelProof = func(ctx context.Context, s *sprint.Snapshot) []sprint.SentinelObservation {
		l := &lander{a: a, st: st}
		defer l.release()
		type target struct{ repo, trunk string }
		tips := map[target]string{}
		dirs := map[string]string{}
		var out []sprint.SentinelObservation
		for _, c := range s.Work.Column(sprint.Waiting) {
			if !sprint.IsSentinel(c) {
				continue
			}
			input := sprint.SentinelInputOf(s, c)
			observation := sprint.SentinelObservation{Input: input}
			for _, p := range input.Producers {
				if p.Kind == "sentinel" || (p.Outcome == "dropped" && !p.Placed) || p.State != string(sprint.Landed) || p.Head == "" {
					continue
				}
				repo := swarm.ReadCardBase([]byte(p.Brief)).Repo
				if repo == "" {
					continue
				}
				dir, known := dirs[repo]
				if !known {
					var why string
					dir, why = l.clone(ctx, repo)
					if why == "" {
						why = l.hold(ctx, dir)
					}
					if why != "" {
						dir = ""
					}
					dirs[repo] = dir
				}
				if dir == "" {
					continue
				}
				trunk := sprint.Trunk(s, p.Stream)
				key := target{repo, trunk}
				tip, known := tips[key]
				if !known {
					tip = l.observedTrunk(ctx, dir, trunk)
					tips[key] = tip
				}
				if tip == "" {
					continue
				}
				_, err := l.git(ctx, dir, "merge-base", "--is-ancestor", p.Head, tip)
				observation.Inclusions = append(observation.Inclusions, sprint.DevInclusion{Producer: p.ID, Repo: repo, Stream: p.Stream, Trunk: trunk, Head: p.Head, Containing: tip, Included: err == nil})
			}
			out = append(out, observation)
		}
		return out
	}
	st.ConsumerTrunk = func(ctx context.Context, s *sprint.Snapshot) []sprint.ConsumerMerge {
		l := &lander{a: a, st: st}
		defer l.release()
		if l.readIdentity(ctx) != "" {
			return nil
		}
		type target struct{ repo, stream, branch, trunk string }
		merged := map[target]sprint.ConsumerMerge{}
		type trunkTarget struct{ repo, trunk string }
		// tla/SentinelConsumerFreshness.tla CallbackUsesOneObservedTip and
		// DealsUseKnownCallbackSnapshot: one observed target per callback.
		observed := map[trunkTarget]string{}
		var out []sprint.ConsumerMerge
		for _, c := range s.Work.Cards() {
			if (c.Col != sprint.Waiting && c.Col != sprint.Ready) || !sprint.ConsumerNeedsTrunk(s, c) {
				continue
			}
			repo := swarm.ReadCardBase([]byte(c.F("brief"))).Repo
			if repo == "" {
				continue
			}
			cardInput, barriers := sprint.SentinelCardOf(c), sprint.ConsumerBarriersOf(s, c)
			key := target{repo, c.Row, sprint.StreamBranch(s, c.Row), sprint.Trunk(s, c.Row)}
			proof, known := merged[key]
			if !known {
				proof = sprint.ConsumerMerge{Repo: repo, Branch: key.branch, Trunk: key.trunk}
				dir, why := l.clone(ctx, repo)
				if why == "" {
					why = l.hold(ctx, dir)
				}
				if why == "" {
					why = l.ensureStreamBranch(ctx, s, dir, repo, c.Row, key.branch)
				}
				if why == "" {
					trunkKey := trunkTarget{repo, key.trunk}
					var observedKnown bool
					proof.Observed, observedKnown = observed[trunkKey]
					if !observedKnown {
						proof.Observed = l.observedTrunk(ctx, dir, key.trunk)
						observed[trunkKey] = proof.Observed
					}
					if proof.Observed != "" {
						tree, w := l.worktree(ctx, dir, c.Row, key.branch)
						if w == "" {
							_, err := l.git(ctx, tree, "checkout", "--detach", "refs/remotes/origin/"+key.branch)
							if err == nil {
								_, err = l.git(ctx, tree, "merge", "--no-edit", proof.Observed)
							}
							if err == nil {
								proof.Merged, err = l.git(ctx, tree, "rev-parse", "HEAD")
							}
							if err == nil {
								_, err = l.git(ctx, tree, "merge-base", "--is-ancestor", proof.Observed, proof.Merged)
								proof.ContainsObserved = err == nil
							}
							if err == nil {
								_, err = l.git(ctx, tree, "push", "origin", proof.Merged+":refs/heads/"+key.branch)
							}
							proof.Success = err == nil
							if err != nil {
								_, _ = l.git(ctx, tree, "merge", "--abort")
							}
						}
					}
				}
				merged[key] = proof
			}
			proof.Epoch = s.Epoch
			proof.Card = cardInput
			proof.Barriers = barriers
			out = append(out, proof)
		}
		return out
	}
}

func (l *lander) observedTrunk(ctx context.Context, dir, trunk string) string {
	if trunk != "main" && trunk != "dev" {
		return ""
	}
	if _, err := l.fetched(ctx, dir, "fetch", "--no-tags", "origin", fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", trunk, trunk)); err != nil {
		return ""
	}
	tip, err := l.git(ctx, dir, "rev-parse", "--verify", "refs/remotes/origin/"+trunk+"^{commit}")
	if err != nil {
		return ""
	}
	return tip
}
