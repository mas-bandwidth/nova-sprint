package worker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
)

// A repository's SIBLINGS file names the checkouts its build reads beside
// it, one per line: `<path> <clone-url> <tag> <40-hex-sha>`, the path
// `../<name>`; blank lines and whole-line `#` comments are allowed, and no
// file is no siblings. Staging reads it at the stream's base in the worker's
// own work clone (never at a card's head), checks every line before any
// fetch, and stages each sibling: its tag fetched into the bench's bare
// mirror <MirrorRoot>/<name>.git from the forge with the worker's one
// credential (on the stream's own origin only), its tree made once per sha
// at <SiblingsRoot>/<name>/<sha>, read-only and shared, linked from the job
// directory as ../<name> of the worktree, and granted to the wall --read.
// Any failure is a staging fault (tla/Runner.tla StageFail: taken to
// reporting with outcome fault), so the card goes back to ready.

// SiblingsName is the file at a repository's root that names its siblings.
const SiblingsName = "SIBLINGS"

// SiblingGrantsName is the file, in the worker's kept copy of a job
// directory (keptDir_internal, outside the wall's reach), listing the
// real paths of the sibling trees staged for that job: the wall's --read.
const SiblingGrantsName = "siblings"

type sibling_internal struct {
	Line                int
	Name, URL, Tag, SHA string
}

func (s sibling_internal) String() string { return "../" + s.Name + " " + s.URL + "@" + s.Tag }

var (
	siblingName_internal = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	siblingSHA_internal  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// jobDirEntries_internal are the names the worker uses in a job
	// directory, which a sibling never takes.
	jobDirEntries_internal = []string{"repo", "home", "tmp", "data", "job.json", ReceiptName, JobMDName, "result.json", "usage.json", "commit-msg.txt", "jobserver.fifo"}
)

// parseSiblings_internal is data, the SIBLINGS file of the job repo
// repoName at repoURL, every line checked; the first bad line refuses the
// whole file.
func parseSiblings_internal(data, repoURL, repoName string) ([]sibling_internal, error) {
	origin, err := cloneOrigin_internal(repoURL)
	if err != nil {
		return nil, fmt.Errorf("the stream's clone URL: %w", err)
	}
	var out []sibling_internal
	sc := bufio.NewScanner(strings.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		s, why := parseSiblingLine_internal(text, origin, repoName)
		if why == "" && slices.ContainsFunc(out, func(o sibling_internal) bool { return strings.EqualFold(o.Name, s.Name) }) {
			why = "../" + s.Name + " is named twice"
		}
		if why != "" {
			return nil, fmt.Errorf("%s line %d %q: %s", SiblingsName, n, text, why)
		}
		s.Line = n
		out = append(out, s)
	}
	return out, sc.Err()
}

// parseSiblingLine_internal is one line's sibling, or why it is refused.
func parseSiblingLine_internal(text, origin, repoName string) (sibling_internal, string) {
	f := strings.Fields(text)
	if len(f) != 4 {
		return sibling_internal{}, "want four fields: <path> <clone-url> <tag> <40-hex-sha>"
	}
	name, ok := strings.CutPrefix(f[0], "../")
	if !ok || !siblingName_internal.MatchString(name) || name == "." || strings.Contains(name, "..") {
		return sibling_internal{}, "the path must be ../<name>, the name of letters, digits, '.', '_' and '-' (no '..')"
	}
	if strings.EqualFold(name, repoName) {
		return sibling_internal{}, "../" + name + " is the job's own repository"
	}
	if slices.ContainsFunc(jobDirEntries_internal, func(e string) bool { return strings.EqualFold(e, name) }) || strings.HasSuffix(strings.ToLower(name), ".tmp") {
		return sibling_internal{}, "../" + name + " is one of the job directory's own entries"
	}
	if o, err := cloneOrigin_internal(f[1]); err != nil {
		return sibling_internal{}, err.Error()
	} else if o != origin {
		return sibling_internal{}, "the clone URL is on " + o + ", not the stream's " + origin + ": the worker's credential goes to the stream's host alone"
	}
	if !validTag_internal(f[2]) {
		return sibling_internal{}, "the tag " + f[2] + " is not a ref name git takes (git check-ref-format)"
	}
	if !siblingSHA_internal.MatchString(f[3]) {
		return sibling_internal{}, "the sha must be 40 lowercase hex"
	}
	return sibling_internal{Name: name, URL: f[1], Tag: f[2], SHA: f[3]}, ""
}

