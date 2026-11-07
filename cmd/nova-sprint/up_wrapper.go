package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

const handoffOutputLimit = 64 << 10

var continuationID = regexp.MustCompile(`^[0-9a-f]{64}$`)

type handoffStatus struct {
	Status string `json:"status"`
	ID     string `json:"id"`
}
type limitedOutput struct {
	buf      bytes.Buffer
	overflow bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	left := handoffOutputLimit - b.buf.Len()
	if left < len(p) {
		b.overflow = true
		if left < 0 {
			left = 0
		}
		p = p[:left]
	}
	b.buf.Write(p) // bytes.Buffer's Write never returns an error (it panics when out of memory)
	return n, nil
}

// runUpWrapper never receives either database credential. Only the two
// nova-secrets children receive their own named credential.
func runUpWrapper(ctx context.Context, args []string) int {
	input, ok := verbs.ParseSetup(args, os.Getenv("NOVA_SPRINT_AS"))
	if !ok {
		r := (&verbs.Engine{Actor: os.Getenv("NOVA_SPRINT_AS")}).Execute(ctx, append([]string{"up"}, args...))
		return emit(r.Output, r.Exit)
	}
	if input.Continuation != "" {
		return emit("SPRINT REFUSED up: --continuation is the second phase's own flag, never given by hand: nova-sprint help up\n", 1)
	}
	if os.Getenv("NOVA_PG_ADMIN_PASSWORD") != "" || os.Getenv("NOVA_SPRINT_PG_PASSWORD") != "" {
		fmt.Fprintln(os.Stderr, "SPRINT REFUSED up: invoke the setup wrapper without database credentials (NOVA_PG_ADMIN_PASSWORD and NOVA_SPRINT_PG_PASSWORD must be unset; each phase gets its own from nova-secrets)")
		return 1
	}
	cfg, err := productionSetupConfigFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "SPRINT REFUSED up: setup configuration unavailable: "+edge.RedactN(err.Error(), edge.CommandMax))
		return 1
	}
	if err = verbs.UpNeedRefusal(toolPin, cfg.upEnv()); err != nil {
		fmt.Fprintln(os.Stderr, "SPRINT REFUSED up: "+err.Error())
		return 1
	}
	p := productionSetup{cfg: cfg}
	if err = p.configError(); err != nil {
		fmt.Fprintln(os.Stderr, "SPRINT REFUSED up: "+edge.RedactN(err.Error(), edge.CommandMax))
		return 1
	}
	if _, err = setupRoleDSN(cfg.DSN, "nova_admin"); err != nil {
		fmt.Fprintln(os.Stderr, "SPRINT REFUSED up: configure a passwordless NOVA_PG_DSN ("+edge.RedactN(err.Error(), edge.CommandMax)+")")
		return 1
	}
	status, output, code := runUpChild(ctx, cfg, "NOVA_PG_ADMIN_PASSWORD", "--internal-up-phase1", args, true)
	if code != 0 {
		return emit(output, code)
	}
	if status == "" {
		if output == "" {
			fmt.Fprintln(os.Stderr, "SPRINT REFUSED up: the first phase exited 0 with no output and no continuation (a bug: take it to the project's own tracker)")
			return 2
		}
		return emit(output, 0)
	}
	if !continuationID.MatchString(status) {
		fmt.Fprintf(os.Stderr, "SPRINT REFUSED up: invalid continuation status %q (want 64 hex digits)\n", edge.Redact(status))
		return 2
	}
	next := append(append([]string{}, args...), "--continuation", status)
	_, output, code = runUpChild(ctx, cfg, "NOVA_SPRINT_PG_PASSWORD", "--internal-up-phase2", next, false)
	return emit(output, code)
}

