package verbs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const installLimit = 128 << 20

// ReleaseInstaller downloads and activates official release artifacts. Optional
// fields permit isolated local fixtures; zero values use the real release.
type ReleaseInstaller struct {
	Home     string
	BaseURL  string
	Client   *http.Client
	Check    func(context.Context, string) error
	Version  func(context.Context, string) (string, error)
	OS, Arch string
}

func installError(problem, fix string) error {
	return &OperationError{Problem: problem, Fix: fix, Exit: 1}
}

func (r ReleaseInstaller) Install(ctx context.Context, version, dir string) (string, error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return "", installError("install is unsupported on this operating system", "install a release manually on Linux or macOS")
	}
	if v, _, ok := parseInstall([]string{"--version", version}); !ok || v != version {
		return "", installError("invalid release version", "use vMAJOR.MINOR.PATCH")
	}
	home := r.Home
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", installError("home directory unavailable", "set HOME and retry")
		}
	}
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		return "", installError("cannot create command directory", "check ~/.local/bin permissions")
	}
	activationLock := filepath.Join(bin, ".nova-sprint-install-lock")
	activationFile, err := installLock(activationLock)
	if err != nil {
		return "", installError("cannot acquire installation lock", "wait for another install to finish or check ~/.local/bin lock permissions")
	}
	defer activationFile.Close()
	if dir == "" {
		dir = filepath.Join(home, ".local", "nova-sprint")
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", installError("invalid installation directory", "choose a valid --dir")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", installError("cannot create installation directory", "check --dir permissions")
	}
	lock := filepath.Join(dir, ".install-lock")
	lockFile, err := installLock(lock)
	if err != nil {
		return "", installError("cannot acquire installation lock", "wait for another install to finish or check --dir lock permissions")
	}
	defer lockFile.Close()
	return r.installLocked(ctx, version, dir, home)
}

func (r ReleaseInstaller) installLocked(ctx context.Context, version, dir, home string) (string, error) {
	goos, arch := r.OS, r.Arch
	if goos == "" {
		goos = runtime.GOOS
	}
	if arch == "" {
		arch = runtime.GOARCH
	}
	asset := fmt.Sprintf("nova-sprint_%s_%s_%s.tar.gz", version, goos, arch)
	base := strings.TrimRight(r.BaseURL, "/")
	if base == "" {
		base = "https://github.com/mas-bandwidth/nova-sprint/releases/download"
	}
	sums, err := r.download(ctx, base+"/"+version+"/SHA256SUMS", 1<<20)
	if err != nil {
		return "", installError("cannot download release checksums", "check the release and network, then retry")
	}
	want, err := installChecksum(sums, asset)
	if err != nil {
		return "", installError("release checksum is missing or ambiguous", "check SHA256SUMS for this platform")
	}
	archive, err := r.download(ctx, base+"/"+version+"/"+asset, installLimit)
	if err != nil {
		return "", installError("cannot download release asset", "check the release and network, then retry")
	}
	hash := sha256.Sum256(archive)
	if !bytes.Equal(hash[:], want) {
		return "", installError("release checksum mismatch", "check the release asset and SHA256SUMS before retrying")
	}
	binary, err := installExtract(archive)
	if err != nil {
		return "", installError("release archive is invalid", "check the release archive and retry")
	}
	target, err := installStage(dir, version, binary)
	if err != nil {
		return "", err
	}
	if err = r.verifyCandidate(ctx, target, version); err != nil {
		return "", err
	}
	previous := installPrevious(home)
	if ctx.Err() != nil {
		return "", installError("installation cancelled before activation", "retry installation")
	}
	if err = installSwitch(home, target); err != nil {
		return "", err
	}
	return previous, nil
}

func (r ReleaseInstaller) verifyCandidate(ctx context.Context, target, version string) error {
	check := r.Check
	if check == nil {
		check = func(ctx context.Context, path string) error { return exec.CommandContext(ctx, path, "check").Run() }
	}
	if err := check(ctx, target); err != nil {
		return installError("staged binary check failed", "repair the check and retry; the staged version remains for inspection")
	}
	readVersion := r.Version
	if readVersion == nil {
		readVersion = func(ctx context.Context, path string) (string, error) {
			out, err := exec.CommandContext(ctx, path, "version").Output()
			return string(out), err
		}
	}
	line, err := readVersion(ctx, target)
	if err != nil || line != version+"\n" {
		return installError("staged binary version mismatch", "inspect the staged version and release before retrying")
	}
	return nil
}

func (r ReleaseInstaller) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("download too large")
	}
	return data, nil
}

func installChecksum(sums []byte, asset string) ([]byte, error) {
	var found []byte
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != asset {
			continue
		}
		if found != nil || len(fields[0]) != 64 {
			return nil, errors.New("ambiguous checksum")
		}
		sum, err := hex.DecodeString(fields[0])
		if err != nil {
			return nil, err
		}
		found = sum
	}
	if found == nil {
		return nil, errors.New("missing checksum")
	}
	return found, nil
}

func installExtract(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	header, err := tr.Next()
	if err != nil {
		return nil, err
	}
	if header.Name != "nova-sprint" || header.Typeflag != tar.TypeReg || header.Size < 1 || header.Size > installLimit {
		return nil, errors.New("invalid archive entry")
	}
	binary, err := io.ReadAll(io.LimitReader(tr, installLimit+1))
	if err != nil || int64(len(binary)) != header.Size {
		return nil, errors.New("invalid binary")
	}
	if _, err = tr.Next(); err != io.EOF {
		return nil, errors.New("extra archive entry")
	}
	padding, err := io.ReadAll(io.LimitReader(gz, (1<<20)+1))
	if err != nil || len(padding) > 1<<20 || len(bytes.Trim(padding, "\x00")) != 0 {
		return nil, errors.New("invalid archive padding")
	}
	return binary, nil
}
