package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func runStub(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

// Every retired verb refuses with the verb it moved to, and nothing else runs.
func TestRetiredVerbsRefuseWithWhereTheyMoved(t *testing.T) {
	t.Parallel()
	for _, verb := range []string{"generate", "template", "lint"} {
		code, stdout, stderr := runStub(verb, "--from", "findings")
		assert.Equal(t, 2, code, verb)
		assert.Empty(t, stdout, verb)
		assert.Equal(t, "nova-card "+verb+" moved to nova-sprint card "+verb+"; run: nova-sprint card "+verb+" -h\n", stderr)
	}
	code, stdout, _ := runStub("version")
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "nova-card")
	code, _, stderr := runStub()
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "nova-sprint card generate")
	code, _, stderr = runStub("bogus")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "run: nova-sprint help")
}
