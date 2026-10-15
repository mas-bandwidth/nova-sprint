package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// cmdStats is the epoch's numbers since the last stats tidy or stats reset, the later
// (sprint.StatsSince; the whole epoch before the first): the stages, each member's work, each reader's reads and each
// route's takes, in seconds, from one read of the work, fleet and readers tables (the
// primaries' work and read cards, retired ones too, read with them in read sets, never a
// card at a time). --routes is the route table from the log over --since, by default
// from the last tidy of the routes.
func (a *app) cmdStats(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("stats")
	routes := fs.Bool("routes", false, "the route table from the log over --since, instead of the live tables")
	children := fs.Bool("children", false, "the per-member histogram of child exit codes for the last hour, from the attempt records on the epoch's cards (each take and read run's record; a member's environment exits, ended=environment exit=<n>, by code), so the seat reads them without ssh")
	since := fs.String("since", "", "with --routes, the window start: a duration back from now (10m) or an RFC 3339 time; the last stats tidy of the routes when not given")
	if pos, err := parse(fs, args); err != nil || len(pos) > 0 {
		return refuse(stderr, "stats", argErr("takes no words ", err, pos...))
	}
	if !*routes && *since != "" {
		return refuse(stderr, "stats", "--routes and --since are one window: the route table from the log")
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "stats", err.Error())
	}
	if *children {
		return a.cmdStatsChildren(st, stdout, stderr)
	}
	if *routes {
		var from time.Time
		if *since == "" {
			if from, err = st.StatsSince(context.Background(), sprint.TidyRoutes); err != nil {
				return a.readFailed("stats", err, stderr)
			}
			if from.IsZero() {
				return refuse(stderr, "stats", "--routes wants --since: no stats tidy of the routes gives the window")
			}
		} else if d, err := time.ParseDuration(*since); err == nil {
			from = a.now().Add(-d)
		} else if t, err := time.Parse(time.RFC3339, *since); err == nil {
			from = t
		} else {
			return refuse(stderr, "stats", "--since wants a duration back from now (10m) or an RFC 3339 time, found "+*since)
		}
		lines, err := st.Log(context.Background())
		if err != nil {
			return a.readFailed("stats", err, stderr)
		}
		fmt.Fprint(stdout, sprint.RouteTable(lines, from))
		return 0
	}
	s, err := st.Load(context.Background(), []string{sprint.Work, sprint.Fleet, sprint.Readers}, sprint.StatsRecords)
	if err != nil {
		return a.readFailed("stats", err, stderr)
	}
	from, err := st.StatsSince(context.Background(), "")
	if err != nil {
		return a.readFailed("stats", err, stderr)
	}
	ps := sprint.StatsSince(s, from)
	if c.json {
		b, _ := json.Marshal(ps)
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	sinceWord := ""
	if !from.IsZero() {
		by := "stats tidy"
		if m, err := st.StatsReset(context.Background()); err == nil && m != nil && m.At.Equal(from) {
			by = "stats reset"
		}
		fmt.Fprintf(stdout, "since %s (%s)\n", from.UTC().Format(time.RFC3339), by)
		sinceWord = " since=" + from.UTC().Format(time.RFC3339)
	}
	fmt.Fprint(stdout, statsText(ps))
	fmt.Fprintf(stdout, "STATS OK epoch=%d primaries=%d members=%d readers=%d routes=%d%s\n", ps.Epoch, ps.Primaries, len(ps.Work), len(ps.Reads), len(ps.Routes), sinceWord)
	return 0
}

// cmdStatsChildren prints the per-member histogram of child exit codes for the last hour,
// from the attempt records on the epoch's cards (docs/SPEC-SPRINT.md, "An environment
// exit"; sprint.EnvExitChildren), so the seat reads the fleet's environment-exit rate
// without ssh: one line per member with a child that ended in the window, `CHILD <member>
// children=<n> env=<k> rate=<pct>% exit_<code>=<count>... other=<n-k> status=<doctor's>
// last: <first line of the last harness tail>`, where each exit_<code> is the environment
// exits at that code and other is the children that ended with a verdict or another end of
// the card's own.
func (a *app) cmdStatsChildren(st *store.Store, stdout, stderr io.Writer) int {
	read, err := st.DoctorRead(context.Background())
	if err != nil {
		return a.readFailed("stats", err, stderr)
	}
	counts := sprint.EnvExitChildren(read.Snap, a.now(), time.Hour)
	if len(counts) == 0 {
		fmt.Fprintln(stdout, "CHILDREN none in the last hour")
		return 0
	}
	for _, c := range counts {
		words := []string{"children=" + strconv.Itoa(c.N), "env=" + strconv.Itoa(c.Env), "rate=" + strconv.Itoa(c.Env*100/c.N) + "%"}
		for _, code := range slices.Sorted(maps.Keys(c.Exits)) {
			words = append(words, "exit_"+code+"="+strconv.Itoa(c.Exits[code]))
		}
		words = append(words, "other="+strconv.Itoa(c.N-c.Env), "status="+sprint.EnvExitStatus(c.Env, c.N))
		last := c.Last
		if last == "" {
			last = "-"
		}
		fmt.Fprintf(stdout, "CHILD %s %s last: %s\n", c.Member, strings.Join(words, " "), oneline.Escape(last))
	}
	return 0
}