// cloneOrigin_internal is where a clone URL fetches from: <scheme>://<host>
// in lower case (the user part aside); "file://" for a file:// URL or an
// absolute path, both on the local file system; ssh://<host> for the
// scp-like form. Anything else is no clone URL a sibling may name.
func cloneOrigin_internal(u string) (string, error) {
	switch {
	case strings.HasPrefix(u, "-"):
		return "", fmt.Errorf("%q is not a clone URL", u)
	case strings.HasPrefix(u, "/"):
		return "file://", nil
	case strings.Contains(u, "://"):
		p, err := url.Parse(u)
		if err != nil || p.Scheme == "" {
			return "", fmt.Errorf("%q is not a clone URL (%v)", u, err)
		}
		if strings.EqualFold(p.Scheme, "file") {
			return "file://", nil
		}
		if p.Host == "" {
			return "", fmt.Errorf("%q is not a clone URL: it names no host", u)
		}
		return strings.ToLower(p.Scheme + "://" + p.Host), nil
	}
	host, _, ok := strings.Cut(u, ":")
	if !ok || host == "" || strings.Contains(host, "/") {
		return "", fmt.Errorf("%q is not a clone URL: give a URL, an absolute path or <host>:<path>", u)
	}
	if _, h, at := strings.Cut(host, "@"); at {
		host = h
	}
	return "ssh://" + strings.ToLower(host), nil
}

// validTag_internal is git check-ref-format's rules for a tag's name.
func validTag_internal(tag string) bool {
	if tag == "" || tag == "@" || strings.HasPrefix(tag, "-") || strings.HasSuffix(tag, "/") || strings.HasSuffix(tag, ".") ||
		strings.Contains(tag, "..") || strings.Contains(tag, "//") || strings.Contains(tag, "@{") || strings.ContainsAny(tag, " ~^:?*[\\\x7f") {
		return false
	}
	for _, r := range tag {
		if r < 0x20 {
			return false
		}
	}
	for c := range strings.SplitSeq(tag, "/") {
		if strings.HasPrefix(c, ".") || strings.HasSuffix(c, ".lock") {
			return false
		}
	}
	return true
}

// stageSiblings_internal stages the siblings j's repo names at j's base
// (read in the work clone under lock's read side), each linked beside j's
// worktree, and keeps their trees as j's wall grants. A failure names the
// sibling; a malformed file stages none.
func (g *OSGitOps) stageSiblings_internal(ctx context.Context, j Job, url, name, work string, lock *sync.RWMutex) error {
	lock.RLock()
	sibs, err := g.readSiblings_internal(ctx, j.BaseSHA, url, name, work)
	lock.RUnlock()
	if err != nil {
		return err
	}
	var trees []string
	for _, s := range sibs {
		tree, err := g.stageSibling_internal(ctx, s)
		if err == nil {
			err = placeSibling_internal(j.JobDir, s.Name, tree)
		}
		if err != nil {
			return fmt.Errorf("sibling %s: %w", s, err)
		}
		trees = append(trees, tree)
	}
	return keepSiblingGrants_internal(j.JobDir, trees)
}

