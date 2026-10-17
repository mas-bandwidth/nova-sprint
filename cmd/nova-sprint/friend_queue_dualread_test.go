package main

// TestSprintFriendQueueDualRead is the friend queue file's dual-read interop
// test at the read site nova-sprint's writeQueueFile reads (the same file
// nova-friend reads, read before it is rewritten). The queue file arrives as
// today's legacy JSON (the object with tasks, and the bare list), the schema
// JSON text, padded standard base64 of the binary wire, or the binary wire;
// every form reads to one equal value, a damaged value of each form is refused
// by name and never read as absent, empty or zero, and each read is counted
// once under the form read. Writers stay legacy here; the writer switch is card
// a10-sprint-friend-queue-writer.

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/friendfilewire"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queueTestSample is the friend queue value the forms are made from.
func queueTestSample() friendQueue {
	return friendQueue{Tasks: []friendTask{
		{Gen: 2, Job: "a10-sprint-friend-queue-dual-read.g1", ID: "a10-sprint-friend-queue-dual-read", State: "queued", Deliverable: ""},
		{Gen: 1, Job: "a10-old-work.g1", ID: "a10-old-work", State: "working", Deliverable: "inbox/a10-old-work/BRIEF.md"},
	}}
}

// queueTestBuild is the FriendQueue table value of tasks, the writer side a
// switched writer runs.
func queueTestBuild(t *testing.T, tasks []friendTask) *friendfilewire.FriendQueue {
	t.Helper()
	var b friendfilewire.FriendQueueBuilder
	require.True(t, b.Init())
	q := b.GetRoot()
	for _, tk := range tasks {
		row := friendfilewire.FriendQueueTasksAdd(&b.Main, &q.Tasks)
		row.Gen = int64(tk.Gen)
		emplace := func(slot *int64, s string) {
			if s != "" {
				friendfilewire.TableStringEmplace(&b.Main, slot, []byte(s))
			}
		}
		emplace(&row.Job, tk.Job)
		emplace(&row.Id, tk.ID)
		emplace(&row.State, tk.State)
		emplace(&row.Deliverable, tk.Deliverable)
	}
	require.True(t, b.Lock())
	return b.AsConst()
}

func queueTestWire(t *testing.T, q *friendfilewire.FriendQueue) []byte {
	t.Helper()
	n := friendfilewire.FriendQueueMeasure(q)
	require.Positive(t, n)
	buf := make([]byte, n)
	require.Equal(t, n, friendfilewire.FriendQueueSave(q, buf))
	return buf
}

func queueTestJSON(t *testing.T, q *friendfilewire.FriendQueue) []byte {
	t.Helper()
	n := friendfilewire.FriendQueueToJsonMeasure(q)
	require.Positive(t, n)
	buf := make([]byte, n)
	require.Equal(t, n, friendfilewire.FriendQueueToJson(q, buf))
	return buf
}

func queueTestRefusal(reason, field string) string {
	if field != "" {
		reason += " (" + field + ")"
	}
	return "FRIENDQUEUE REFUSED: " + reason + "; run: nova-sprint friend"
}