// emit writes a phase's output on stdout and returns code; when stdout
// cannot take it, the output and why go to stderr and the exit is 2.
func emit(output string, code int) int {
	if _, err := fmt.Fprint(os.Stdout, output); err != nil {
		fmt.Fprintf(os.Stderr, "SPRINT REFUSED up: cannot write to stdout (%s); the output was: %s", edge.RedactN(err.Error(), edge.CommandMax), output)
		return 2
	}
	return code
}
func runUpChild(ctx context.Context, cfg productionSetupConfig, key, mode string, args []string, channel bool) (string, string, int) {
	// nova-secrets exec drops NOVA_SECRETS_* from its child; the phases read
	// them (productionSetupConfigFromEnv), so env puts the paths back.
	s := cfg.Secrets
	flags := []string{"exec", "--only", key, "--store", s.Store, "--key", s.KeyFile, "--as", s.Seat, "--", "/usr/bin/env",
		"NOVA_SECRETS_STORE=" + s.Store, "NOVA_SECRETS_KEY=" + s.KeyFile, "NOVA_SECRETS_PINS=" + s.Pins, "NOVA_SECRETS_SEAT=" + s.Seat, cfg.Binary, mode}
	cmd := exec.CommandContext(ctx, "nova-secrets", append(flags, args...)...)
	cmd.Env = setupWrapperEnv(os.Environ())
	if key == "NOVA_SPRINT_PG_PASSWORD" {
		cmd.Env = append(cmd.Env, "NOVA_PG_PASSWORD_ENV=NOVA_SPRINT_PG_PASSWORD")
	}
	cmd.Stdin = os.Stdin
	out, errOut := &limitedOutput{}, &limitedOutput{}
	cmd.Stdout = out
	cmd.Stderr = errOut
	var reader, writer *os.File
	var err error
	if channel {
		reader, writer, err = os.Pipe()
		if err != nil {
			return "", "SPRINT REFUSED up: cannot open continuation channel: " + edge.RedactN(err.Error(), edge.CommandMax) + "\n", 2
		}
		defer reader.Close()
		cmd.ExtraFiles = []*os.File{writer}
		cmd.Env = append(cmd.Env, "NOVA_SPRINT_HANDOFF_FD=3")
	}
	if err = cmd.Start(); err != nil {
		if writer != nil {
			_ = writer.Close()
		}
		return "", "SPRINT REFUSED up: cannot start secrets child: " + edge.Failure(cmd.Args, cmd.Env, -1, "", "", err).Error() + "\n", 2
	}
	var result chan handoffRead
	if channel {
		result = startHandoffReader(reader, writer)
	}
	err = cmd.Wait()
	output := out.buf.String() + errOut.buf.String()
	if out.overflow || errOut.overflow {
		return "", "SPRINT REFUSED up: child output exceeded limit: " + edge.CommandLine(cmd.Args, cmd.Env) + "\n", 2
	}
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok && e.ExitCode() >= 0 {
			return "", output, e.ExitCode()
		}
		return "", "SPRINT REFUSED up: secrets child stopped: " + edge.Failure(cmd.Args, cmd.Env, -1, out.buf.String(), errOut.buf.String(), err).Error() + "\n", 2
	}
	if !channel {
		return "", output, 0
	}
	status, err := waitHandoff(ctx, result)
	if err != nil {
		return "", "SPRINT REFUSED up: invalid continuation channel: " + edge.RedactN(err.Error(), edge.CommandMax) + "\n", 2
	}
	return status, output, 0
}
func startHandoffReader(reader, writer *os.File) chan handoffRead {
	_ = writer.Close()
	result := make(chan handoffRead, 1)
	go func() {
		defer reader.Close()
		id, err := readHandoffStatus(reader)
		result <- handoffRead{id: id, err: err}
	}()
	return result
}
func readHandoffStatus(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, 257))
	if err != nil {
		return "", edge.Cause("invalid status: read", err)
	}
	if len(data) > 256 {
		return "", fmt.Errorf("invalid status: %s", edge.ParseRefused("longer than 256 bytes", 256, data))
	}
	if len(data) == 0 {
		return "", nil
	}
	var s handoffStatus
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return "", fmt.Errorf("invalid status: %s", edge.ParseFailure(err, data))
	}
	if s.Status != "continue" || !continuationID.MatchString(s.ID) {
		return "", fmt.Errorf("invalid status: %s", edge.ParseRefused("status is not continue with a continuation id", 0, data))
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return "", fmt.Errorf("duplicate status: %s", edge.ParseRefused("more than one status", int(dec.InputOffset()), data))
	}
	return s.ID, nil
}
func setupWrapperEnv(source []string) []string {
	names := map[string]bool{"PATH": true, "HOME": true, "SSH_AUTH_SOCK": true, "LANG": true, "LC_ALL": true, "TZ": true, "TMPDIR": true, "SSL_CERT_DIR": true, "SSL_CERT_FILE": true, "NOVA_PG_DSN": true, "NOVA_BUS_REDIS": true, "NOVA_BUS_USER": true, "NOVA_SPRINT_BACKUP_DIR": true, "NOVA_SECRETS_STORE": true, "NOVA_SECRETS_KEY": true, "NOVA_SECRETS_PINS": true, "NOVA_SECRETS_SEAT": true, "NOVA_SPRINT_AS": true}
	out := []string{}
	for _, entry := range source {
		name, _, ok := strings.Cut(entry, "=")
		if ok && names[name] {
			out = append(out, entry)
		}
	}
	return out
}
func reportHandoff(ctx context.Context, id string, _ verbs.SetupInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fd, err := strconv.Atoi(os.Getenv("NOVA_SPRINT_HANDOFF_FD"))
	if err != nil {
		return edge.Cause("continuation channel unavailable: NOVA_SPRINT_HANDOFF_FD", err)
	}
	if fd != 3 {
		return fmt.Errorf("continuation channel unavailable: NOVA_SPRINT_HANDOFF_FD is %d, want 3", fd)
	}
	if !continuationID.MatchString(id) {
		return fmt.Errorf("continuation channel unavailable: continuation id %q is not well formed", edge.Redact(id))
	}
	file := os.NewFile(uintptr(fd), "setup-continuation")
	if file == nil {
		return fmt.Errorf("continuation channel unavailable: fd 3 is not open")
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(handoffStatus{Status: "continue", ID: id})
}

type handoffRead struct {
	id  string
	err error
}

func waitHandoff(ctx context.Context, result <-chan handoffRead) (string, error) {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case r := <-result:
		return r.id, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", fmt.Errorf("continuation channel did not close")
	}
}
