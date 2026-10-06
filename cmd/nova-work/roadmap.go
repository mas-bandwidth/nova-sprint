package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/roadmap"
	"github.com/mas-bandwidth/nova-sprint/internal/tool"
)

// roadmapVerbs edit a roadmap sexp (roadmaps/*.sexp, docs/SPEC-WORK-V1.md
// section 1.12) so no one edits it by hand: every one that writes refuses a file
// check already rejects, checks the bytes it is about to save the same way,
// and writes nothing when either fails.
func roadmapVerbs() []tool.Verb {
	return []tool.Verb{
		{
			Name:      "roadmap check",
			Usage:     "roadmap check --file <roadmap.sexp>",
			Effect:    tool.Inspection,
			Detail:    roadmapDetail,
			ExitTable: "0 balanced, counted, every card with one id and no empty stream; 1 check found problems (unbalanced or unparsable, a count mismatch, a card with no id, a duplicate id, an empty stream), one FAILED line each; 2 could not run (a flag, an unreadable file)",
			Flags: func(f *tool.Flags) {
				f.Required("file", "the roadmap file to check")
			},
			Run: roadmapCheck,
		},
		{
			Name:      "roadmap add",
			Usage:     "roadmap add --file <roadmap.sexp> --stream <s> --brief-dir <dir>",
			Effect:    tool.LocalWrite + ": rewrites --file",
			Detail:    roadmapDetail,
			ExitTable: "0 the cards are added and the file saved; 2 could not run, nothing written (a flag, an unreadable file or directory, a card id already in the roadmap, an empty brief, a file check rejects)",
			Flags: func(f *tool.Flags) {
				f.Required("file", "the roadmap file to edit")
				f.Required("stream", "the stream of the first release to add the cards to; created when it is not there")
				f.Required("brief-dir", "the directory of briefs: each <id>.md is one card")
			},
			Run: roadmapAdd,
		},
		{
			Name:      "roadmap remove",
			Usage:     "roadmap remove --file <roadmap.sexp> --id <id>...",
			Effect:    tool.LocalWrite + ": rewrites --file",
			Detail:    roadmapDetail,
			ExitTable: "0 the cards are removed and the file saved; 2 could not run, nothing written (a flag, an unreadable file, an id not in the roadmap, a file check rejects)",
			Flags: func(f *tool.Flags) {
				f.Required("file", "the roadmap file to edit")
				idFlag(f, "remove")
			},
			Run: roadmapRemove,
		},
		{
			Name:      "roadmap pull",
			Usage:     "roadmap pull --file <roadmap.sexp> --id <id>... --out <dir>",
			Effect:    tool.LocalWrite + ": writes <id>.md in --out and rewrites --file",
			Detail:    roadmapDetail,
			ExitTable: "0 the briefs are written and the cards removed; 2 could not run, nothing written (a flag, an unreadable file, an id not in the roadmap, a card with an empty brief, a file check rejects)",
			Flags: func(f *tool.Flags) {
				f.Required("file", "the roadmap file to edit")
				idFlag(f, "pull")
				f.Required("out", "the directory the <id>.md briefs are written to; created when it is not there")
			},
			Run: roadmapPull,
		},
		{
			Name:      "roadmap note",
			Usage:     "roadmap note --file <roadmap.sexp> --id <id>... --text <t>",
			Effect:    tool.LocalWrite + ": rewrites --file",
			Detail:    roadmapDetail,
			ExitTable: "0 the text is appended and the file saved; 2 could not run, nothing written (a flag, an unreadable file, an id not in the roadmap, a file check rejects)",
			Flags: func(f *tool.Flags) {
				f.Required("file", "the roadmap file to edit")
				idFlag(f, "note")
				f.Required("text", "the text to append, after a blank line, to each card's :brief")
			},
			Run: roadmapNote,
		},
	}
}

const roadmapDetail = `A roadmap is one (:roadmap ... :releases ((:release "<v>" ... :cards <n>
:streams ((:stream "<s>" :cards ((:id "<id>" :tier "<t>" :needs ("<id>"...)
:title "<t>" :brief "<b>") ...)) ...)) ...)) form (docs/SPEC-WORK-V1.md
section 1.12). The verbs keep every byte they do not change: comments, spacing
and the order of everything else.

check: string-aware paren balance, each release's :cards equal to the cards
in its streams, every card with an :id, no id twice, no stream with no card.
add: each <id>.md in --brief-dir becomes one card of --stream in the first
release (TIER: and DEPENDS-ON: or NEEDS: lines give :tier and :needs, the
first sentence after THE TASK. the :title), and :cards goes up.
remove: the cards go, :cards goes down, a stream left with none goes.
pull: as remove, after writing each card's :brief to <id>.md in --out, the
brief nova-sprint add reads; one CARD line names each card's release and stream.
note: --text is appended to each card's :brief after a blank line.

A verb that writes refuses a file check rejects (run check to see why),
and checks the bytes it would save the same way: nothing is written unless
they pass.
`

// refuse is a refusal whose remedy is the verb's own help.
func refuse(verb, why string) *tool.Out {
	o := tool.Refuse(why)
	o.Remedy = "nova-work roadmap " + verb + " -h"
	return o
}

// idFlag declares --id, repeated: the cards a verb acts on.
func idFlag(f *tool.Flags, verb string) {
	f.Var(&idList{}, "id", "a card `id` to "+verb+"; repeat for more (required)")
	f.Check(func(c *tool.Call) {
		if len(ids(c)) == 0 {
			c.Problem("--id is required; it wants a card id, repeated for more; refusing to guess")
		}
	})
}

