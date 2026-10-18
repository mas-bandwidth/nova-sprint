package main

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/redisconn"
	"github.com/mas-bandwidth/nova-tools/pkg/secrets"
)

// loginApp is a test app with the seat login on, as main has it: no --redis and no
// NOVA_SPRINT_REDIS* in its environment, its config under dir, its secrets a fake that
// holds one password, and its store the in-memory one, opened through the real path
// (redisBackend, storeOptions) and recorded at each dial with the user and the
// password its login resolves to.
type loginApp struct {
	*testApp
	env   map[string]string
	dials []dialedLogin
	reads []secrets.Login
}

type dialedLogin struct{ addr, user, password string }

const loginPW = "Zq7xWp4Lk9mN2vB8tY"

func newLoginApp(t *testing.T, dir string) *loginApp {
	la := &loginApp{testApp: newTestApp(t), env: map[string]string{"NOVA_SPRINT_ACTOR": "coordinator", "XDG_CONFIG_HOME": filepath.Join(dir, "config")}}
	la.a.getenv = func(k string) string { return la.env[k] }
	la.a.seatLoginOn()
	la.a.backend = la.a.redisBackend
	la.a.loginSecret = func(l secrets.Login) (secrets.Secret, error) {
		la.reads = append(la.reads, l)
		if l.Name != "NOVA_REDIS_COORDINATOR_PASSWORD" {
			return secrets.Secret{}, errors.New("seat " + l.As + " of store " + l.Store + " holds no " + l.Name)
		}
		return secrets.NewSecret(loginPW), nil
	}
	la.a.dialStore = func(_ context.Context, addr string, o redisconn.Options, getenv func(string) string, _ sprint.Names) (store.Backend, error) {
		r, err := redisconn.Resolve(o, getenv) // the login as redisconn.Open would make it
		require.NoError(t, err)
		pw := ""
		if r.PasswordEnv != "" {
			pw = getenv(r.PasswordEnv)
		}
		la.dials = append(la.dials, dialedLogin{addr, r.User, pw})
		return la.m, nil
	}
	return la
}