// readSiblings_internal is the SIBLINGS file at base in the work clone,
// parsed; none when base is unknown or the file is absent there.
func (g *OSGitOps) readSiblings_internal(ctx context.Context, base, url, name, work string) ([]sibling_internal, error) {
	if base == "" {
		return nil, nil
	}
	to := g.to_internal(g.Timeouts.WithDefaults().Stage)
	out, err := g.runGit_internal(ctx, to, work, "ls-tree", base, "--", SiblingsName)
	if err != nil {
		return nil, fmt.Errorf("read %s at %s: %w", SiblingsName, base, err)
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	meta, _, _ := strings.Cut(out, "\t")
	f := strings.Fields(meta)
	if len(f) != 3 || f[1] != "blob" || (f[0] != "100644" && f[0] != "100755") {
		return nil, fmt.Errorf("%s at %s is %q, not a file", SiblingsName, base, strings.TrimSpace(out))
	}
	data, err := g.runGit_internal(ctx, to, work, "cat-file", "blob", f[2])
	if err != nil {
		return nil, fmt.Errorf("read %s at %s: %w", SiblingsName, base, err)
	}
	return parseSiblings_internal(data, url, name)
}

// stageSibling_internal is s's tree, staged once per sha under the
// bench-wide lock on it: reused when present (checked at the sha and
// read-only), else made from the mirror into <tree>.tmp, checked, renamed
// into place and made read-only. Its real path.
func (g *OSGitOps) stageSibling_internal(ctx context.Context, s sibling_internal) (string, error) {
	if g.MirrorRoot == "" || g.SiblingsRoot == "" {
		return "", errors.New("the worker has no mirror root or siblings root to stage it in")
	}
	tree := filepath.Join(g.SiblingsRoot, s.Name, s.SHA)
	f, err := git.LockClone(ctx, tree)
	if err != nil {
		return "", err
	}
	defer f.Close()
	to := g.to_internal(g.Timeouts.WithDefaults().Stage)
	if !fileExists_internal(tree) {
		if err := g.fetchSibling_internal(ctx, s); err != nil {
			return "", err
		}
		tmp := tree + ".tmp"
		if err := os.RemoveAll(tmp); err != nil {
			return "", err
		}
		if _, err := g.runGit_internal(ctx, to, "", "init", "--quiet", tmp); err != nil {
			return "", fmt.Errorf("make the tree: %w", err)
		}
		mirror := filepath.Join(g.MirrorRoot, s.Name+".git")
		for _, args := range [][]string{{"fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-auto-gc", "--depth=1", mirror, s.SHA}, {"checkout", "--quiet", "--detach", s.SHA}} {
			if _, err := g.runGit_internal(ctx, to, tmp, args...); err != nil {
				return "", fmt.Errorf("make the tree: %w", err)
			}
		}
		if err := g.checkTree_internal(ctx, tmp, s.SHA); err != nil {
			return "", err
		}
		if err := os.Rename(tmp, tree); err != nil {
			return "", err
		}
	} else if err := g.checkTree_internal(ctx, tree, s.SHA); err != nil {
		return "", err
	}
	if err := readOnly_internal(tree); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(tree)
}

// fetchSibling_internal fetches s's tag into its bare mirror from s's
// clone URL with the credential (a stage hop, shallow as the mirror is),
// under the mirror's bench-wide lock, and checks it names s's sha.
func (g *OSGitOps) fetchSibling_internal(ctx context.Context, s sibling_internal) error {
	mirror := filepath.Join(g.MirrorRoot, s.Name+".git")
	unlock, err := g.lockMirror_internal(ctx, s.Name)
	if err != nil {
		return err
	}
	to := g.to_internal(g.Timeouts.WithDefaults().Stage)
	if !fileExists_internal(mirror) {
		_, err = g.runGit_internal(ctx, to, "", "init", "--quiet", "--bare", mirror)
	}
	ref := "refs/tags/" + s.Tag
	if err == nil {
		err = g.fetchHops_internal(ctx, []stageHop_internal{{dir: mirror, gitDir: mirror, src: s.URL, remote: true}}, []string{"+" + ref + ":" + ref}, nil, nil)
	}
	got := ""
	if err == nil {
		got, err = g.runGit_internal(ctx, to, mirror, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	}
	if uerr := unlock(); err == nil {
		err = uerr
	}
	if err != nil {
		return fmt.Errorf("fetch the tag: %w", err)
	}
	if got = strings.TrimSpace(got); got != s.SHA {
		return fmt.Errorf("the tag is at %s, not the pinned %s", got, s.SHA)
	}
	return nil
}

// checkTree_internal: the tree's checkout is at sha.
func (g *OSGitOps) checkTree_internal(ctx context.Context, tree, sha string) error {
	got, err := g.headSHA_internal(ctx, tree, "HEAD")
	if err != nil {
		return fmt.Errorf("check the tree %s: %w", tree, err)
	}
	if got != sha {
		return fmt.Errorf("the tree %s is at %s, not the pinned %s", tree, got, sha)
	}
	return nil
}

// readOnly_internal takes the write bits off everything under root but
// its links, root last; nothing to do when root already has none (only a
// stop between a tree's rename and this leaves it writable).
func readOnly_internal(root string) error {
	st, err := os.Lstat(root)
	if err != nil || st.Mode().Perm()&0o222 == 0 {
		return err
	}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == root || d.Type()&fs.ModeSymlink != 0 {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.Chmod(p, info.Mode().Perm()&^0o222)
	})
	if err != nil {
		return err
	}
	return os.Chmod(root, st.Mode().Perm()&^0o222)
}

// placeSibling_internal links <jobDir>/<name> (../<name> of the worktree)
// to tree; a link already there is replaced, anything else is refused.
func placeSibling_internal(jobDir, name, tree string) error {
	link := filepath.Join(jobDir, name)
	if st, err := os.Lstat(link); err == nil {
		if st.Mode()&fs.ModeSymlink == 0 {
			return fmt.Errorf("the job directory already holds %s, not a link the worker made", link)
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Symlink(tree, link)
}

// keepSiblingGrants_internal keeps trees as the job's wall grants in the
// worker's copy of jobDir (SiblingGrantsName); none removes the file.
func keepSiblingGrants_internal(jobDir string, trees []string) error {
	kept := keptDir_internal(jobDir)
	if kept == "" {
		return nil
	}
	path := filepath.Join(kept, SiblingGrantsName)
	if len(trees) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(kept, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path+".tmp", []byte(strings.Join(trees, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// siblingGrants_internal are the sibling trees kept for jobDir's job, each
// an absolute path; none when it has none.
func siblingGrants_internal(jobDir string) []string {
	kept := keptDir_internal(jobDir)
	if kept == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(kept, SiblingGrantsName))
	if err != nil {
		return nil
	}
	return slices.DeleteFunc(strings.Split(string(data), "\n"), func(p string) bool { return !filepath.IsAbs(p) })
}