// statsText is the four tables, seconds as "median max n=<count>".
func statsText(ps sprint.PassStats) string {
	count := func(name string) ntable.Column {
		return ntable.Column{Name: name, Projection: ntable.Text, Fold: ntable.Sum}
	}
	secs := func(name string) ntable.Column {
		return ntable.Column{Name: name, Label: name + " (median max n)", Projection: ntable.Text, Fold: ntable.None}
	}
	row := func(key string, texts ...string) ntable.Row {
		r := ntable.Row{Key: key, Texts: map[string]string{}}
		for i := 0; i+1 < len(texts); i += 2 {
			r.Texts[texts[i]] = texts[i+1]
		}
		return r
	}
	n := strconv.Itoa
	stages := ntable.Table{Columns: []ntable.Column{secs("seconds")}, Rows: []ntable.Row{
		row("deal wait", "seconds", measureText(ps.Stages.DealWait)),
		row("finish to two reads", "seconds", measureText(ps.Stages.FinishToReads)),
		row("accept to land", "seconds", measureText(ps.Stages.AcceptToLand)),
		row("total", "seconds", measureText(ps.Stages.Total)),
	}}
	work := ntable.Table{Columns: []ntable.Column{count("cards"), count("failed"), secs("take wait"), secs("run wall"), secs("report lag")}}
	for _, m := range ps.Work {
		work.Rows = append(work.Rows, row(m.Member, "cards", n(m.Cards), "failed", n(m.Failed),
			"take wait", measureText(m.TakeWait), "run wall", measureText(m.RunWall), "report lag", measureText(m.ReportLag)))
	}
	reads := ntable.Table{Columns: []ntable.Column{count("cards"), secs("begin wait"), secs("run wall"), secs("report lag")}}
	for _, r := range ps.Reads {
		reads.Rows = append(reads.Rows, row(r.Reader, "cards", n(r.Cards),
			"begin wait", measureText(r.BeginWait), "run wall", measureText(r.RunWall), "report lag", measureText(r.ReportLag)))
	}
	routes := ntable.Table{Columns: []ntable.Column{count("takes"), count("ok"), count("failed"), count("provider"), secs("run wall")}}
	for _, r := range ps.Routes {
		routes.Rows = append(routes.Rows, row(r.Route, "takes", n(r.Takes), "ok", n(r.OK), "failed", n(r.Failed), "provider", n(r.Provider),
			"run wall", measureText(r.RunWall)))
	}
	return ntable.Render(stages, ntable.RenderOpts{Title: "stages"}) + statsLegend["stages"] + "\n" +
		ntable.Render(work, ntable.RenderOpts{Title: "work"}) + statsLegend["work"] + "\n" +
		ntable.Render(reads, ntable.RenderOpts{Title: "reads"}) + statsLegend["reads"] + "\n" +
		ntable.Render(routes, ntable.RenderOpts{Title: "routes"}) + statsLegend["routes"] + "\n"
}

// statsLegend is the line under each stats table saying what its columns
// count (sprint.Stats): a reader meets the tables cold.
var statsLegend = map[string]string{
	"stages": "stages: per primary, admitted to first dealt (deal wait), its last ok finish to accepted (finish to two reads), accepted to landed, admitted to landed (total); seconds as median, max, n primaries\n",
	"work":   "work: per member, cards is its work cards (one per attempt, counted to the member it was last dealt to), failed those finished failed; take wait is dealt to taken, run wall the child's wall from its usage (a friend's card, which reports none: the friend's take to the friend REPORT.md's time), report lag taken to finished less the run wall\n",
	"reads":  "reads: per reader, cards is the read cards asked of it (retired ones too); begin wait is asked to begun, run wall the usage's wall, report lag begun to read less the run wall\n",
	"routes": "routes: per route, takes is every take on it, work and read alike, each take of a card again counted (the cards' cost records); ok came back with its answer, provider ended by the provider or with no result, failed every other end; run wall the takes' usage walls\n",
}

// measureText is a measure as its cell prints it: the median and the max in seconds
// to a tenth, fixed width so a column's numbers line up, and the count; "-" for none.
func measureText(m sprint.Measure) string {
	if m.N == 0 {
		return "-"
	}
	return fmt.Sprintf("%7.1f %7.1f n=%d", m.Median, m.Max, m.N)
}