// seat-store-login-built-in: nova-sprint <verb> typed bare by the coordinator reaches the
// store with the coordinator login, the password read in process from nova-secrets and
// never printed, put in an environment or written to a file, with no wrapper script.
func TestABareVerbOpensTheStoreWithTheSeatLoginFromSecrets(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	la := newLoginApp(t, dir)
	file := filepath.Join(dir, "config", "nova-sprint", "login.json")
	var said []string
	do := func(line string) (int, string, string) {
		code, out, errs := la.do(line)
		said = append(said, out, errs)
		return code, out, errs
	}

	// bare, with nothing recorded: the refusal names the login as a way on
	code, _, errs := do("where")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "a login recorded by nova-sprint seat login")
	assert.Empty(t, la.dials)

	login := "seat login --store " + filepath.Join(dir, "secrets") + " --as studio --key " + filepath.Join(dir, "studio.key") +
		" --sops /usr/local/bin/sops --secret NOVA_REDIS_COORDINATOR_PASSWORD --user coordinator --redis 127.0.0.1:6380"
	code, out, errs := do(login)
	require.Equal(t, 0, code, errs)
	assert.Contains(t, out, "SEAT LOGIN RECORDED file="+file)
	assert.Contains(t, out, "user=coordinator")
	assert.Contains(t, out, "resolves=yes")
	assert.Empty(t, la.dials, "seat login records; it opens no store")

	// the record holds where the password is, never the password, for its user alone
	b, err := os.ReadFile(file)
	require.NoError(t, err)
	var rec storeLogin
	require.NoError(t, json.Unmarshal(b, &rec))
	assert.Equal(t, storeLogin{Redis: "127.0.0.1:6380", User: "coordinator", Store: filepath.Join(dir, "secrets"), As: "studio",
		Key: filepath.Join(dir, "studio.key"), Sops: "/usr/local/bin/sops", Secret: "NOVA_REDIS_COORDINATOR_PASSWORD"}, rec)
	fi, err := os.Stat(file)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())

	// bare verbs now reach the recorded store as the recorded user, the password read
	// from the seat in this process
	code, _, errs = do("init --readers reader-a --members m1")
	require.Equal(t, 0, code, errs)
	code, _, errs = do("where")
	require.Equal(t, 0, code, errs)
	require.Len(t, la.dials, 1, "one dial per process and address")
	assert.Equal(t, dialedLogin{"127.0.0.1:6380", "coordinator", loginPW}, la.dials[0])
	require.NotEmpty(t, la.reads)
	assert.Equal(t, secrets.Login{Store: filepath.Join(dir, "secrets"), As: "studio", Key: filepath.Join(dir, "studio.key"), Sops: "/usr/local/bin/sops", Name: "NOVA_REDIS_COORDINATOR_PASSWORD"}, la.reads[len(la.reads)-1])

	code, out, errs = do("seat login --check")
	require.Equal(t, 0, code, errs)
	assert.Contains(t, out, "SEAT LOGIN file="+file+" redis=127.0.0.1:6380 user=coordinator")
	assert.Contains(t, out, "secret=NOVA_REDIS_COORDINATOR_PASSWORD resolves=yes")

	// an explicit --redis wins, and the login is not taken to another store
	code, _, errs = do("where --redis 127.0.0.1:7000")
	require.Equal(t, 0, code, errs)
	assert.Equal(t, dialedLogin{"127.0.0.1:7000", "", ""}, la.dials[len(la.dials)-1])

	// the environment's login wins over the recorded one
	envd := newLoginApp(t, dir)
	envd.m = la.m
	envd.env["NOVA_SPRINT_REDIS_USER"] = "bench"
	envd.env["NOVA_SPRINT_REDIS_PASSWORD_ENV"] = "BENCH_PW"
	envd.env["BENCH_PW"] = "bench-pw"
	code, _, errs = envd.do("where")
	require.Equal(t, 0, code, errs)
	assert.Equal(t, []dialedLogin{{"127.0.0.1:6380", "bench", "bench-pw"}}, envd.dials)
	assert.Empty(t, envd.reads, "the secret is not read when the environment's login wins")

	// a secret the seat does not hold is a refusal naming the setting and the remedy,
	// never a login with no password
	rec.Secret = "NOVA_REDIS_GONE"
	b, err = json.Marshal(rec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, b, 0o600))
	gone := newLoginApp(t, dir)
	code, _, errs = gone.do("where")
	said = append(said, errs)
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "the seat login "+file)
	assert.Contains(t, errs, "NOVA_REDIS_GONE")
	assert.Contains(t, errs, "run: nova-sprint seat login --check")
	assert.Empty(t, gone.dials, "no store is opened without its password")
	code, out, errs = gone.do("seat login --check")
	said = append(said, out, errs)
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "resolves=no")
	code, _, errs = gone.do(strings.Replace(login, "NOVA_REDIS_COORDINATOR_PASSWORD", "NOVA_REDIS_GONE", 1))
	said = append(said, errs)
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "nothing was recorded")

	// a file that is not a login is refused as it is, never passed over
	require.NoError(t, os.WriteFile(file, []byte("{"), 0o600))
	code, _, errs = newLoginApp(t, dir).do("where")
	said = append(said, errs)
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "is not a login")
	assert.Contains(t, errs, "run: nova-sprint seat logout")

	// logout removes the record: a bare verb is refused again
	code, out, errs = do("seat logout")
	require.Equal(t, 0, code, errs)
	assert.Contains(t, out, "was=recorded")
	assert.NoFileExists(t, file)
	code, out, _ = do("seat logout")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "was=none")
	code, _, _ = newLoginApp(t, dir).do("where")
	assert.Equal(t, 2, code)

	// the password is in no output, no environment a child would inherit, and no file
	pw := secrets.NewSecret(loginPW)
	for _, s := range said {
		assert.False(t, secrets.Leaks(s, pw), "output leaks the password: %q", s)
	}
	assert.False(t, secrets.Leaks(strings.Join(os.Environ(), "\n"), pw), "the process environment holds the password")
	require.NoError(t, filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		require.NoError(t, err)
		assert.False(t, secrets.Leaks(string(b), pw), "%s holds the password", p)
		return nil
	}))
}

// seat login -h and seat logout -h are their help at exit 0, and record nothing.
func TestSeatLoginHelp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	la := newLoginApp(t, dir)
	code, out, _ := la.do("seat login -h")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "nova-sprint seat login --store <secrets dir>")
	assert.Contains(t, out, "-secret")
	code, out, _ = la.do("seat logout --help")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "seat logout removes the recorded seat login")
	assert.NoDirExists(t, filepath.Join(dir, "config"))
}

// The startup scrub ahead of the real store path: the launcher's shape (the user, the
// pointer NOVA_SPRINT_REDIS_PASSWORD_ENV, the value under the name it gives) scrubbed out
// of a fake environment, and the store is still dialed with the password, read through
// the app's getenv from the holder; the environment the app's children would inherit
// holds none of it.
func TestScrubbedSecretStillDialsTheStore(t *testing.T) {
	t.Parallel()
	secret := unitSecret(t)
	la := newLoginApp(t, t.TempDir())
	for k, v := range map[string]string{"NOVA_SPRINT_REDIS": "127.0.0.1:6380", "NOVA_SPRINT_REDIS_USER": "sprinter",
		"NOVA_SPRINT_REDIS_PASSWORD_ENV": "FAKE_STORE_PASSWORD", "FAKE_STORE_PASSWORD": secret} {
		la.env[k] = v
	}
	held, err := scrubEnv(environOf(la.env), func(k string) error { delete(la.env, k); return nil })
	require.NoError(t, err)
	la.a.getenv = held.getenv(la.a.getenv)
	for _, kv := range environOf(la.env) {
		assert.False(t, strings.Contains(kv, secret), "the environment a child inherits holds no byte of it")
	}

	code, out, errs := la.do("init --readers reader-a --members m1")
	require.Equal(t, 0, code, errs)
	require.Len(t, la.dials, 1)
	assert.Equal(t, dialedLogin{"127.0.0.1:6380", "sprinter", secret}, la.dials[0], "the store is dialed with the held password")
	assert.False(t, strings.Contains(out+errs, secret), "no output holds it")
}

