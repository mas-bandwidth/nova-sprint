package worker

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/cardcost"
	"github.com/mas-bandwidth/nova-tools/pkg/member"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
	"github.com/mas-bandwidth/nova-tools/pkg/typedrec"
)

// PushJob is one ended work card's push: the checkout the child committed in, the
// origin and branch it goes to, the head the result names, the commit it was staged on
// and the RESULT.md read (""  when the gh shim's finish was).
type PushJob struct {
	Launch, Checkout, URL, Branch, Head, Base, Result string
}

// Pusher pushes a work card's commit from outside the wall. git never runs in the
// checkout the child wrote: the commit is fetched from it into the worker's own bare
// repository, checked there (it descends from the staged commit and adds one at least),
// and pushed from there to origin's branch, never forced.
type Pusher struct {
	Repo string           // the bare repository, made once: <root>/push.git
	Now  func() time.Time // when to tidy the repository; nil never tidies

	mu      sync.Mutex
	tidied  time.Time
	initErr error
	inited  bool
}

// TidyEvery is how often the bare repository is repacked and pruned.
const TidyEvery = time.Hour

func (g *Pusher) git(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", g.Repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (g *Pusher) init() error {
	if g.inited {
		return g.initErr
	}
	g.inited = true
	if _, err := os.Stat(filepath.Join(g.Repo, "HEAD")); err != nil {
		if out, err := exec.Command("git", "init", "-q", "--bare", "--", g.Repo).CombinedOutput(); err != nil {
			g.initErr = fmt.Errorf("git init %s: %v: %s", g.Repo, err, oneline.Escape(string(out)))
			return g.initErr
		}
	}
	for _, kv := range [][2]string{{"gc.auto", "0"}, {"maintenance.auto", "false"}, {"core.hooksPath", "/dev/null"}} {
		if out, err := g.git("config", kv[0], kv[1]); err != nil {
			g.initErr = fmt.Errorf("git config %s: %v: %s", kv[0], err, oneline.Escape(out))
		}
	}
	return g.initErr
}

// Push pushes j's head, or the checkout's head when the result names a commit the
// checkout does not hold (a mistyped sha), to origin's branch.
func (g *Pusher) Push(j PushJob) member.Push {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.init(); err != nil {
		return member.Push{Refused: err.Error()}
	}
	ns := "refs/member/" + j.Launch + "/"
	defer g.drop(ns)
	if out, err := g.git("fetch", "-q", "--no-tags", "--no-write-fetch-head", "--", j.Checkout, "+HEAD:"+ns+"HEAD", "+refs/heads/*:"+ns+"heads/*"); err != nil {
		return member.Push{Refused: "fetch from the checkout: " + oneline.Escape(out)}
	}
	if out, err := g.git("rev-parse", "--verify", "-q", "--end-of-options", j.Base+"^{commit}"); err != nil || !typedrec.IsFullSha(out) {
		return member.Push{None: "the staged commit " + j.Base + " is not in the checkout"}
	}
	full, err := g.git("rev-parse", "--verify", "-q", "--end-of-options", j.Head+"^{commit}")
	if err == nil {
		if on, _ := g.git("for-each-ref", "--count=1", "--format=%(refname)", "--contains", full, ns); on == "" {
			err = errors.New("not on the checkout")
		}
	}
	if err != nil {
		// the result names a commit the checkout does not hold: the checkout's own head,
		// when it descends from the staged commit, is what the child committed
		tip, terr := g.git("rev-parse", "--verify", "-q", ns+"HEAD")
		if terr != nil || g.isAncestor(j.Base, tip) != nil {
			return member.Push{Refused: "the result's head " + j.Head + " is not a commit of the checkout"}
		}
		full = tip
	}
	if err := g.isAncestor(j.Base, full); err != nil {
		return member.Push{Refused: "the head " + full + " does not descend from the staged commit " + j.Base + ": " + err.Error()}
	}
	if n, _ := g.git("rev-list", "--count", "--end-of-options", full, "^"+j.Base); n == "0" {
		return member.Push{None: "the child committed nothing"}
	}
	if why := g.stale(j); why != "" {
		return member.Push{Refused: why}
	}
	if out, err := g.git("push", "-q", "--porcelain", "--no-verify", "--", j.URL, full+":refs/heads/"+j.Branch); err != nil {
		return member.Push{Refused: oneline.Escape(out)}
	}
	g.tidy()
	return member.Push{Sha: full}
}

