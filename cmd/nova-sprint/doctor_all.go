package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

type doctorRunResult struct {
	code   int
	stdout string
	stderr string
}

type doctorPair struct {
	novaDoctor doctorRunResult
	sprint     doctorRunResult
}

// runDoctorPair starts both checks before waiting for either. Results stay in
// their named slots so their output order is stable regardless of completion.
func runDoctorPair(novaDoctor, sprintDoctor func() doctorRunResult) doctorPair {
	var pair doctorPair
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		pair.novaDoctor = novaDoctor()
	}()
	go func() {
		defer wg.Done()
		pair.sprint = sprintDoctor()
	}()
	wg.Wait()
	return pair
}

func runNovaDoctor(asJSON bool) doctorRunResult {
	args := []string{"run"}
	if asJSON {
		args = append(args, "--json")
	}
	cmd := exec.Command("nova-doctor", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = 2
			fmt.Fprintf(&stderr, "nova-doctor run: %s\n", err)
		}
	}
	return doctorRunResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func pairExit(pair doctorPair) int {
	if pair.novaDoctor.code < 0 || pair.novaDoctor.code >= 2 || pair.sprint.code < 0 || pair.sprint.code >= 2 {
		return 2
	}
	if pair.novaDoctor.code != 0 || pair.sprint.code != 0 {
		return 1
	}
	return 0
}

func printDoctorPair(stdout, stderr io.Writer, pair doctorPair) {
	print := func(name string, result doctorRunResult) {
		fmt.Fprintf(stdout, "DOCTOR ALL %s exit=%d\n", name, result.code)
		if result.stdout != "" {
			io.WriteString(stdout, result.stdout)
			if !strings.HasSuffix(result.stdout, "\n") {
				io.WriteString(stdout, "\n")
			}
		}
		if result.stderr != "" {
			fmt.Fprintf(stderr, "DOCTOR ALL %s stderr:\n", name)
			io.WriteString(stderr, result.stderr)
			if !strings.HasSuffix(result.stderr, "\n") {
				io.WriteString(stderr, "\n")
			}
		}
	}
	print("nova-doctor", pair.novaDoctor)
	print("nova-sprint", pair.sprint)
}

func printDoctorPairJSON(w io.Writer, pair doctorPair) {
	child := func(result doctorRunResult) map[string]any {
		var report any
		if json.Unmarshal([]byte(result.stdout), &report) != nil {
			report = result.stdout
		}
		return map[string]any{"exit": result.code, "report": report, "stderr": result.stderr}
	}
	code := pairExit(pair)
	status := "ok"
	if code != 0 {
		status = "red"
	}
	b, err := json.Marshal(map[string]any{
		"verb": "doctor", "mode": "all", "status": status, "exit": code,
		"doctors": map[string]any{
			"nova-doctor": child(pair.novaDoctor),
			"nova-sprint": child(pair.sprint),
		},
	})
	if err != nil {
		fmt.Fprintf(w, "{\"verb\":\"doctor\",\"mode\":\"all\",\"status\":\"red\",\"exit\":2,\"error\":%q}\n", err.Error())
		return
	}
	fmt.Fprintln(w, string(b))
}
