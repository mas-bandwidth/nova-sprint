package verbs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

const takeNotePrefix = "take:v1:"
const takeNoteLimit = 4096

type takeInput struct {
	id, actor, opID string
	gen, epoch      int
	machine         bool
}
type takeNote struct {
	Version   int    `json:"version"`
	Repo      string `json:"repo"`
	Base      string `json:"base"`
	RequirePR bool   `json:"pr"`
}
type machineTake struct {
	Consumer, Producer, Kind, Stream, Repo, Base, Branch, Head, Tier, Brief, Prose, Finding string
	StreamRef                                                                               string   `json:"stream_ref"`
	BaseSHA                                                                                 string   `json:"base_sha"`
	PriorHead                                                                               string   `json:"prior_head"`
	Paths                                                                                   []string `json:"paths"`
	DoneWhen                                                                                []string `json:"done_when"`
	Epoch, Gen, Attempt, Review                                                             int
	RequirePR                                                                               bool `json:"require_pr"`
}

func parseTake(args []string, actor string) (takeInput, bool) {
	in := takeInput{actor: actor}
	if len(args) == 0 {
		return in, false
	}
	var ok bool
	in.id, in.gen, ok = cardRef(args[0])
	if !ok {
		return in, false
	}
	filtered := []string{}
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		key := args[i]
		if seen[key] {
			return in, false
		}
		seen[key] = true
		if key == "--json" {
			in.machine = true
			continue
		}
		if i+1 >= len(args) || args[i+1] == "" {
			return in, false
		}
		if key == "--id" {
			in.opID = args[i+1]
		} else {
			filtered = append(filtered, key, args[i+1])
		}
		i++
	}
	in.actor, in.epoch, ok = parseActor(filtered, actor)
	if !ok {
		return in, false
	}
	if in.machine {
		return in, in.opID != "" && in.epoch > 0
	}
	return in, in.opID == ""
}
func (e *Engine) takeNote(ctx context.Context, snap store.Snapshot, c store.Card, machine bool) (string, error) {
	stream, ok := snap.Streams[c.Stream]
	if !ok {
		return "", fmt.Errorf("stream unavailable")
	}
	st, ok := e.Store.(store.Store)
	if !ok {
		if machine {
			return "", fmt.Errorf("stream settings unavailable")
		}
		return stream.Repo, nil
	}
	body, found, err := (Choices{Store: st}).getAt(ctx, "stream:"+c.Stream, snap)
	if err != nil {
		return "", err
	}
	if !found {
		if machine {
			return "", fmt.Errorf("stream settings unavailable")
		}
		return stream.Repo, nil
	}
	var settings streamSettings
	if json.Unmarshal([]byte(body), &settings) != nil || settings.Repo != stream.Repo || settings.Base != stream.Base || !validMode(settings.Mode) {
		return "", fmt.Errorf("stream settings invalid")
	}
	raw, err := json.Marshal(takeNote{Version: 1, Repo: stream.Repo, Base: stream.Base, RequirePR: settings.PR})
	if err != nil || len(raw) > takeNoteLimit {
		return "", fmt.Errorf("stream metadata invalid")
	}
	return takeNotePrefix + string(raw), nil
}
func parseTakeNote(note string) (takeNote, bool) {
	var n takeNote
	if !strings.HasPrefix(note, takeNotePrefix) || len(note) > takeNoteLimit+len(takeNotePrefix) {
		return n, false
	}
	decoder := json.NewDecoder(bytes.NewBufferString(strings.TrimPrefix(note, takeNotePrefix)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&n) != nil || n.Version != 1 || n.Repo == "" || n.Base == "" {
		return takeNote{}, false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return takeNote{}, false
	}
	return n, true
}
func takeHumanRepo(note string) string {
	if n, ok := parseTakeNote(note); ok {
		return n.Repo
	}
	return note
}
func machineTakeReceipt(r store.Receipt, actor, id, opID string) Result {
	if r.Op != opID || r.Actor != actor || r.Kind != "take" {
		return refused("take", "operation cannot be performed", "nova-sprint next --as "+actor)
	}
	if !r.Accepted {
		return refused("take", r.Reason, "nova-sprint show "+id)
	}
	if r.After == nil {
		return unavailable("take")
	}
	c, ok := r.After.Cards[id]
	if !ok {
		return unavailable("take")
	}
	n, ok := parseTakeNote(r.Note)
	if !ok {
		return unavailable("take")
	}
	out := machineTake{Consumer: c.ID, Producer: c.Of, Kind: c.Kind, Stream: c.Stream, Repo: n.Repo, Base: n.Base, Branch: c.Branch, Head: c.Head, Tier: c.Tier, Brief: c.Brief, Prose: c.Prose, Finding: c.Finding, StreamRef: "sprint/" + c.Stream + "/group", BaseSHA: c.BaseSHA, PriorHead: c.Prior, Paths: c.Paths, DoneWhen: c.DoneWhen, Epoch: r.Epoch, Gen: c.Gen, Attempt: c.Attempt, Review: c.Review, RequirePR: n.RequirePR}
	data, err := json.Marshal(out)
	if err != nil {
		return unavailable("take")
	}
	return Result{string(data) + "\n", 0}
}
