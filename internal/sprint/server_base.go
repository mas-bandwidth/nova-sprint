package sprint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/buildinfo"
	"github.com/mas-bandwidth/nova-sprint/internal/gitrun"
)

// The server is built from the sprint base and from nothing else (docs/SPEC-SPRINT.md
// section 14). server switch fetches origin's base and refuses a candidate whose
// build commit is not an ancestor of that tip. The tick raises one judgment while
// the running server is off the base the clone already has, and does not fetch:
// a fetch inside a tick would hold the tick for the network.

// ServerBaseFileSuffix is the file server switch writes beside the target,
// `<target>.serverbase`, naming the clone and the base the tick reads back.
const ServerBaseFileSuffix = ".serverbase"

// ServerBaseFact is what a tick is told about the running server. Checked is
// false when this tick was not given a clone (nothing is raised and nothing
// open is closed). Off is set while the build commit is missing or is not an
// ancestor of origin/<base>; What is the judgment's one line.
type ServerBaseFact struct {
	Checked bool
	Off     bool
	What    string
}

// BaseCheck is one binary against one sprint base.
type BaseCheck struct {
	Base     string
	Commit   string
	Tip      string
	NoCommit bool
	On       bool
}

var (
	sprintBaseName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)
	commitHex      = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
)

func validBase(base string) bool {
	return sprintBaseName.MatchString(base) && !strings.Contains(base, "..") && !strings.Contains(base, "//")
}

// baseRemedy is what a refusal and the judgment both tell the operator to do.
func baseRemedy(base string) string {
	return "build from origin/" + base + " at its tip, then switch"
}

// Refusal is the server switch error, or "" when the binary is on the base.
func (c BaseCheck) Refusal() string {
	if c.On {
		return ""
	}
	if c.NoCommit {
		return "server switch REFUSED: candidate binary has no source commit; " + baseRemedy(c.Base)
	}
	return fmt.Sprintf("server switch REFUSED: the candidate binary was built from commit %s which is not an ancestor of origin/%s (%s); %s", c.Commit, c.Base, c.Tip, baseRemedy(c.Base))
}

// Judgment is the tick's line while the server is off the base.
func (c BaseCheck) Judgment() string {
	if c.NoCommit {
		return "the running server has no source commit; " + baseRemedy(c.Base)
	}
	return fmt.Sprintf("the running server was built from %s which is not an ancestor of origin/%s (%s); %s", c.Commit, c.Base, c.Tip, baseRemedy(c.Base))
}

// CheckBinaryOnBase reads the binary's stamped build commit and reports whether
// it is an ancestor of origin/<base> in repo. fetch asks origin for the base
// first (server switch). The tick passes fetch false and uses the ref the clone
// already has, so the tick does not go to the network. timeout 0 is git's own
// budget. A binary with no revision is NoCommit, not an error.
func CheckBinaryOnBase(ctx context.Context, binary, repo, base string, timeout time.Duration, fetch bool) (BaseCheck, error) {
	if !validBase(base) {
		return BaseCheck{}, fmt.Errorf("sprint base %q is not a branch name", base)
	}
	rev, _, ok, err := buildinfo.BinaryRevision(binary)
	rev = strings.ToLower(rev)
	if err != nil || !ok || !commitHex.MatchString(rev) {
		return BaseCheck{Base: base, NoCommit: true}, nil
	}
	tip, err := readBaseTip(ctx, repo, base, timeout, fetch)
	if err != nil {
		return BaseCheck{}, err
	}
	on, err := commitIsAncestor(ctx, repo, rev, tip, timeout)
	if err != nil {
		return BaseCheck{}, err
	}
	return BaseCheck{Base: base, Commit: rev, Tip: tip, On: on}, nil
}

func readBaseTip(ctx context.Context, repo, base string, timeout time.Duration, fetch bool) (string, error) {
	o := gitrun.Options{C: repo, OwnRepo: true, Timeout: timeout}
	ref := "refs/remotes/origin/" + base
	if fetch {
		out, err := gitrun.Combined(ctx, o, "fetch", "--no-tags", "-q", "origin", "+refs/heads/"+base+":"+ref)
		if err != nil {
			return "", fmt.Errorf("cannot fetch origin/%s in %s: %s", base, repo, baseFetchLine(strings.TrimSpace(string(out)+" "+err.Error())))
		}
	}
	tip, err := gitrun.Output(ctx, o, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("origin/%s is not in %s", base, repo)
	}
	return tip, nil
}

func commitIsAncestor(ctx context.Context, repo, commit, tip string, timeout time.Duration) (bool, error) {
	o := gitrun.Options{C: repo, OwnRepo: true, Timeout: timeout}
	_, err := gitrun.Run(ctx, o, "merge-base", "--is-ancestor", commit, tip)
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && (exit.ExitCode() == 1 || exit.ExitCode() == 128) {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor %s %s in %s: %w", commit, tip, repo, err)
}

func baseFetchLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// ObserveServerBase is the tick's check: the clone's already-fetched origin/<base>,
// no fetch. An error means the base could not be read; the tick then checks nothing.
func ObserveServerBase(ctx context.Context, binary, repo, base string) (ServerBaseFact, error) {
	chk, err := CheckBinaryOnBase(ctx, binary, repo, base, 2*time.Second, false)
	if err != nil {
		return ServerBaseFact{}, err
	}
	if chk.NoCommit || !chk.On {
		return ServerBaseFact{Checked: true, Off: true, What: chk.Judgment()}, nil
	}
	return ServerBaseFact{Checked: true}, nil
}

// WriteServerBaseFile records the clone and the base beside a switched binary.
func WriteServerBaseFile(path, repo, base string) error {
	return os.WriteFile(path, []byte(repo+"\n"+base+"\n"), 0o644)
}

// ReadServerBaseFile is the clone and base a switch wrote, ok false when the
// file is absent or not that shape.
func ReadServerBaseFile(path string) (repo, base string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(b) == 0 || len(b) > 4096 {
		return "", "", false
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	repo = strings.TrimSpace(lines[0])
	if repo == "" {
		return "", "", false
	}
	base = "main"
	if len(lines) >= 2 {
		if s := strings.TrimSpace(lines[1]); s != "" {
			base = s
		}
	}
	if !validBase(base) {
		return "", "", false
	}
	return repo, base, true
}