// idList is --id, repeated.
type idList []string

func (l *idList) String() string { return strings.Join(*l, ",") }
func (l *idList) Get() any       { return []string(*l) }
func (l *idList) Set(v string) error {
	if strings.TrimSpace(v) == "" || strings.ContainsAny(v, " \t\n\"") {
		return fmt.Errorf("%q is not a card id", v)
	}
	*l = append(*l, v)
	return nil
}

func ids(c *tool.Call) []string { return c.Get("id").([]string) }

// loadChecked loads --file and refuses one check already rejects: a verb
// edits only a roadmap that is sound.
func loadChecked(c *tool.Call, verb string) (*roadmap.Roadmap, *tool.Out) {
	file := c.Str("file")
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, refuse(verb, err.Error())
	}
	if problems := roadmap.CheckBytes(data); len(problems) > 0 {
		o := tool.Refuse(fmt.Sprintf("%s fails check (%s); nothing written", file, problems[0]))
		o.Remedy = "nova-work roadmap check --file " + file
		return nil, o
	}
	r, err := roadmap.LoadBytes(data)
	if err != nil {
		return nil, refuse(verb, err.Error())
	}
	return r, nil
}

// save writes r to --file only when its bytes pass check.
func save(c *tool.Call, verb string, r *roadmap.Roadmap) *tool.Out {
	file := c.Str("file")
	if problems := roadmap.CheckBytes(r.Bytes()); len(problems) > 0 {
		return refuse(verb, fmt.Sprintf("the edit would leave %s failing check (%s); nothing written", file, problems[0]))
	}
	if err := r.Save(file); err != nil {
		return refuse(verb, fmt.Sprintf("save %s: %v", file, err))
	}
	return nil
}

func total(r *roadmap.Roadmap) int {
	n := 0
	for _, rel := range r.Releases {
		n += rel.CardsCount
	}
	return n
}

func roadmapCheck(c *tool.Call) *tool.Out {
	file := c.Str("file")
	problems, err := roadmap.CheckFile(file)
	if err != nil {
		return refuse("check", err.Error())
	}
	if len(problems) > 0 {
		return tool.Fail(problems...).Fact("file", file).Fact("problems", len(problems))
	}
	r, err := roadmap.Load(file)
	if err != nil {
		return refuse("check", err.Error())
	}
	streams := 0
	for _, rel := range r.Releases {
		streams += len(rel.Streams)
	}
	return tool.Done().Fact("file", file).Fact("cards", total(r)).Fact("releases", len(r.Releases)).Fact("streams", streams)
}

func roadmapAdd(c *tool.Call) *tool.Out {
	if fi, err := os.Stat(c.Str("brief-dir")); err != nil || !fi.IsDir() {
		return refuse("add", fmt.Sprintf("--brief-dir %q is not a directory", c.Str("brief-dir")))
	}
	r, o := loadChecked(c, "add")
	if o != nil {
		return o
	}
	added, err := r.Add(c.Str("stream"), c.Str("brief-dir"))
	if err != nil {
		return refuse("add", err.Error()+"; nothing written")
	}
	if o := save(c, "add", r); o != nil {
		return o
	}
	return tool.Done().Fact("file", c.Str("file")).Fact("stream", c.Str("stream")).Fact("added", added).Fact("total", total(r))
}

func roadmapRemove(c *tool.Call) *tool.Out {
	r, o := loadChecked(c, "remove")
	if o != nil {
		return o
	}
	removed, err := r.Remove(ids(c)...)
	if err != nil {
		return refuse("remove", err.Error()+"; nothing written")
	}
	if o := save(c, "remove", r); o != nil {
		return o
	}
	return tool.Done().Fact("file", c.Str("file")).Fact("removed", removed).Fact("remaining", total(r))
}

func roadmapPull(c *tool.Call) *tool.Out {
	r, o := loadChecked(c, "pull")
	if o != nil {
		return o
	}
	// The edit is made and checked on a copy before any brief is written.
	probe, err := roadmap.LoadBytes(r.Bytes())
	if err != nil {
		return refuse("pull", err.Error())
	}
	if _, err := probe.Remove(ids(c)...); err != nil {
		return refuse("pull", err.Error()+"; nothing written")
	}
	if problems := roadmap.CheckBytes(probe.Bytes()); len(problems) > 0 {
		return refuse("pull", fmt.Sprintf("the edit would leave %s failing check (%s); nothing written", c.Str("file"), problems[0]))
	}
	pulled, err := r.Pull(c.Str("out"), ids(c)...)
	if err != nil {
		return refuse("pull", err.Error())
	}
	if o := save(c, "pull", r); o != nil {
		return o
	}
	out := tool.Done().Fact("file", c.Str("file")).Fact("pulled", len(pulled)).Fact("out", c.Str("out")).Fact("remaining", total(r))
	for _, card := range pulled {
		release := ""
		if card.Parent.Parent != nil {
			release = card.Parent.Parent.Version
		}
		out.Item("card", "id", card.ID, "release", release, "stream", card.Parent.Name, "tier", card.Tier)
	}
	return out
}

func roadmapNote(c *tool.Call) *tool.Out {
	r, o := loadChecked(c, "note")
	if o != nil {
		return o
	}
	noted, err := r.Note(c.Str("text"), ids(c)...)
	if err != nil {
		return refuse("note", err.Error()+"; nothing written")
	}
	if o := save(c, "note", r); o != nil {
		return o
	}
	return tool.Done().Fact("file", c.Str("file")).Fact("noted", noted)
}