// declaredFlag is one flag a FlagSet call of the source declares: its name and its usage
// ("" when the usage is not a literal, which the judge then refuses for a secret name).
type declaredFlag struct{ at, name, usage string }

// flagMethods is the FlagSet methods that declare a flag, with the index of the name.
var flagMethods = map[string]int{
	"String": 0, "Bool": 0, "Int": 0, "Int64": 0, "Uint": 0, "Uint64": 0, "Float64": 0, "Duration": 0, "Func": 0, "BoolFunc": 0, "TextVar": 1,
	"StringVar": 1, "BoolVar": 1, "IntVar": 1, "Int64Var": 1, "UintVar": 1, "Uint64Var": 1, "Float64Var": 1, "DurationVar": 1, "Var": 1,
}

// declaredFlags is every flag the Go source declares through a FlagSet method with a
// literal name: every verb's flags and every sub-verb's (seat login is cmdSeatLogin's),
// whichever FlagSet they are declared on.
func declaredFlags(t *testing.T, fset *gotoken.FileSet, files map[string]*ast.File) []declaredFlag {
	t.Helper()
	var out []declaredFlag
	lit := func(e ast.Expr) (string, bool) {
		b, ok := e.(*ast.BasicLit)
		if !ok || b.Kind != gotoken.STRING {
			return "", false
		}
		s, err := strconv.Unquote(b.Value)
		return s, err == nil
	}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			i, ok := flagMethods[sel.Sel.Name]
			if !ok || len(call.Args) < i+2 {
				return true
			}
			name, ok := lit(call.Args[i])
			if !ok {
				return true
			}
			usage, _ := lit(call.Args[len(call.Args)-1])
			out = append(out, declaredFlag{fset.Position(call.Pos()).String(), name, usage})
			return true
		})
	}
	return out
}

// secretFlag is the refusal for a flag whose name says it carries a secret (it ends in
// password, secret or token) and whose usage does not say it takes a NAME or a path; ""
// for any other flag.
func secretFlag(name, usage string) string {
	n := strings.ToLower(name)
	if !strings.HasSuffix(n, "password") && !strings.HasSuffix(n, "secret") && !strings.HasSuffix(n, "token") {
		return ""
	}
	if strings.Contains(usage, "NAME") || strings.Contains(strings.ToLower(usage), "path") {
		return ""
	}
	return "--" + name + " (" + usage + ")"
}

// No flag of nova-sprint takes a secret value, so none can put one in argv, where ps
// reads it: every flag the package's source declares on a FlagSet, every verb's and
// every sub-verb's, is judged. Reversed witnesses: a source with a --redis-password
// flag is named, and the walk sees seat login's --secret (which takes a NAME).
func TestNoFlagCarriesASecretValue(t *testing.T) {
	t.Parallel()
	fset := gotoken.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	require.NoError(t, err)
	require.Contains(t, pkgs, "main")
	flags := declaredFlags(t, fset, pkgs["main"].Files)
	var bad []string
	sawSecret := false
	for _, f := range flags {
		if f.name == "secret" && strings.HasPrefix(filepath.Base(f.at), "storelogin.go:") {
			sawSecret = true
		}
		if s := secretFlag(f.name, f.usage); s != "" {
			bad = append(bad, f.at+" "+s)
		}
	}
	assert.Empty(t, bad, "a flag that carries a secret value: take the NAME of a variable or a file's path instead")
	assert.True(t, sawSecret, "the walk reads seat login's flags")
	assert.Greater(t, len(flags), len(verbs), "the walk reads the verbs' flags")

	// the judge's witness: a source with one secret-valued flag among two that are not
	src := `package main
func f(fs *flag.FlagSet, s *string) {
	fs.String("redis-password", "", "the store's password")
	fs.StringVar(s, "api-token", "", "the NAME of the variable holding the token")
	fs.String("key-secret", "", "the path of the file holding it")
	fs.String("built-token", "", "built " + "usage")
}`
	wf := gotoken.NewFileSet()
	file, err := parser.ParseFile(wf, "witness.go", src, 0)
	require.NoError(t, err)
	var named []string
	for _, f := range declaredFlags(t, wf, map[string]*ast.File{"witness.go": file}) {
		if s := secretFlag(f.name, f.usage); s != "" {
			named = append(named, s)
		}
	}
	assert.Equal(t, []string{"--redis-password (the store's password)", "--built-token ()"}, named)
}