func TestSprintFriendQueueDualRead(t *testing.T) {
	t.Parallel()

	sample := queueTestSample()
	q := queueTestBuild(t, sample.Tasks)
	wire := queueTestWire(t, q)
	b64 := []byte(base64.StdEncoding.EncodeToString(wire))
	jsonText := queueTestJSON(t, q)

	legacyObject := `{"tasks": [
  {
    "gen": 2,
    "job": "a10-sprint-friend-queue-dual-read.g1",
    "id": "a10-sprint-friend-queue-dual-read",
    "state": "queued"
  },
  {
    "gen": 1,
    "job": "a10-old-work.g1",
    "id": "a10-old-work",
    "state": "working",
    "deliverable": "inbox/a10-old-work/BRIEF.md"
  }
]}`
	legacyBare := `[
  {
    "gen": 2,
    "job": "a10-sprint-friend-queue-dual-read.g1",
    "id": "a10-sprint-friend-queue-dual-read",
    "state": "queued"
  },
  {
    "gen": 1,
    "job": "a10-old-work.g1",
    "id": "a10-old-work",
    "state": "working",
    "deliverable": "inbox/a10-old-work/BRIEF.md"
  }
]`

	t.Run("every form reads to the same value", func(t *testing.T) {
		t.Parallel()
		r := newFriendQueueRead()
		cases := []struct {
			name string
			in   []byte
			form schemaread.Form
		}{
			{"binary wire", wire, schemaread.FormBinary},
			{"padded base64", b64, schemaread.FormBase64},
			{"schema JSON text", jsonText, schemaread.FormJSON},
			{"legacy object", []byte(legacyObject), schemaread.FormJSON},
			{"legacy bare list", []byte(legacyBare), schemaread.FormLegacy},
		}
		for _, c := range cases {
			got, res, err := r.r.Read(c.in)
			require.NoError(t, err, "%s", c.name)
			assert.Equal(t, c.form, res.Form, "%s", c.name)
			assert.Equal(t, sample, got, "%s: every form reads to one equal value", c.name)
		}
		// each form counted once under the form read
		assert.Equal(t, uint64(1), r.r.Counts().Reads(schemaread.FormBinary))
		assert.Equal(t, uint64(1), r.r.Counts().Reads(schemaread.FormBase64))
		assert.Equal(t, uint64(2), r.r.Counts().Reads(schemaread.FormJSON))
		assert.Equal(t, uint64(1), r.r.Counts().Reads(schemaread.FormLegacy))
	})

	t.Run("the legacy object reads when a field is absent", func(t *testing.T) {
		t.Parallel()
		r := newFriendQueueRead()
		// a legacy file omits empty deliverable and gen fields; it must still read
		ones := []byte(`{"tasks":[{"id":"card-1","state":"queued"}]}`)
		got, res, err := r.r.Read(ones)
		require.NoError(t, err)
		assert.Equal(t, schemaread.FormJSON, res.Form)
		assert.Equal(t, friendQueue{Tasks: []friendTask{{ID: "card-1", State: "queued"}}}, got)
	})

	t.Run("a damaged value of each form is refused by name", func(t *testing.T) {
		t.Parallel()
		r := newFriendQueueRead()
		refuse := func(name string, in []byte, want string) {
			t.Helper()
			got, _, err := r.r.Read(in)
			require.Error(t, err, name)
			var ref *schemaread.Refusal
			require.True(t, errors.As(err, &ref), "%s: every refusal is a *schemaread.Refusal", name)
			assert.Equal(t, want, err.Error(), name)
			assert.Zero(t, got, "%s: a refused read is the zero value", name)
		}
		flipped := append([]byte{}, wire...)
		flipped[len(flipped)-1] ^= 0xff
		caseC := bytes.Clone(jsonText)
		caseC[len(caseC)-1] ^= 0xff
		cases := []struct {
			name string
			in   []byte
			want string
		}{
			{"truncated wire", wire[:len(wire)-1], queueTestRefusal("wire_damaged", "")},
			{"flipped wire byte", flipped, queueTestRefusal("wire_damaged", "")},
			{"truncated base64", b64[:len(b64)-3], queueTestRefusal("malformed", "")},
			{"base64 with a stray byte", append(append([]byte{}, b64...), '!'), queueTestRefusal("malformed", "")},
			{"a form this reader does not know", append([]byte{9}, wire[1:]...), queueTestRefusal("unknown_form", "")},
			{"not a queue at all", []byte("#202 HOLD at 3f9c"), queueTestRefusal("unknown_form", "")},
			{"an unclosed JSON text", []byte(`{"tasks": [{"id": "x", "state": "queued"`), queueTestRefusal("malformed", "")},
			{"a JSON key given twice", []byte(`{"tasks": [], "tasks": []}`), queueTestRefusal("malformed", "")},
			{"a damaged schema JSON", caseC, queueTestRefusal("malformed", "")},
			{"a bare list that is not JSON", []byte(`[{"id": "x", `), queueTestRefusal("legacy_json_unreadable", "")},
		}
		for _, c := range cases {
			refuse(c.name, c.in, c.want)
		}
		for i := range wire {
			assert.NotEmpty(t, func() string {
				_, _, err := r.r.Read(wire[:i])
				if err == nil {
					return ""
				}
				return err.Error()
			}(), "every truncation of the binary wire is refused, never read as a shorter queue: cut at %d", i)
		}
		assert.Positive(t, r.r.Counts().Refused("wire_damaged"))
		assert.Positive(t, r.r.Counts().Refused("malformed"))
		assert.Positive(t, r.r.Counts().Refused("unknown_form"))
	})

	t.Run("the per-form counter moves on the form read", func(t *testing.T) {
		t.Parallel()
		r := newFriendQueueRead()
		want := map[string]uint64{"binary": 0, "base64": 0, "json": 0, "legacy": 0, "unknown_fields": 0}
		assert.Equal(t, want, r.r.Counts().Snapshot())
		read := func(in []byte, key string) {
			t.Helper()
			_, _, err := r.r.Read(in)
			require.NoError(t, err, key)
			want[key]++
			assert.Equal(t, want, r.r.Counts().Snapshot(), "after a %s read only %s moved", key, key)
		}
		read(wire, "binary")
		read(b64, "base64")
		read(jsonText, "json")
		read([]byte(legacyBare), "legacy")
		read(jsonText, "json")
		_, res, err := r.r.Read(wire[:9])
		require.Error(t, err)
		assert.Equal(t, schemaread.FormBinary, res.Form)
		want["binary"]++
		want["refused_wire_damaged"] = 1
		assert.Equal(t, want, r.r.Counts().Snapshot())
	})

	t.Run("writeQueueFile reads a file from a tempdir", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := dir + "/inbox/QUEUE.json"
		require.NoError(t, os.MkdirAll(dir+"/inbox", 0o755))
		require.NoError(t, os.WriteFile(path, append(jsonText, '\n'), 0o644))

		got, form, err := readQueueFile(path)
		require.NoError(t, err)
		assert.Equal(t, schemaread.FormJSON, form)
		assert.Equal(t, sample, got)
	})
}
