package git

import (
	"fmt"
	"strings"

	"context"
)

// Branches is every branch at the clone address url whose name starts
// with prefix (a/b/ lists a/b/*), by name without refs/heads/, with its
// tip: one `git ls-remote --heads` and no clone (admission's read of the
// card branches already at origin).
func (c *Client) Branches(ctx context.Context, url, prefix string) (map[string]string, error) {
	if url == "" {
		return nil, fmt.Errorf("git refused: branches need a clone address")
	}
	args := []string{"ls-remote", "--heads", url, "refs/heads/" + prefix + "*"}
	stdout, stderr, code, err := c.runGit_internal(ctx, "", c.cfg.PushTimeout, args...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, c.failed_internal(code, stdout, stderr, nil, args...)
	}
	out := map[string]string{}
	for _, line := range strings.Split(stdout, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		name, ok := strings.CutPrefix(f[1], "refs/heads/")
		if !ok || !isFullSHA_internal(f[0]) || !strings.HasPrefix(name, prefix) {
			return nil, c.failed_internal(code, stdout, stderr, fmt.Errorf("a line is not <sha> refs/heads/%s...: %q", prefix, line), args...)
		}
		out[name] = strings.ToLower(f[0])
	}
	return out, nil
}

// ListTree is every file path at rev in the clone at repoDir (`git ls-tree
// -r --name-only`), the tree admission checks a card's PATHS against.
func (c *Client) ListTree(ctx context.Context, repoDir, rev string) ([]string, error) {
	args := []string{"ls-tree", "-r", "--name-only", "-z", rev}
	stdout, stderr, code, err := c.runGit_internal(ctx, repoDir, c.cfg.RebaseTimeout, args...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, c.failed_internal(code, stdout, stderr, nil, args...)
	}
	var out []string
	for _, p := range strings.Split(stdout, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}
