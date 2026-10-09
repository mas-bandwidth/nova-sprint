package sprint

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/mas-bandwidth/nova-sprint/internal/buildinfo"
)

// The nova-tools nova-sprint runs on (docs/SPEC-SPRINT.md, "The nova-tools it runs
// on"; the owner, 2026-10-06 08:50: "ensure that anybody installing nova-sprint does so
// on top of the latest nova-tools"). NOVA-TOOLS-VERSION at the repository root names the
// nova-tools release the tree was seeded from (tools/seed writes it); NovaToolsVersion
// is the same tag compiled in, kept equal to the file by
// TestNovaToolsVersionIsTheFilesTag, since a binary runs far from its checkout. The
// check is pure: what each binary's --version printed in, one refusal line per binary
// out; the lookup is the caller's.

// NovaToolsVersion is the nova-tools release nova-sprint needs at least: the tag line
// of NOVA-TOOLS-VERSION.
const NovaToolsVersion = "v1.2.1"

// NovaToolsVersionFile is the file at the repository root that records it.
const NovaToolsVersionFile = "NOVA-TOOLS-VERSION"

// NovaToolsModule is the module the nova-tools binaries install from.
const NovaToolsModule = "github.com/mas-bandwidth/nova-tools"

// NovaToolsBinaries are the nova-tools commands nova-sprint calls: the friends, the
// bus and the config store, in the order they are checked.
var NovaToolsBinaries = []string{"nova-friend", "nova-bus", "nova-config"}

// NovaToolsInstall is the command that installs bin at the release tag, never
// @latest: the tag is the one NOVA-TOOLS-VERSION names.
func NovaToolsInstall(bin, tag string) string {
	return "go install " + NovaToolsModule + "/cmd/" + bin + "@" + tag
}

// NovaToolsInstallNote is the install command as a refusal prints it, saying that it
// works once the release is published (a tree is seeded ahead of the cut).
func NovaToolsInstallNote(bin, tag string) string {
	return "install (once nova-tools " + tag + " is published): " + NovaToolsInstall(bin, tag)
}

// NovaToolsProbe is one binary as the lookup found it: on PATH or not, and what its
// --version printed (Err when it ran and failed).
type NovaToolsProbe struct {
	Bin    string
	Found  bool
	Output string
	Err    string
}

// NovaToolsRefusal is the one line that refuses p against the required tag, "" when
// p passes: found, a version line naming a release at or past required. A build that
// names no release (devel, a vcs stamp) is refused, since nothing says it is not older.
func NovaToolsRefusal(p NovaToolsProbe, required string) string {
	install := NovaToolsInstallNote(p.Bin, required)
	if !p.Found {
		return fmt.Sprintf("%s not found on PATH; nova-tools %s required (%s); %s", p.Bin, required, NovaToolsVersionFile, install)
	}
	found := "no version line"
	if f, ok := buildinfo.Parse(p.Output); ok {
		found = f.Version
		if semver.IsValid(found) && semver.Compare(found, required) >= 0 {
			return ""
		}
	} else if p.Err != "" {
		found = "no version line (" + p.Err + ")"
	}
	return fmt.Sprintf("%s %s found, older than nova-tools %s required (%s); %s", p.Bin, found, required, NovaToolsVersionFile, install)
}

// NovaToolsRefusals is every refusal of probes against required, in probe order;
// none is the install passing.
func NovaToolsRefusals(probes []NovaToolsProbe, required string) []string {
	var out []string
	for _, p := range probes {
		if r := NovaToolsRefusal(p, required); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// NovaToolsRecord is NOVA-TOOLS-VERSION read: the release tag and the commit it
// names ("" when the file records none).
type NovaToolsRecord struct {
	Tag    string
	Commit string
}

// FormatNovaToolsVersion is the file tools/seed writes for tag at commit.
func FormatNovaToolsVersion(tag, commit string) string {
	return "# The nova-tools release this tree was seeded from, and the least nova-sprint runs on.\n" +
		"# Written by tools/seed; read by nova-sprint seat check (docs/SPEC-SPRINT.md).\n" +
		"tag " + tag + "\n" +
		"commit " + commit + "\n"
}

// ParseNovaToolsVersion reads NOVA-TOOLS-VERSION: lines `tag <vX.Y.Z>` and `commit
// <40 hex>`, blank lines and # comments. The tag is required and is a release; the
// commit, when present, is a full hash. Canonical prerelease tags are accepted
// for private canaries; build metadata is not, since SemVer does not order it.
func ParseNovaToolsVersion(data string) (NovaToolsRecord, error) {
	var r NovaToolsRecord
	for i, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		val = strings.TrimSpace(val)
		switch key {
		case "tag":
			if !semver.IsValid(val) || semver.Build(val) != "" || semver.Canonical(val) != val {
				return r, fmt.Errorf("%s line %d: tag %q is no release tag vX.Y.Z", NovaToolsVersionFile, i+1, val)
			}
			r.Tag = val
		case "commit":
			if !fullHash(val) {
				return r, fmt.Errorf("%s line %d: commit %q is no full 40-hex hash", NovaToolsVersionFile, i+1, val)
			}
			r.Commit = val
		default:
			return r, fmt.Errorf("%s line %d: %q is neither tag nor commit", NovaToolsVersionFile, i+1, line)
		}
	}
	if r.Tag == "" {
		return r, fmt.Errorf("%s names no tag", NovaToolsVersionFile)
	}
	return r, nil
}

func fullHash(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
