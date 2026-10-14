package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two doctors start before either result is awaited. Release them in the
// opposite order from their display slots; text output remains stable.
func TestRunDoctorPairStartsBothAndKeepsStableTextOrder(t *testing.T) {
	t.Parallel()
	started := make(chan string, 2)
	finished := make(chan string, 2)
	novaRelease := make(chan struct{})
	sprintRelease := make(chan struct{})
	pairDone := make(chan doctorPair, 1)

	call := func(name string, release <-chan struct{}, result doctorRunResult) func() doctorRunResult {
		return func() doctorRunResult {
			started <- name
			<-release
			finished <- name
			return result
		}
	}
	go func() {
		pairDone <- runDoctorPair(
			call("nova-doctor", novaRelease, doctorRunResult{code: 1, stdout: "DOCTOR FAIL nova check\n", stderr: "nova detail\n"}),
			call("nova-sprint", sprintRelease, doctorRunResult{stdout: "DOCTOR OK sprint check\n"}),
		)
	}()

	first, second := <-started, <-started
	assert.ElementsMatch(t, []string{"nova-doctor", "nova-sprint"}, []string{first, second}, "both callbacks enter before either is released")
	select {
	case name := <-finished:
		require.FailNowf(t, "", "%s completed before the test released either callback", name)
	default:
	}

	close(sprintRelease)
	assert.Equal(t, "nova-sprint", <-finished, "release sprint first to reverse completion order")
	close(novaRelease)
	assert.Equal(t, "nova-doctor", <-finished)
	pair := <-pairDone
	assert.Equal(t, 1, pairExit(pair), "one refusal makes the pair red")

	var stdout, stderr bytes.Buffer
	printDoctorPair(&stdout, &stderr, pair)
	assert.Equal(t, "DOCTOR ALL nova-doctor exit=1\nDOCTOR FAIL nova check\nDOCTOR ALL nova-sprint exit=0\nDOCTOR OK sprint check\n", stdout.String())
	assert.Equal(t, "DOCTOR ALL nova-doctor stderr:\nnova detail\n", stderr.String(), "child stderr stays separate")
}

// A failed child does not suppress the other result. Valid child JSON is
// decoded; malformed output is retained verbatim as that child's report.
func TestDoctorPairJSONPreservesBothFailureReports(t *testing.T) {
	t.Parallel()
	pair := doctorPair{
		novaDoctor: doctorRunResult{code: 2, stdout: "not-json\n", stderr: "nova invocation failed\n"},
		sprint:     doctorRunResult{code: 1, stdout: `{"status":"refused"}`, stderr: "sprint refusal detail\n"},
	}
	assert.Equal(t, 2, pairExit(pair), "an invocation failure takes precedence")

	var stdout, stderr bytes.Buffer
	printDoctorPairJSON(&stdout, pair)
	assert.Empty(t, stderr.String())
	var got struct {
		Status  string `json:"status"`
		Exit    int    `json:"exit"`
		Doctors map[string]struct {
			Exit   int             `json:"exit"`
			Report json.RawMessage `json:"report"`
			Stderr string          `json:"stderr"`
		} `json:"doctors"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got), stdout.String())
	assert.Equal(t, "red", got.Status)
	assert.Equal(t, 2, got.Exit)
	require.Contains(t, got.Doctors, "nova-doctor")
	require.Contains(t, got.Doctors, "nova-sprint")
	assert.Equal(t, 2, got.Doctors["nova-doctor"].Exit)
	var raw string
	require.NoError(t, json.Unmarshal(got.Doctors["nova-doctor"].Report, &raw))
	assert.Equal(t, "not-json\n", raw)
	assert.Equal(t, "nova invocation failed\n", got.Doctors["nova-doctor"].Stderr)
	assert.Equal(t, 1, got.Doctors["nova-sprint"].Exit)
	var report map[string]string
	require.NoError(t, json.Unmarshal(got.Doctors["nova-sprint"].Report, &report))
	assert.Equal(t, "refused", report["status"])
	assert.Equal(t, "sprint refusal detail\n", got.Doctors["nova-sprint"].Stderr)
}

func TestDoctorHelpListsAllModeAndExistingOptions(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	a := newApp(func(string) string { return "" })
	code := runInto(a, []string{"doctor", "-h"}, &stdout, &stderr)
	require.Equal(t, 0, code, "%s", stderr.String())
	assert.Empty(t, stderr.String())
	for _, flag := range []string{"--all", "--area", "--preflight", "--json"} {
		assert.Contains(t, stdout.String(), flag, "doctor help: %s", stdout.String())
	}
}
