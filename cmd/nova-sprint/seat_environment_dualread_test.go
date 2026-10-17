package main

// TestSprintSeatEnvironmentDualRead pins the one seat environment read (S1,
// S2, S4): the record app.loadSeatEnvironment reads, read alike from its binary
// image, its padded standard base64, the schema JSON text and today's legacy
// form. A malformed value of each form is refused by name, never read as
// absent, empty or zero, and reads are counted per record and per form. The
// files the read site reads come from a t.TempDir() this test writes; no
// fixture holds a secret value.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seatDualSample is the one record every form is read as: a version 1 seat
// with the three endpoints and a forge identity directory the read validates.
// It holds no secret value.
func seatDualSample(t *testing.T) seatEnvironment {
	t.Helper()
	return seatEnvironment{
		Version: 1,
		Environment: map[string]string{
			"NOVA_SPRINT_REDIS":  "r:6380",
			"NOVA_SPRINT_SERVER": "s:6390",
			"NOVA_BUS_REDIS":     "b:6381",
		},
		ForgeIdentity: t.TempDir(),
	}
}

// seatDamage flips the last byte of a wire image.
func seatDamage(b []byte) []byte {
	out := append([]byte(nil), b...)
	out[len(out)-1] ^= 0xff
	return out
}

// seatRefusalReason is the reason a refusal names, or "" when it reads.
func seatRefusalReason(t *testing.T, r *schemaread.Reader[seatEnvironment], in []byte) string {
	t.Helper()
	_, _, err := r.Read(in)
	if err == nil {
		return ""
	}
	var ref *schemaread.Refusal
	require.True(t, errors.As(err, &ref), "every refusal is a *schemaread.Refusal: %v", err)
	assert.True(t, strings.HasPrefix(err.Error(), "SEATENVIRONMENT REFUSED: "), err.Error())
	assert.True(t, strings.HasSuffix(err.Error(), "; run: "+seatHelp), err.Error())
	return ref.Reason
}

func TestSprintSeatEnvironmentDualRead(t *testing.T) {
	t.Parallel()

	want := seatDualSample(t)
	r := newSeatEnvironmentReader()

	bin, err := r.Write(&want, schemaread.FormBinary)
	require.NoError(t, err)
	require.NotEmpty(t, bin)
	assert.Equal(t, byte(1), bin[0], "the variable image opens with form byte 1")

	b64, err := r.Write(&want, schemaread.FormBase64)
	require.NoError(t, err)
	assert.Equal(t, base64.StdEncoding.EncodeToString(bin), string(b64), "text carries padded standard base64 of the wire")

	js, err := r.Write(&want, schemaread.FormJSON)
	require.NoError(t, err)
	assert.Equal(t, byte('\n'), js[len(js)-1], "the schema JSON text ends with one newline")

	leg, err := json.Marshal(want)
	require.NoError(t, err)
	assert.False(t, strings.Contains(string(leg), "\n"), "today's legacy JSON has no trailing newline")

	// every form reads to the one equal value of today's Go struct
	for _, tc := range []struct {
		name string
		in   []byte
		form schemaread.Form
	}{
		{"fixed binary", bin, schemaread.FormBinary},
		{"padded base64", b64, schemaread.FormBase64},
		{"schema JSON", js, schemaread.FormJSON},
		{"legacy JSON", leg, schemaread.FormLegacy},
	} {
		got, res, err := r.Read(tc.in)
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.form, res.Form, tc.name)
		assert.Equal(t, want, got, tc.name)
	}

	// the read site reads each form from the file the test writes
	for _, tc := range []struct {
		name string
		in   []byte
	}{
		{"fixed binary", bin},
		{"padded base64", b64},
		{"schema JSON", js},
		{"legacy JSON", leg},
	} {
		a := seatEnvApp(t, string(tc.in))
		require.NoError(t, a.loadSeatEnvironment(), tc.name)
		require.NotNil(t, a.seatEnvironment, tc.name)
		assert.Equal(t, want, *a.seatEnvironment, tc.name)
	}

	// a malformed value of each form is refused by name, never the zero value
	var binReason string
	for _, tc := range []struct {
		name string
		in   []byte
	}{
		{"truncated wire", bin[:len(bin)-1]},
		{"damaged wire", seatDamage(bin)},
		{"truncated base64", b64[:len(b64)-3]},
		{"malformed schema JSON", js[:len(js)/2]},
		{"malformed legacy", []byte(`{"version":1`)},
	} {
		reason := seatRefusalReason(t, r, tc.in)
		require.NotEmpty(t, reason, tc.name)
		if tc.name == "truncated wire" {
			binReason = reason
		}
		a := seatEnvApp(t, string(tc.in))
		err := a.loadSeatEnvironment()
		require.Error(t, err, tc.name)
		assert.Nil(t, a.seatEnvironment, tc.name+": a refusal reads as nothing")
	}

	// reads are counted per record and per form; a refusal counts under its
	// form and its reason, and reads nothing
	counts := newSeatEnvironmentReader()
	wantCounts := map[string]uint64{"binary": 0, "base64": 0, "json": 0, "legacy": 0, "unknown_fields": 0}
	assert.Equal(t, wantCounts, counts.Counts().Snapshot())
	step := func(in []byte, key string) {
		t.Helper()
		_, _, err := counts.Read(in)
		require.NoError(t, err, key)
		wantCounts[key]++
		assert.Equal(t, wantCounts, counts.Counts().Snapshot(), "after a %s read only %s moved", key, key)
	}
	step(leg, "legacy")
	step(bin, "binary")
	step(b64, "base64")
	step(js, "json")
	step(js, "json")
	_, res, err := counts.Read(bin[:len(bin)-1])
	require.Error(t, err)
	assert.Equal(t, schemaread.FormBinary, res.Form)
	wantCounts["binary"]++
	wantCounts["refused_"+binReason] = 1
	assert.Equal(t, wantCounts, counts.Counts().Snapshot())
}
