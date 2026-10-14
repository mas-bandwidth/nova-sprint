// Package must provides one-line assertions for the multi-line check shapes
// that repeat across the repository's tests and that testify leaves at three or
// more lines: a text that must carry several substrings and none of others, a
// file that must read as a given body, a JSON document that must carry given
// keys, and a step whose only refusal must be read before its reason is judged.
//
// A test names the row it checks and calls one function; require is for setup,
// assert is for the check, so one bad row does not hide the rest.
//
//	func TestQueue(t *testing.T) {
//		t.Parallel()
//		for _, tc := range cases {
//			t.Run(tc.name, func(t *testing.T) {
//				t.Parallel()
//				must.ContainsAll(t, tc.got, tc.want...)
//			})
//		}
//	}
package must

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ContainsAll asserts that got carries every wanted substring.
func ContainsAll(t testing.TB, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		assert.Contains(t, got, want)
	}
}

// ContainsNone asserts that got carries none of the unwanted substrings.
func ContainsNone(t testing.TB, got string, nots ...string) {
	t.Helper()
	for _, not := range nots {
		assert.NotContains(t, got, not)
	}
}

// FileReads asserts that the file at path reads exactly as body and is
// readable.
func FileReads(t testing.TB, path, body string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if !assert.NoError(t, err, "must: reading %s", path) {
		return
	}
	assert.Equal(t, body, string(b), "must: %s", path)
}

// JSONHas asserts that doc parses as a JSON object carrying every key.
func JSONHas(t testing.TB, doc string, keys ...string) {
	t.Helper()
	var m map[string]json.RawMessage
	if !assert.NoError(t, json.Unmarshal([]byte(doc), &m), "must: JSON object") {
		return
	}
	for _, k := range keys {
		assert.Contains(t, m, k)
	}
}

// SoleRefusal asserts that a step moved nothing and was refused exactly once,
// and returns that one refusal so its reason can be judged. It fails the test
// when moved or refused has any other size.
func SoleRefusal[M any, R any](t testing.TB, moved []M, refused []R) R {
	t.Helper()
	assert.Empty(t, moved, "must: a refused step moved something")
	assert.Len(t, refused, 1, "must: one refusal")
	var zero R
	if len(refused) != 1 {
		return zero
	}
	return refused[0]
}
