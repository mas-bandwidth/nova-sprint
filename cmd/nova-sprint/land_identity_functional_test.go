//go:build functional

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/redis/go-redis/v9"

	"github.com/mas-bandwidth/nova-sprint/pkg/gitrun"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
)

func TestMachineLandingIdentitySurvivesHostGitConfig(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	global := filepath.Join(xdg, "git", "config")
	require.NoError(t, os.MkdirAll(filepath.Dir(global), 0o700))
	require.NoError(t, os.WriteFile(global, []byte("[user]\n\tname = Fiedler\n\temail = glenn@networknext.com\n"), 0o600))
	gitEnv := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + xdg, "GIT_CONFIG_NOSYSTEM=1"}
	for _, kv := range gitEnv {
		require.NotContains(t, kv, "NOVA_")
	}
	env := map[string]string{"HOME": home, "XDG_CONFIG_HOME": xdg,
		"NOVA_SPRINT_GIT_NAME": "Stella Machine", "NOVA_SPRINT_GIT_EMAIL": "stella-machine@example.invalid"}
	a := newApp(func(k string) string { return env[k] })
	a.gitEnv = gitEnv
	defer a.close()
	a.forward = func(_ context.Context, addr string, verbs ...[]string) ([]sprintwire.Result, error) {
		t.Fatalf("a verb was forwarded to a sprint server at %s: %q", addr, verbs)
		return nil, errors.New("forwarding is refused in this test")
	}
	require.Empty(t, func() string { s, _ := a.namedServer(); return s }(), "no sprint server is named in the test environment")
	private := filepath.Join(home, "repo.git")
	worker, clone := filepath.Join(home, "worker"), filepath.Join(home, "land")
	functionalGit(t, gitEnv, "", "init", "-q", "--bare", "--initial-branch=main", private)
	functionalGit(t, gitEnv, "", "clone", "-q", private, worker)
	functionalGit(t, gitEnv, worker, "config", "user.name", "Worker")
	functionalGit(t, gitEnv, worker, "config", "user.email", "worker@example.invalid")
	require.NoError(t, os.WriteFile(filepath.Join(worker, "README"), []byte("base\n"), 0o600))
	functionalGit(t, gitEnv, worker, "add", "README")
	functionalGit(t, gitEnv, worker, "commit", "-q", "-m", "base")
	functionalGit(t, gitEnv, worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	functionalGit(t, gitEnv, "", "clone", "-q", private, clone)

	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	require.NoError(t, fn.Load(context.Background(), client))
	run := func(line string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := a.run(append(strings.Fields(line), "--redis", addr), &out, &errb)
		require.Zero(t, code, "%s: exit %d\n%s%s", line, code, out.String(), errb.String())
		return out.String()
	}
	for _, line := range []string{"init --readers reader-a,reader-b --members m1", "add --stream s1 --count 1 --one", "fleet beat m1", "start", "tick", "tick"} {
		run(line)
	}
	functionalGit(t, gitEnv, worker, "switch", "-q", "-c", "sprint/s1-1", "refs/remotes/origin/main")
	require.NoError(t, os.WriteFile(filepath.Join(worker, "work.txt"), []byte("machine change\n"), 0o600))
	functionalGit(t, gitEnv, worker, "add", "work.txt")
	functionalGit(t, gitEnv, worker, "commit", "-q", "-m", "card work")
	head := functionalGit(t, gitEnv, worker, "rev-parse", "HEAD")
	functionalGit(t, gitEnv, worker, "push", "-q", "origin", "HEAD:refs/heads/sprint/s1-1")
	run("take --as m1 --epoch 0 --limit 10")
	run("finish --as m1 s1-1.w1@1 --epoch 0 --head " + head + " --report done")
	for _, line := range []string{"queue --as reader-a", "queue --as reader-b", "tick",
		"read --as reader-a --begin --epoch 0 --limit 10", "read --as reader-a --ok --epoch 0 --limit 10",
		"read --as reader-b --begin --epoch 0 --limit 10", "read --as reader-b --ok --epoch 0 --limit 10"} {
		run(line)
	}
	run("accept --read-ok")
	run("stream set s1 --land-protected any")
	run("land --repo-dir " + clone + " --base main")
	got := functionalGit(t, gitEnv, private, "log", "-1", "--format=%an <%ae>|%cn <%ce>", "main")
	require.Equal(t, "Stella Machine <stella-machine@example.invalid>|Stella Machine <stella-machine@example.invalid>", got)
}

func functionalGit(t *testing.T, env []string, dir string, args ...string) string {
	t.Helper()
	res, err := gitrun.Run(context.Background(), gitrun.Options{C: dir, Env: env, OwnRepo: dir != ""}, args...)
	require.NoError(t, err, "git %v: %s", args, strings.TrimSpace(string(res.Stderr)+"\n"+string(res.Stdout)))
	return strings.TrimSpace(string(res.Stdout))
}

