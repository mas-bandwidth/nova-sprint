package sprint

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/internal/gitrun"
	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

// The mechanical proof's git: one clone of the card's repository, read with no
// checkout (the diff from the merge-base, then every tracked .go file at the head),
// and proved by WorkProof. It runs outside the plan's state: what it reads is the
// commit's.

// ReadProofView reads the mechanical proof's view of head against the base in the
// clone dir: the lint's own view (ReadWorkView) for the diff, then every tracked
// .go file at the head by name.
func ReadProofView(ctx context.Context, dir, base, head string, env []string) (ProofView, error) {
	wv, err := ReadWorkView(ctx, dir, base, head, env)
	if err != nil {
		return ProofView{}, err
	}
	if !wv.Pushed {
		return ProofView{}, fmt.Errorf("the head %s is not in the repository", head)
	}
	v := ProofView{Diff: wv.Diff, Files: map[string]string{}}
	o := gitrun.Options{C: dir, Env: env, OwnRepo: true}
	out, err := gitrun.Output(ctx, o, "ls-tree", "-r", "--name-only", head)
	if err != nil {
		return ProofView{}, err
	}
	for _, p := range strings.Split(out, "\n") {
		if p = strings.TrimSpace(p); p == "" || !strings.HasSuffix(p, ".go") {
			continue
		}
		src, ok := wv.Show(p)
		if !ok {
			continue
		}
		v.Files[p] = string(src)
	}
	return v, nil
}

// ProofGit is where the mechanical proof finds and reads a card's repository:
// Clone is the clone of the repository a brief names (its REPO or base-repo line),
// an error when there is none; Env the git children's environment, nil inheriting;
// Budget bounds one attempt's whole read, zero for ProofBudget; TTL is how long one
// head's proof is kept, zero for two minutes (the tick plans a part more than once,
// and a tree is not read twice while its proof is fresh); Now is the clock the
// cache is kept by, nil for the wall clock.
type ProofGit struct {
	Clone  func(repo string) (string, error)
	Env    []string
	Budget time.Duration
	TTL    time.Duration
	Now    func() time.Time
}

// NewTreeProof is the mechanical proof over g: each mechanical attempt's brief
// names its repository and its proof operands, its tree at the head is read, and
// the kind's proof (WorkProof) holds or refuses it. A tree that cannot be read is
// a proof that refuses with the reason: the machine could not prove the change, so
// the attempt is reworked and no reader is asked.
func NewTreeProof(g ProofGit) ProofRunner {
	budget := g.Budget
	if budget <= 0 {
		budget = ProofBudget
	}
	ttl := g.TTL
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	now := g.Now
	if now == nil {
		now = time.Now
	}
	type kept struct {
		at  time.Time
		run ProofRun
	}
	var mu sync.Mutex
	cache := map[string]kept{}
	return func(s *Snapshot, pr *Card) ProofRun {
		brief := pr.F("brief")
		proof, why := cardhdr.ReadProof(brief)
		if why != "" {
			return ProofRun{Failed: []ProofFinding{{What: why}}}
		}
		if !cardhdr.MechanicalKind(proof.Kind) {
			return ProofRun{}
		}
		cb := swarm.ReadCardBase([]byte(brief))
		base := cb.Ref
		if base == "" {
			base = "main"
		}
		head := pr.F("head")
		if cb.Repo == "" || g.Clone == nil {
			return ProofRun{Failed: []ProofFinding{{What: "the brief of " + pr.ID + " names no repository the proof can read"}}}
		}
		if !headRE.MatchString(head) {
			return ProofRun{Failed: []ProofFinding{{What: "the attempt names no pushed head: " + orDash(head)}}}
		}
		key := strings.Join([]string{cb.Repo, base, head, brief}, "\x00")
		mu.Lock()
		if k, ok := cache[key]; ok && now().Sub(k.at) < ttl {
			mu.Unlock()
			return k.run
		}
		mu.Unlock()
		run := func() ProofRun {
			dir, err := g.Clone(cb.Repo)
			if err != nil {
				return ProofRun{Failed: []ProofFinding{{What: "the proof could not read the tree: " + err.Error()}}}
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			v, err := ReadProofView(ctx, dir, base, head, g.Env)
			if err != nil {
				return ProofRun{Failed: []ProofFinding{{What: "the proof could not read the tree: " + err.Error()}}}
			}
			fs := WorkProof(proof, v)
			return ProofRun{Passed: len(fs) == 0, Failed: fs}
		}()
		mu.Lock()
		if len(cache) > 4096 {
			clear(cache)
		}
		cache[key] = kept{at: now(), run: run}
		mu.Unlock()
		return run
	}
}
