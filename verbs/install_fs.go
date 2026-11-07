package verbs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func installStage(dir, version string, binary []byte) (string, error) {
	final := filepath.Join(dir, version)
	target := filepath.Join(final, "nova-sprint")
	if _, err := os.Lstat(final); err == nil {
		return installExisting(final, target, binary)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", installError("cannot inspect version directory", "check --dir permissions")
	}
	stage, err := os.MkdirTemp(dir, ".install-stage-")
	if err != nil {
		return "", installError("cannot stage release", "check --dir permissions")
	}
	defer os.RemoveAll(stage)
	if err = os.WriteFile(filepath.Join(stage, "nova-sprint"), binary, 0755); err != nil {
		return "", installError("cannot stage release", "check --dir permissions")
	}
	if err = os.Rename(stage, final); err != nil {
		return "", installError("cannot publish version directory", "check --dir and retry")
	}
	return target, nil
}

func installExisting(dir, target string, binary []byte) (string, error) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return "", installError("version directory already exists with different contents", "choose another --dir or inspect the existing directory")
	}
	info, err = os.Lstat(target)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", installError("existing version binary is invalid", "choose another --dir or inspect the existing directory")
	}
	data, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(data, binary) {
		return "", installError("existing version binary differs from the release", "choose another --dir or inspect the existing directory")
	}
	return target, nil
}

func installSwitch(home, target string) error {
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		return installError("cannot create command directory", "check ~/.local/bin permissions")
	}
	link := filepath.Join(bin, "nova-sprint")
	info, err := os.Lstat(link)
	if err == nil && info.Mode()&os.ModeSymlink == 0 {
		return installError("command destination is not a symlink", "move the existing ~/.local/bin/nova-sprint aside and retry")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return installError("cannot inspect command destination", "check ~/.local/bin permissions")
	}
	tmp, err := os.MkdirTemp(bin, ".nova-sprint-link-")
	if err != nil {
		return installError("cannot stage command symlink", "check ~/.local/bin permissions")
	}
	defer os.RemoveAll(tmp)
	staged := filepath.Join(tmp, "nova-sprint")
	if err = os.Symlink(target, staged); err != nil {
		return installError("cannot stage command symlink", "check ~/.local/bin permissions")
	}
	if err = os.Rename(staged, link); err != nil {
		return installError("cannot switch command symlink", "check ~/.local/bin permissions")
	}
	return nil
}

func installPrevious(home string) string {
	link := filepath.Join(home, ".local", "bin", "nova-sprint")
	target, err := os.Readlink(link)
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	if filepath.Base(target) != "nova-sprint" {
		return ""
	}
	version := filepath.Base(filepath.Dir(target))
	if _, _, ok := parseInstall([]string{"--version", version}); !ok {
		return ""
	}
	dir := filepath.Dir(filepath.Dir(target))
	return "nova-sprint install --version " + version + " --dir '" + strings.ReplaceAll(dir, "'", "'\\''") + "'"
}
