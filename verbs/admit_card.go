package verbs

import (
	"context"
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// cardFacts are what admission reads of one producer, from a brief (add) or
// a stored card (restore, check): the store's fields and the brief's text.
type cardFacts struct {
	ID, Stream, Tier, Brief string
	Paths, DoneWhen         []string
}

func factsOfAdd(a card.Add) cardFacts {
	return cardFacts{ID: a.ID, Stream: a.Stream, Tier: a.Tier, Brief: a.Brief, Paths: a.Paths, DoneWhen: a.DoneWhen}
}

func factsOfCard(c store.Card) cardFacts {
	return cardFacts{ID: c.ID, Stream: c.Stream, Tier: c.Tier, Brief: c.Brief, Paths: c.Paths, DoneWhen: c.DoneWhen}
}

// streamAtBase is one stream as admission reads it once for all its cards:
// the stream, its clone address, the base tip, every file and directory at
// it, and the card branches at origin.
type streamAtBase struct {
	s        store.Stream
	url, tip string
	tree     map[string]bool
	branches map[string]string
}

// admitCards proves each producer of facts (CORRECT-USE "Admission"): its
// stream is live (its repository and base read at origin now), its tier and
// routes have a serving row in nova-config, its PATHS exist at the base, its
// DONE-WHEN lines are each one command (edge.ParseGate), it names no retired mechanic and no bench by name,
// and its work is not already at the base. Each stream is read once, all at
// once; the result is each refused card's refusal, by id.
func (e *Engine) admitCards(ctx context.Context, snap store.Snapshot, facts []cardFacts) (map[string]error, error) {
	lim, _, err := e.rules(ctx, snap)
	if err != nil {
		return nil, err
	}
	ro, err := e.roster(ctx, lim, snap)
	if err != nil {
		return nil, err
	}
	streams, serr := e.readStreams(ctx, snap, facts)
	out := map[string]error{}
	for _, f := range facts {
		if err := cardLint(f, ro); err != nil {
			out[f.ID] = err
		} else if err := serr[f.Stream]; err != nil {
			named := admissionWhat(f.Stream, err)
			named.What = "card " + f.ID + ": " + named.What
			out[f.ID] = named
		} else if err := e.cardAtBase(ctx, streams[f.Stream], f); err != nil {
			out[f.ID] = err
		}
	}
	return out, nil
}

// readStreams reads every stream facts name at origin, at once.
func (e *Engine) readStreams(ctx context.Context, snap store.Snapshot, facts []cardFacts) (map[string]*streamAtBase, map[string]error) {
	names := map[string]bool{}
	for _, f := range facts {
		names[f.Stream] = true
	}
	out, errs := map[string]*streamAtBase{}, map[string]error{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for n := range names {
		wg.Go(func() {
			sb, err := e.streamBase(ctx, snap, n)
			mu.Lock()
			out[n], errs[n] = sb, err
			mu.Unlock()
		})
	}
	wg.Wait()
	return out, errs
}

// streamBase is stream name read at origin: live (in the store, its base
// at origin), its tree at the base tip, and its card branches.
func (e *Engine) streamBase(ctx context.Context, snap store.Snapshot, name string) (*streamAtBase, error) {
	s, ok := snap.Streams[name]
	if !ok {
		return nil, refuse("no stream "+name, "nova-sprint stream add "+name+" --repo <org/repo>")
	}
	tip, err := e.baseTip(ctx, s, "add the card again")
	if err != nil {
		return nil, err
	}
	url, _ := streamURL(s) // baseTip read this address
	files, err := e.Origin.Tree(ctx, url, tip)
	if err != nil {
		return nil, RefusalOr(err, broken("the tree of "+s.Repo+" at "+shortSHA(tip)+" cannot be read ("+errText(err)+")", "make "+url+" cloneable from this host, then add the card again"))
	}
	branches, err := e.Origin.Branches(ctx, url, "sprint/"+s.Name+"/")
	if err != nil {
		return nil, RefusalOr(err, broken("the branches of "+s.Repo+" cannot be listed ("+errText(err)+")", "make "+url+" readable from this host, then add the card again"))
	}
	return &streamAtBase{s: s, url: url, tip: tip, tree: treeSet(files), branches: branches}, nil
}

// treeSet is every file and every directory above one ("" is the root).
func treeSet(files []string) map[string]bool {
	set := map[string]bool{"": true}
	for _, f := range files {
		for p := f; p != "." && p != "" && !set[p]; p = path.Dir(p) {
			set[p] = true
		}
	}
	return set
}

// cardLint is the checks that need no repository: the tier and routes
// served by a row in nova-config, DONE-WHEN parsing, and no retired
// mechanic or bench named (a stored brief is re-linted the same way).
func cardLint(f cardFacts, ro roster.Roster) error {
	routes := card.NeedsRoutes(f.Brief)
	if !served(ro, f.Tier, routes) {
		return refuse(fmt.Sprintf("card %s: no row in nova-config serves tier %s%s", f.ID, f.Tier, routeWords(routes)), serveFix(f.Tier, routes))
	}
	for _, d := range f.DoneWhen {
		if _, why := edge.ParseGate(d); why != "" {
			return refuse("card "+f.ID+": DONE-WHEN "+clip(d)+" is not one command: "+why, edge.GateFix+", then add the card again")
		}
	}
	lines := strings.Split(f.Brief, "\n")
	if n, what, fix := retiredMechanic(lines); what != "" {
		return refuse(fmt.Sprintf("card %s: line %d names %s, a retired mechanic", f.ID, n, what), fix)
	}
	if n, bench := benchNamed(lines, ro); bench != "" {
		return refuse(fmt.Sprintf("card %s: line %d's gate names the bench %s", f.ID, n, bench), retiredFix)
	}
	return nil
}

// served is whether a row nova-config lists, at a width above 0, serves
// tier and every route.
func served(ro roster.Roster, tier string, routes []string) bool {
	for _, n := range append(slices.Collect(maps.Keys(ro.Workers)), slices.Collect(maps.Keys(ro.Kin))...) {
		_, width, tiers, _ := ro.Row(n)
		if width > 0 && slices.Contains(tiers, tier) && ro.ServesRoutes(n, routes) {
			return true
		}
	}
	return false
}

func routeWords(routes []string) string {
	if len(routes) == 0 {
		return ""
	}
	return " with route(s) " + strings.Join(routes, ", ")
}

// serveFix is how a tier and routes come to be served.
func serveFix(tier string, routes []string) string {
	if len(routes) > 0 {
		return "give a bench that has it the role " + roster.RoutePrefix + routes[0] + " (nova-config machine set <bench> --roles bench," + roster.RoutePrefix + routes[0] + " --actor <you> --reason <why>), or drop the " + card.RouteField + " line, then add the card again"
	}
	return "give a friend the tier (nova-config friend set <name> --tiers ...," + tier + "), give a bench cores, or change the brief's tier line, then add the card again"
}

// benchNamed is the first gate line (DONE-WHEN, TEST) naming a bench
// nova-config lists, as a word: a gate names facts, never a host.
func benchNamed(lines []string, ro roster.Roster) (int, string) {
	for i, line := range lines {
		key, value, _ := strings.Cut(strings.TrimSpace(line), ":")
		if key != "DONE-WHEN" && key != "TEST" {
			continue
		}
		for _, n := range slices.Sorted(maps.Keys(ro.Workers)) {
			if hasWord(value, n) {
				return i + 1, n
			}
		}
	}
	return 0, ""
}

// cardAtBase is the checks at the base: PATHS exist (pathProblem), and the
// work is not already there: the card's branch at origin, the brief's HEAD,
// or its PR's head, an ancestor of the base, or the PR merged.
func (e *Engine) cardAtBase(ctx context.Context, sb *streamAtBase, f cardFacts) error {
	for _, p := range f.Paths {
		if why := pathProblem(sb.tree, f.Paths, p); why != "" {
			return refuse("card "+f.ID+": PATHS "+p+": "+why+" at "+sb.s.Repo+"'s "+sb.s.Base+" ("+shortSHA(sb.tip)+")",
				"correct the path, or declare a new directory as its own PATHS entry ending in / (its parent must exist), then add the card again")
		}
	}
	heads, err := e.carriedHeads(ctx, sb, f)
	if err != nil {
		return err
	}
	for _, h := range heads {
		in, err := e.Origin.Contains(ctx, sb.url, h.sha, sb.tip)
		if err != nil {
			return RefusalOr(err, broken("git merge-base in "+sb.s.Repo+" failed ("+errText(err)+")", "add the card again"))
		}
		if in {
			return refuse("card "+f.ID+": its work is already at the base: "+h.what+" "+shortSHA(h.sha)+" is in "+sb.s.Base, "do not add it (nova-sprint show "+f.ID+" if it was added before), or brief the work that is still to do")
		}
	}
	return nil
}

// carried is a head the card's work already has, and where it came from.
type carried struct{ what, sha string }

// carriedHeads are the card's branch at origin, the brief's HEAD, and its
// PR's head (a merged PR is the work at the base already: a refusal).
func (e *Engine) carriedHeads(ctx context.Context, sb *streamAtBase, f cardFacts) ([]carried, error) {
	var out []carried
	if sha, ok := sb.branches["sprint/"+sb.s.Name+"/"+f.ID]; ok {
		out = append(out, carried{"its branch sprint/" + sb.s.Name + "/" + f.ID, sha})
	}
	for _, h := range card.BriefField(f.Brief, "HEAD") {
		out = append(out, carried{"its HEAD", strings.ToLower(h)})
	}
	for _, v := range card.BriefField(f.Brief, "PR") {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, RefusalOr(err, refuse("card "+f.ID+": PR "+clip(v)+" is not a number ("+errText(err)+")", "write PR: <number>, then add the card again"))
		}
		if e.Forge == nil {
			return nil, broken("card "+f.ID+": PR "+v+" cannot be read: the forge is not configured", "take it to the project's own tracker with this line")
		}
		head, merged, err := e.Forge.PR(ctx, sb.s.Repo, n)
		if err != nil {
			return nil, RefusalOr(err, broken("card "+f.ID+": PR "+v+" of "+sb.s.Repo+" cannot be read ("+errText(err)+")", "check gh pr view "+v+" -R "+sb.s.Repo+", then add the card again"))
		}
		if merged {
			return nil, refuse("card "+f.ID+": its work is already at the base: PR "+v+" merged", "do not add it, or brief the work that is still to do")
		}
		out = append(out, carried{"its PR " + v + "'s head", head})
	}
	return out, nil
}

// pathProblem is why PATHS entry p is not admitted at the base, or "". A
// glob (* ? [) must match a file or directory there (path.Match, each
// segment; ** is not special). A literal is admitted when it is there, when
// its directory is (a file or directory the card creates), or when its
// directory is itself declared in PATHS as a new directory (an entry ending
// in /) whose parent is there.
func pathProblem(tree map[string]bool, paths []string, p string) string {
	clean := strings.TrimSuffix(p, "/")
	if strings.ContainsAny(clean, "*?[") {
		for f := range tree {
			if ok, err := path.Match(clean, f); err != nil {
				return "the glob does not parse (" + err.Error() + ")"
			} else if ok {
				return ""
			}
		}
		return "the glob matches nothing"
	}
	dir := parentDir(clean)
	switch {
	case tree[clean], tree[dir]:
		return ""
	case slices.Contains(paths, dir+"/") && tree[parentDir(dir)]:
		return ""
	}
	return "neither it nor its directory " + dir + "/ is there"
}

// parentDir is p's directory, "" for the root.
func parentDir(p string) string {
	if d := path.Dir(p); d != "." && d != "/" {
		return d
	}
	return ""
}

// hasWord is whether word occurs in s with no name character
// (a-z A-Z 0-9 . _ -) on either side.
func hasWord(s, word string) bool {
	nameChar := func(i int) bool {
		c := s[i]
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
	}
	for at := 0; word != ""; {
		i := strings.Index(s[at:], word)
		if i < 0 {
			return false
		}
		i += at
		if end := i + len(word); (i == 0 || !nameChar(i-1)) && (end == len(s) || !nameChar(end)) {
			return true
		}
		at = i + 1
	}
	return false
}