func (g *Pusher) isAncestor(base, head string) error {
	if out, err := g.git("merge-base", "--is-ancestor", "--end-of-options", base, head); err != nil {
		return fmt.Errorf("git merge-base: %v %s", err, oneline.Escape(out))
	}
	return nil
}

// stale refuses a RESULT.md read from the checkout that is the repository's own file at
// the staged commit: a child that wrote no result would be read by the one it found.
func (g *Pusher) stale(j PushJob) string {
	if j.Result == "" || !strings.HasPrefix(j.Result, j.Checkout+string(os.PathSeparator)) {
		return ""
	}
	tracked, err := g.git("rev-parse", "--verify", "-q", "--end-of-options", j.Base+":RESULT.md")
	if err != nil {
		return ""
	}
	if blob, err := g.git("hash-object", "--no-filters", "--", j.Result); err == nil && blob == tracked {
		return "the RESULT.md read is a stale file from the repository, not this card's result"
	}
	return ""
}

// drop deletes the refs one push fetched.
func (g *Pusher) drop(ns string) {
	refs, err := g.git("for-each-ref", "--format=delete %(refname)", ns)
	if err != nil || refs == "" {
		return
	}
	cmd := exec.Command("git", "-C", g.Repo, "update-ref", "--stdin")
	cmd.Stdin = strings.NewReader(refs + "\n")
	_ = cmd.Run()
}

// tidy repacks and prunes the bare repository at most once in TidyEvery.
func (g *Pusher) tidy() {
	if g.Now == nil {
		return
	}
	now := g.Now()
	if g.tidied.IsZero() {
		g.tidied = now
		return
	}
	if now.Sub(g.tidied) < TidyEvery {
		return
	}
	g.tidied = now
	_, _ = g.git("repack", "-adq")
	_, _ = g.git("prune", "--expire=now")
}

// OpenCodeDB is the harness's own database under a launch's data home.
const OpenCodeDB = "opencode/opencode.db"

// usageSQL is each message's provider, model, tokens and cost.
const usageSQL = `SELECT json_extract(data,'$.modelID'), json_extract(data,'$.providerID'), ` +
	`json_extract(data,'$.tokens.input'), json_extract(data,'$.tokens.output'), json_extract(data,'$.tokens.cache.write'), ` +
	`json_extract(data,'$.tokens.cache.read'), json_extract(data,'$.tokens.reasoning'), json_extract(data,'$.cost') ` +
	`FROM message WHERE json_extract(data,'$.tokens') IS NOT NULL`

// SQLiteQuery runs one read-only statement against a database with sqlite3 and returns
// its rows of tab-separated fields.
func SQLiteQuery(db, sql string) ([][]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sqlite3", "-readonly", "-separator", "\t", db, sql).Output()
	if err != nil {
		return nil, err
	}
	var rows [][]string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			rows = append(rows, strings.Split(l, "\t"))
		}
	}
	return rows, nil
}

// OpenCodeUsage reads what opencode spent from a launch's data home through query.
func OpenCodeUsage(query func(db, sql string) ([][]string, error)) Usage {
	return func(dataHome string) (cardcost.Usage, bool) {
		db := filepath.Join(dataHome, OpenCodeDB)
		if _, err := os.Stat(db); err != nil {
			return cardcost.Usage{}, false
		}
		rows, err := query(db, usageSQL)
		if err != nil {
			return cardcost.Usage{}, false
		}
		return SumUsage(rows), true
	}
}

// SumUsage folds the messages' rows into one record.
func SumUsage(rows [][]string) cardcost.Usage {
	u := cardcost.NoUsage()
	u.Tokens.Input, u.Tokens.Output, u.Tokens.CacheWrite, u.Tokens.CacheRead, u.Tokens.Reasoning, u.Tokens.Requests = 0, 0, 0, 0, 0, 0
	cost := 0.0
	for _, r := range rows {
		if len(r) < 8 {
			continue
		}
		if u.Model == "" && r[0] != "" {
			u.Model = r[1] + "/" + r[0]
		}
		for i, n := range []*int64{&u.Tokens.Input, &u.Tokens.Output, &u.Tokens.CacheWrite, &u.Tokens.CacheRead, &u.Tokens.Reasoning} {
			v, _ := strconv.ParseInt(r[2+i], 10, 64)
			*n += max(v, 0)
		}
		c, _ := strconv.ParseFloat(r[7], 64)
		cost += max(c, 0)
		u.Tokens.Requests++
	}
	if len(rows) > 0 {
		u.Actual, u.ActualBy = strconv.FormatFloat(math.Round(cost*1e9)/1e9, 'f', -1, 64), cardcost.ActualByHarness
	}
	return u
}
