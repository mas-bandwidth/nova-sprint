// Temporary: re-import the v1 sprint's live cards into a fresh v2 store at the final head
// (streams with Mode auto and Forge github; cards waiting). Copy into cmd/ of the clone to run.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

var twin = regexp.MustCompile(`~\d+$`)

func field(brief, name string) string {
	for _, l := range strings.Split(brief, "\n") {
		if strings.HasPrefix(l, name+":") {
			return strings.TrimSpace(strings.TrimPrefix(l, name+":"))
		}
	}
	return ""
}

func list(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		x = twin.ReplaceAllString(strings.TrimSpace(x), "")
		if x != "" && x != "-" && x != "none" {
			out = append(out, x)
		}
	}
	return out
}

func main() {
	raw, err := os.ReadFile(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var dump map[string]map[string]string
	if err := json.Unmarshal(raw, &dump); err != nil {
		fmt.Fprintln(os.Stderr, "json:", err)
		os.Exit(2)
	}
	live := map[string]map[string]string{}
	for k, v := range dump {
		if v["epoch"] == "2" && v["dropped_at"] == "" {
			live[twin.ReplaceAllString(strings.TrimPrefix(k, "sprint:w:"), "")] = v
		}
	}
	streams := map[string][2]string{}
	var order []string
	for _, v := range live {
		s := v["stream"]
		if _, ok := streams[s]; !ok {
			streams[s] = [2]string{"", ""}
			order = append(order, s)
		}
		if repo := field(v["brief"], "REPO"); repo != "" && streams[s][0] == "" {
			streams[s] = [2]string{repo, field(v["brief"], "BASE")}
		}
	}
	sort.Strings(order)
	var writes []store.Write
	for i, s := range order {
		rb := streams[s]
		writes = append(writes, store.PutStream{Name: s, Repo: rb[0], Base: rb[1], Pos: i + 1, Mode: "auto", Forge: "github"})
	}
	ids := make([]string, 0, len(live))
	for id := range live {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	placed := map[string]bool{}
	var ordered []string
	for len(ordered) < len(ids) {
		progress := false
		for _, id := range ids {
			if placed[id] {
				continue
			}
			ready := true
			for _, n := range append(list(live[id]["needs"]), list(live[id]["land_after"])...) {
				if _, ok := live[n]; ok && !placed[n] {
					ready = false
				}
			}
			if ready {
				placed[id] = true
				ordered = append(ordered, id)
				progress = true
			}
		}
		if !progress {
			cut := false
			for _, id := range ids {
				if placed[id] || live[id]["land_after"] == "" || cut {
					continue
				}
				fmt.Printf("CYCLE: dropping after %q of %s\n", live[id]["land_after"], id)
				live[id]["land_after"] = ""
				cut = true
			}
			if !cut {
				fmt.Fprintln(os.Stderr, "needs cycle among the unplaced cards")
				os.Exit(2)
			}
		}
	}
	dropped := 0
	for _, id := range ordered {
		v := live[id]
		kind := "producer"
		if v["kind"] == "sentinel" {
			kind = "sentinel"
		}
		var keep []string
		for _, a := range list(v["land_after"]) {
			if _, ok := live[a]; ok {
				keep = append(keep, a)
			} else {
				dropped++
			}
		}
		tier := v["tier_now"]
		if tier == "" {
			if m := regexp.MustCompile(`tier: *(\w+)`).FindStringSubmatch(v["brief"]); m != nil {
				tier = m[1]
			}
		}
		if tier == "" {
			tier = "pro"
		}
		if kind == "sentinel" {
			writes = append(writes, store.PutCard{ID: id, Kind: kind, Stream: v["stream"], Needs: list(v["needs"]), After: keep, Tier: "pro"})
			continue
		}
		writes = append(writes, store.PutCard{ID: id, Kind: kind, Stream: v["stream"], Needs: list(v["needs"]), After: keep, Brief: v["brief"], Tier: tier, Paths: list(field(v["brief"], "PATHS")), Prose: v["brief"]})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	s, err := store.Open(ctx, os.Args[1], os.Getenv("NOVA_PG_PASSWORD"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(2)
	}
	defer s.Close()
	snap, err := s.Snapshot(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snapshot:", err)
		os.Exit(2)
	}
	t0 := time.Now()
	r, err := s.Apply(ctx, store.Op{ID: "import-v1-" + t0.UTC().Format("20060102T150405Z"), Epoch: snap.Epoch, Actor: "rowan", Kind: "import", At: t0, Writes: writes, Note: "the v1 sprint's live cards and dependencies, all waiting"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "apply:", err)
		os.Exit(1)
	}
	fmt.Printf("IMPORT accepted=%v reason=%q streams=%d cards=%d after-dropped=%d seq=%d apply=%s\n", r.Accepted, r.Reason, len(order), len(ordered), dropped, r.Seq, time.Since(t0))
}
