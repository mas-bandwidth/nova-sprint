package verbs

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func installFixture() []byte { return []byte("fixture binary") }

type installFixtureTransport struct {
	binary []byte
	sums   string
}

func (f installFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body string
	code := http.StatusOK
	switch req.URL.Path {
	case "/v1.2.3/SHA256SUMS":
		body = f.sums
	case "/v1.2.3/nova-sprint_v1.2.3_linux_amd64":
		body = string(f.binary)
	default:
		code = http.StatusNotFound
	}
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
}
func installTestRelease(t *testing.T, binary []byte, sums string) (ReleaseInstaller, string) {
	t.Helper()
	home := t.TempDir()
	return ReleaseInstaller{Home: home, BaseURL: "https://fixture.invalid", OS: "linux", Arch: "amd64", Client: &http.Client{Transport: installFixtureTransport{binary: binary, sums: sums}}, Check: func(context.Context, string) error { return nil }, Version: func(context.Context, string) (string, error) { return "v1.2.3\n", nil }}, home
}
func installSums(binary []byte) string {
	return fmt.Sprintf("%x  nova-sprint_v1.2.3_linux_amd64\n", sha256.Sum256(binary))
}

func TestInstallReleaseAndExistingRollback(t *testing.T) {
	binary := installFixture()
	r, home := installTestRelease(t, binary, installSums(binary))
	dir := filepath.Join(home, "versions")
	ctx := context.Background()
	if _, err := r.Install(ctx, "v1.2.3", dir); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "v1.2.3", "nova-sprint")
	link := filepath.Join(home, ".local", "bin", "nova-sprint")
	if got, err := os.Readlink(link); err != nil || got != target {
		t.Fatalf("link %q: %v", got, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "fixture binary" {
		t.Fatalf("binary %q: %v", got, err)
	}
	if _, err := r.Install(ctx, "v1.2.3", dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("unrelated"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Install(ctx, "v1.2.3", dir); err == nil {
		t.Fatal("accepted different existing binary")
	}
}

func TestInstallRefusesBadReleaseAndRegularDestination(t *testing.T) {
	binary := installFixture()
	for _, tc := range []struct {
		name   string
		binary []byte
		sums   string
	}{
		{"checksum", binary, strings.Repeat("0", 64) + "  nova-sprint_v1.2.3_linux_amd64\n"},
		{"duplicate", binary, installSums(binary) + installSums(binary)},
		{"empty binary", []byte{}, ""},
		{"tarball-only checksum", binary, strings.ReplaceAll(installSums(binary), "linux_amd64", "linux_amd64.tar.gz")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.sums == "" {
				tc.sums = installSums(tc.binary)
			}
			r, home := installTestRelease(t, tc.binary, tc.sums)
			if _, err := r.Install(context.Background(), "v1.2.3", ""); err == nil {
				t.Fatal("accepted invalid release")
			}
			if _, err := os.Lstat(filepath.Join(home, ".local", "bin", "nova-sprint")); !os.IsNotExist(err) {
				t.Fatalf("unexpected destination: %v", err)
			}
		})
	}
	r, home := installTestRelease(t, binary, installSums(binary))
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(bin, "nova-sprint")
	if err := os.WriteFile(link, []byte("keep"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Install(context.Background(), "v1.2.3", ""); err == nil {
		t.Fatal("overwrote regular destination")
	}
	if got, _ := os.ReadFile(link); string(got) != "keep" {
		t.Fatal("destination changed")
	}
}

func TestInstallFailedCheckAndCancellation(t *testing.T) {
	binary := installFixture()
	r, _ := installTestRelease(t, binary, installSums(binary))
	r.Check = func(context.Context, string) error { return errors.New("bad") }
	_, err := r.Install(context.Background(), "v1.2.3", "")
	var op *OperationError
	if !errors.As(err, &op) || !strings.Contains(op.Problem, "check failed") {
		t.Fatalf("check failure: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Install(ctx, "v1.2.3", ""); err == nil {
		t.Fatal("cancel ignored")
	}
}

func TestParseInstall(t *testing.T) {
	for _, args := range [][]string{{}, {"--version", "1.2.3"}, {"--version", "v1.2.3-dev"}, {"--version", "v+1.2.3"}, {"--version", "v1.2.3", "--version", "v1.2.3"}, {"--version", "v1.2.3", "--dir"}} {
		if _, _, ok := parseInstall(args); ok {
			t.Fatalf("accepted %v", args)
		}
	}
	if v, d, ok := parseInstall([]string{"--dir", "/tmp/a", "--version", "v1.2.3"}); !ok || v != "v1.2.3" || d != "/tmp/a" {
		t.Fatal(v, d, ok)
	}
}

func TestInstallPreservesPreviousLinkOnCandidateFailure(t *testing.T) {
	binary := installFixture()
	for _, mismatch := range []bool{false, true} {
		t.Run(fmt.Sprint(mismatch), func(t *testing.T) {
			r, home := installTestRelease(t, binary, installSums(binary))
			bin := filepath.Join(home, ".local", "bin")
			if err := os.MkdirAll(bin, 0755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(bin, "nova-sprint")
			old := filepath.Join(home, "old", "v1.0.0", "nova-sprint")
			if err := os.Symlink(old, link); err != nil {
				t.Fatal(err)
			}
			if mismatch {
				r.Version = func(context.Context, string) (string, error) { return "v9.0.0\n", nil }
			} else {
				r.Check = func(context.Context, string) error { return errors.New("bad check") }
			}
			if _, err := r.Install(context.Background(), "v1.2.3", ""); err == nil {
				t.Fatal("accepted failed candidate")
			}
			if got, err := os.Readlink(link); err != nil || got != old {
				t.Fatalf("previous link changed: %q %v", got, err)
			}
			if _, err := os.Stat(filepath.Join(home, ".local", "nova-sprint", "v1.2.3", "nova-sprint")); err != nil {
				t.Fatal("staged version missing", err)
			}
		})
	}
}

func TestInstallActivationLockAndRollbackCommand(t *testing.T) {
	binary := installFixture()
	r, home := installTestRelease(t, binary, installSums(binary))
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(bin, ".nova-sprint-install-lock")
	lockFile, err := installLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Install(context.Background(), "v1.2.3", ""); err == nil {
		t.Fatal("concurrent install accepted")
	}
	if err := lockFile.Close(); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(home, "old path", "v1.0.0", "nova-sprint")
	if err := os.Symlink(old, filepath.Join(bin, "nova-sprint")); err != nil {
		t.Fatal(err)
	}
	rollback, err := r.Install(context.Background(), "v1.2.3", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rollback, "--version v1.0.0 --dir '") || !strings.Contains(rollback, "old path") {
		t.Fatal(rollback)
	}
}

func TestInstallCancellationBeforeActivation(t *testing.T) {
	binary := installFixture()
	r, home := installTestRelease(t, binary, installSums(binary))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Version = func(context.Context, string) (string, error) { cancel(); return "v1.2.3\n", nil }
	if _, err := r.Install(ctx, "v1.2.3", ""); err == nil {
		t.Fatal("cancel ignored")
	}
	if _, err := os.Lstat(filepath.Join(home, ".local", "bin", "nova-sprint")); !os.IsNotExist(err) {
		t.Fatalf("activated after cancellation: %v", err)
	}
}
