package main

import (
	"context"
	"crypto/sha256"
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
			if !sentinelProducersComplete(input) {
				continue
			}
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
		for _, c := range s.Work.Column(sprint.Waiting, sprint.Ready) {
			if (c.Col == sprint.Waiting && len(sprint.WaitsFor(s, c, nil)) != 0) || !sprint.ConsumerNeedsTrunk(s, c) {
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
					trunkKey := trunkTarget{repo, key.trunk}
					var knownTip bool
					proof.Observed, knownTip = observed[trunkKey]
					if !knownTip {
						proof.Observed = l.observedTrunk(ctx, dir, key.trunk)
						observed[trunkKey] = proof.Observed
					}
					if proof.Observed != "" {
						branchTip, missing := l.observedRemoteTip(ctx, dir, key.branch)
						if branchTip != "" || missing {
							proof = l.mergeObservedConsumer(ctx, dir, c.Row, proof, branchTip, missing)
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

// All current live producers must be complete before acquiring any clone or
// fetching. Dropped unplaced records and released named sentinel identities
// have no worker head of their own.
func sentinelProducersComplete(in sprint.SentinelInput) bool {
	for _, p := range in.Producers {
		if p.Outcome == "dropped" && !p.Placed {
			continue
		}
		if p.State != string(sprint.Landed) {
			return false
		}
		if p.Kind == "sentinel" {
			continue
		}
		if (len(p.Head) != 40 && len(p.Head) != 64) || !shaRE.MatchString(p.Head) || swarm.ReadCardBase([]byte(p.Brief)).Repo == "" {
			return false
		}
	}
	return true
}

func (l *lander) observedTrunk(ctx context.Context, dir, trunk string) string {
	if trunk != "main" && trunk != "dev" {
		return ""
	}
	tip, _ := l.observedRemoteTip(ctx, dir, trunk)
	return tip
}

// Capture a freshly fetched commit while holding the existing fetch mutex.
// These private refs never move a checkout; failed fetches return no old tip.
func (l *lander) observedRemoteTip(ctx context.Context, dir, branch string) (tip string, missing bool) {
	mu := l.locks().fetchLock(l.fetchKey(ctx, dir))
	mu.Lock()
	defer mu.Unlock()
	ref := fmt.Sprintf("refs/nova-sentinel/%x", sha256.Sum256([]byte(branch)))
	if _, err := l.git(ctx, dir, "fetch", "--no-tags", "origin", "+refs/heads/"+branch+":"+ref); err != nil {
		return "", containsAny(err.Error(), notOnOrigin)
	}
	tip, err := l.git(ctx, dir, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", false
	}
	return tip, false
}

// A successful no-op observes both remote tips and proves ancestry without
// taking the checkout/land lock or pushing. Mutations reobserve the branch
// under that lock, using this callback's one captured trunk tip throughout.
func (l *lander) mergeObservedConsumer(ctx context.Context, dir, stream string, proof sprint.ConsumerMerge, branchTip string, missing bool) sprint.ConsumerMerge {
	contained := func(tip string) bool {
		if tip == "" {
			return false
		}
		_, err := l.git(ctx, dir, "merge-base", "--is-ancestor", proof.Observed, tip)
		return err == nil
	}
	if contained(branchTip) {
		proof.Merged = branchTip
		proof.ContainsObserved = true
		proof.Success = true
		return proof
	}
	if l.hold(ctx, dir) != "" {
		return proof
	}
	defer l.release() // release before the callback observes another stream
	branchTip, missing = l.observedRemoteTip(ctx, dir, proof.Branch)
	if branchTip == "" && !missing {
		return proof
	}
	if missing {
		if _, err := l.fetched(ctx, dir, "push", "origin", proof.Observed+":refs/heads/"+proof.Branch); err != nil {
			return proof
		}
		branchTip, _ = l.observedRemoteTip(ctx, dir, proof.Branch)
		if branchTip == "" {
			return proof
		}
	}
	if contained(branchTip) {
		proof.Merged = branchTip
		proof.ContainsObserved = true
		proof.Success = true
		return proof
	}
	// The existing worktree recovery reads the branch's tracking ref. Updating
	// it here is fenced by the land lock and names the freshly observed branch.
	if _, err := l.fetched(ctx, dir, "update-ref", "refs/remotes/origin/"+proof.Branch, branchTip); err != nil {
		return proof
	}
	tree, why := l.worktree(ctx, dir, stream, proof.Branch)
	if why != "" {
		return proof
	}
	_, err := l.git(ctx, tree, "checkout", "--detach", branchTip)
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
		_, err = l.git(ctx, tree, "push", "origin", proof.Merged+":refs/heads/"+proof.Branch)
	}
	proof.Success = err == nil
	if err != nil {
		_, _ = l.git(ctx, tree, "merge", "--abort")
	}
	return proof
}
