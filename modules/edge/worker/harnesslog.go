package worker

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The harness's own words on why it ended. On 2026-11-10, 181 work
// attempts on one model ended "no verdict: the harness exited 1" with only
// the harness's last output (a tool's) in the reason, and the clean step
// then removed the job directory with the harness's own log in it, so no
// copy of the cause was left anywhere. Now a not-ok reason carries the
// log's last error lines (harvestStep_internal), and a job that did not end
// ok keeps its logs after the clean (keepJobLogs_internal), bounded.

const (
	// KeptLogsDir holds, outside every job directory, the logs of the jobs
	// that did not end ok: <root>/kept-logs/<consumer>@<gen>.
	KeptLogsDir = "kept-logs"
	// KeptLogBytes is the most kept of one log: its end.
	KeptLogBytes = 256 << 10
	// KeptLogJobs is how many jobs' logs are kept, the newest. With
	// KeptLogBytes it bounds the directory: four files and the harness's
	// log directory (one file for opencode), at most about 40 MB.
	KeptLogJobs = 32
	// harnessLogTail is how much of the harness's log is read for its
	// error lines: its end.
	harnessLogTail = 256 << 10
	// harnessLogLines and harnessLogLineBytes bound the error lines a
	// reason carries.
	harnessLogLines, harnessLogLineBytes = 2, 400
)

// keptJobFiles are the job directory's own files a not-ok job keeps:
// what keepLogs_internal wrote of the harness's run, and the job record.
var keptJobFiles = []string{"stdout.log", "stderr.log", "command.txt", "job.json"}

// readTail_internal is the last n bytes of name under root, a regular file
// only: the job directory is the harness's to write, and a link it planted
// is never followed (os.Root refuses one out of the job; Lstat refuses any).
func readTail_internal(root *os.Root, name string, n int64) ([]byte, error) {
	st, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fst, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fst.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	if st.Size() > n {
		if _, err := f.Seek(st.Size()-n, io.SeekStart); err != nil {
			return nil, err
		}
	}
	return io.ReadAll(io.LimitReader(f, n))
}

// harnessLogFiles_internal are the regular files of the harness's log
// directory (logDir, under jobDir; Needs.LogDir), by name.
func harnessLogFiles_internal(root *os.Root, logDir string) []string {
	if logDir == "" {
		return nil
	}
	d, err := root.Open(logDir)
	if err != nil {
		return nil
	}
	defer d.Close()
	entries, err := d.ReadDir(-1)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			names = append(names, filepath.Join(logDir, e.Name()))
		}
	}
	slices.Sort(names)
	return names
}

// isErrorLine_internal: a log line at the error level, in the forms a
// harness's logger writes it (level=ERROR, or ERROR first).
func isErrorLine_internal(l string) bool {
	return strings.Contains(l, "level=ERROR") || strings.Contains(l, `"level":"error"`) || strings.HasPrefix(l, "ERROR")
}

// harnessLogErrors_internal is the last error lines of the harness's own
// log directory (logDir, under jobDir), each cut short, joined with " | ";
// "" when it has none or no log directory.
func harnessLogErrors_internal(jobDir, logDir string) string {
	if jobDir == "" || logDir == "" {
		return ""
	}
	root, err := os.OpenRoot(jobDir)
	if err != nil {
		return ""
	}
	defer root.Close()
	var lines []string
	for _, name := range harnessLogFiles_internal(root, logDir) {
		b, err := readTail_internal(root, name, harnessLogTail)
		if err != nil {
			continue
		}
		for l := range strings.SplitSeq(string(b), "\n") {
			if l = strings.TrimSpace(l); isErrorLine_internal(l) {
				lines = append(lines, l)
			}
		}
	}
	if len(lines) > harnessLogLines {
		lines = lines[len(lines)-harnessLogLines:]
	}
	for i, l := range lines {
		if len(l) > harnessLogLineBytes {
			lines[i] = strings.ToValidUTF8(l[:harnessLogLineBytes], "") + "..."
		}
	}
	return strings.Join(lines, " | ")
}

// withHarnessLog_internal is reason with the harness's log lines put
// before its last output (noVerdict_internal's form), or after it all; the
// reason's head is kept whole and the whole cut to MaxReasonBytes.
func withHarnessLog_internal(reason, lines string) string {
	if lines == "" {
		return reason
	}
	const last = "; the harness's last output: "
	add := "; the harness's log: " + lines
	if i := strings.Index(reason, last); i >= 0 {
		reason = reason[:i] + add + reason[i:]
	} else {
		reason += add
	}
	if len(reason) > MaxReasonBytes {
		reason = strings.ToValidUTF8(reason[:MaxReasonBytes], "")
	}
	return reason
}

// keepJobLogs_internal copies the logs of a job that did not end ok out of
// its directory before the clean removes it: keptJobFiles and the
// harness's log directory, each its last KeptLogBytes, to
// <root>/kept-logs/<consumer>@<gen>; then only the newest KeptLogJobs
// jobs' logs are kept. Nothing here stops the clean: a copy that fails is
// said.
func (r *Runner) keepJobLogs_internal(jobDir string, rec JobRecord) {
	root, err := os.OpenRoot(jobDir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			r.report("keeping the logs of "+rec.Consumer, err)
		}
		return
	}
	defer root.Close()
	names := slices.Clone(keptJobFiles)
	if h, err := r.harnessFor_internal(rec.Harness); err == nil && rec.Harness != "" {
		names = append(names, harnessLogFiles_internal(root, h.Needs().LogDir)...)
	}
	keep := filepath.Join(r.RootDir, KeptLogsDir, fmt.Sprintf("%s@%d", rec.Consumer, rec.Gen))
	var errs []error
	for _, name := range names {
		b, err := readTail_internal(root, name, KeptLogBytes)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err == nil {
			err = os.MkdirAll(filepath.Join(keep, filepath.Dir(name)), 0o700)
		}
		if err == nil {
			err = os.WriteFile(filepath.Join(keep, name), b, 0o600)
		}
		errs = append(errs, err)
	}
	r.report("keeping the logs of "+rec.Consumer+" in "+keep, errors.Join(errs...))
	r.report("pruning "+KeptLogsDir, pruneKeptLogs_internal(filepath.Join(r.RootDir, KeptLogsDir), KeptLogJobs))
}

// pruneKeptLogs_internal removes all but the newest n job directories of
// dir, by modification time.
func pruneKeptLogs_internal(dir string, n int) error {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= n {
		return err
	}
	type kept struct {
		name string
		at   int64
	}
	var ks []kept
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		ks = append(ks, kept{e.Name(), info.ModTime().UnixNano()})
	}
	slices.SortFunc(ks, func(a, b kept) int {
		if a.at != b.at {
			return int(min(max(b.at-a.at, -1), 1))
		}
		return strings.Compare(b.name, a.name)
	})
	var errs []error
	for _, k := range ks[min(n, len(ks)):] {
		errs = append(errs, removeTree_internal(filepath.Join(dir, k.name)))
	}
	return errors.Join(errs...)
}
