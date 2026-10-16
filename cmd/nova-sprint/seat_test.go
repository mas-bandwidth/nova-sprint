package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The coordinator's seat moves by a verb (Glenn, 2026-10-02 5:15 PM: "We need to
// make handover MORE SMOOTH."; 5:20 PM: "it needs to be able to be done, even if
// the old coordinator is asleep or out of credits" and "you can be given
// coordinator status, or you can take it (with my permission only)";
// nova-tools#5096 item 28).

// seatSprint is a sprint whose owner is glenn and whose coordinator is the test
// app's actor, coordinator.
func seatSprint(t *testing.T) *testApp {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1,m2 --owner glenn")
	ta.ok("add --stream s1 --count 2")
	return ta
}

// holder is the sprint's coordinator as where --json carries it.
func (ta *testApp) holder() string {
	ta.t.Helper()
	var v struct {
		Coordinator string `json:"coordinator"`
	}
	ta.json("where", &v)
	return v.Coordinator
}

// The holder gives the seat: the log says so with the reason and who gave it,
// every coordinator verb then takes the new name and refuses the old one,
// where names the holder, and the leaving seat gets the handover as a receipt.
func TestTheHolderGivesTheSeat(t *testing.T) {
	t.Parallel()
	ta := seatSprint(t)
	out := ta.ok("coordinator rowan --reason 'stella holds the merges; rowan is back'")
	assert.Contains(t, out, "COORDINATOR OK holder=rowan from=coordinator by=coordinator given\n", out)
	assert.Contains(t, out, "HANDOVER seat=rowan since=", "the leaving seat's receipt is the handover:\n%s", out)
	assert.Equal(t, "rowan", ta.holder())

	log := ta.ok("log")
	assert.Contains(t, log, "seat: coordinator -> rowan: stella holds the merges; rowan is back, by coordinator\n", log)

	code, _, errs := ta.do("add --stream s2 --count 1 --one")
	assert.Equal(t, 2, code, errs)
	assert.Contains(t, errs, "the coordinator's alone: rowan, not coordinator", errs)
	ta.ok("add --stream s2 --count 1 --one --actor rowan")
	code, _, errs = ta.do("inbox --read")
	assert.Equal(t, 2, code, "the old holder moves no cursor: %s", errs)
	ta.ok("inbox --read --actor rowan")

	where := ta.ok("where")
	assert.Contains(t, where, "SPRINT TABLE  coordinator rowan\n", where)
	assert.NotContains(t, where, "taken", where)
	// inbox --json names the holder: the push through the server reads it there
	var in struct {
		Coordinator string `json:"coordinator"`
	}
	ta.json("inbox", &in)
	assert.Equal(t, "rowan", in.Coordinator)
}

// The owner gives the seat too; anyone else is refused and nothing changes; a
// seat change wants a reason, a name, and a name that is not the holder's.
func TestTheSeatIsTheHoldersOrTheOwnersToGive(t *testing.T) {
	t.Parallel()
	ta := seatSprint(t)
	for _, c := range []struct{ line, says string }{
		{"coordinator rowan --reason r --actor intruder", "the seat is the holder's to give: coordinator, not intruder"},
		{"coordinator rowan --actor coordinator", "--reason"},
		{"coordinator --reason r", "one name"},
		{"coordinator coordinator --reason r", "coordinator holds the seat already"},
		{"coordinator 'a b' --reason r", "a name wants letters, digits, _ and -"},
	} {
		code, out, errs := ta.do(c.line)
		assert.Equal(t, 2, code, "%s: %s%s", c.line, out, errs)
		assert.Contains(t, errs, c.says, "%s: %s", c.line, errs)
		assert.Equal(t, "coordinator", ta.holder(), "%s changed the seat", c.line)
	}
	assert.NotContains(t, ta.ok("log"), "seat", "a refused seat change wrote to the log")

	out := ta.ok("coordinator rowan --reason 'glenn moves the seat' --actor glenn --dry-run")
	assert.Equal(t, "COORDINATOR DRY-RUN holder=rowan from=coordinator by=glenn given; nothing was changed\n", out)
	assert.Equal(t, "coordinator", ta.holder(), "--dry-run moved the seat")
	out = ta.ok("coordinator rowan --reason 'glenn moves the seat' --actor glenn")
	assert.Contains(t, out, "COORDINATOR OK holder=rowan from=coordinator by=glenn given\n", out)
	assert.Contains(t, ta.ok("log"), "seat: coordinator -> rowan: glenn moves the seat, by glenn\n")
}

// The seat is taken by the new holder, with the owner's name: it works with the
// old holder doing nothing, the log says TAKEN loudly with the owner's name, the
// old holder has a note addressed to them in the inbox, and where says taken
// and when until the next handover is given.
func TestTheSeatIsTakenWithTheOwnersName(t *testing.T) {
	t.Parallel()
	ta := seatSprint(t)
	taken := ta.a.now().In(ta.a.zone()).Format("3:04 PM")
	out := ta.ok("coordinator rowan --take --approved-by glenn --reason 'stella is out of credits' --actor rowan")
	assert.Contains(t, out, "COORDINATOR OK holder=rowan from=coordinator by=rowan taken approved_by=glenn\n", out)
	assert.Equal(t, "rowan", ta.holder())
	assert.Contains(t, ta.ok("log"), "seat TAKEN: coordinator -> rowan, approved by glenn: stella is out of credits\n")

	var for_ *sprint.Group
	for _, g := range ta.inboxGroups() {
		if g.Type == sprint.NSeatTaken {
			g := g
			for_ = &g
		}
	}
	require.NotNil(t, for_, "no seat TAKEN note in the inbox")
	assert.Equal(t, "coordinator", for_.To, "the note is addressed to the old holder")
	assert.Contains(t, ta.ok("inbox"), "for=coordinator", "the inbox shows who it is for")

	where := ta.ok("where")
	assert.Contains(t, where, "SPRINT TABLE  coordinator rowan (taken "+taken+")\n", where)
	var v struct {
		Seat struct {
			Holder     string `json:"holder"`
			Taken      bool   `json:"taken"`
			ApprovedBy string `json:"approved_by"`
		} `json:"seat"`
	}
	ta.json("where", &v)
	assert.True(t, v.Seat.Taken)
	assert.Equal(t, "glenn", v.Seat.ApprovedBy)

	// the next handover, given, clears the mark
	ta.ok("coordinator stella --reason 'stella woke' --actor rowan")
	where = ta.ok("where")
	assert.Contains(t, where, "SPRINT TABLE  coordinator stella\n", where)
	assert.NotContains(t, where, "taken", where)
}

// A take is refused without the owner's name, with another name than the
// owner's, by anyone but the name taking it, and on a sprint that names no
// owner; nothing changes and nothing is logged.
func TestATakeWithoutTheOwnersNameIsRefused(t *testing.T) {
	t.Parallel()
	ta := seatSprint(t)
	for _, c := range []struct{ line, says string }{
		{"coordinator rowan --take --reason r --actor rowan", "--take wants --approved-by <owner>"},
		{"coordinator rowan --take --approved-by rowan --reason r --actor rowan", "the sprint's owner is glenn, not rowan"},
		{"coordinator rowan --take --approved-by glenn --reason r --actor stella", "a seat is taken by the one taking it: rowan, not stella"},
		{"coordinator rowan --take --approved-by glenn --actor rowan", "--reason"},
	} {
		code, out, errs := ta.do(c.line)
		assert.Equal(t, 2, code, "%s: %s%s", c.line, out, errs)
		assert.Contains(t, errs, c.says, "%s: %s", c.line, errs)
		assert.Equal(t, "coordinator", ta.holder(), "%s changed the seat", c.line)
	}
	assert.NotContains(t, ta.ok("log"), "seat", "a refused take wrote to the log")

	// a sprint with no owner takes no take; NOVA_SPRINT_OWNER names one
	ta2 := newTestApp(t)
	ta2.ok("init --readers reader-a,reader-b --members m1")
	code, _, errs := ta2.do("coordinator rowan --take --approved-by glenn --reason r --actor rowan")
	assert.Equal(t, 2, code, errs)
	assert.Contains(t, errs, "the sprint names no owner", errs)
	env := ta2.a.getenv
	ta2.a.getenv = func(k string) string {
		if k == "NOVA_SPRINT_OWNER" {
			return "glenn"
		}
		return env(k)
	}
	ta2.ok("coordinator rowan --take --approved-by glenn --reason r --actor rowan")
	assert.Equal(t, "rowan", ta2.holder())
}

// handover prints the store view, then the coordinator's numbered rules, one
// line each.
func TestHandoverPrintsWhatTheNextSeatNeeds(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1,m2 --owner glenn")
	ta.ok("add --stream s1 --count 1 --one")
	ta.ok("add --stream s1 --sentinel s1-stop")
	ta.ok("add --stream s1 --count 2")
	ta.ok("add --stream s2 --count 1 --one")
	// b waits on the live s2-1: a need naming a dropped card is refused at
	// add, and drop refuses a card a waiting card still needs without
	// --cascade (docs/SPEC-SPRINT.md section 11), so no blocked judgment opens.
	ta.ok("add --stream s3 b --one --needs s2-1")
	ta.ok("fleet down m2")
	// started and stopped: a STOPPED machine with moves due is a judgment (a sprint
	// never started is READY and raises none, docs/SPEC-SPRINT.md section 14)
	ta.ok("start")
	ta.ok("stop --reason r --until 1h")

	out := ta.ok("handover")
	for _, want := range []string{
		"HANDOVER seat=coordinator since=init\n",
		// the stream line counts s1's cards, never its stop sentinel (the owner, 2026-10-14)
		"STREAM s1 waiting=2 ready=1 working=0 review=0 merging=0 landed=0\n",
		"SENTINEL s1-stop stream=s1 held: 2 wait behind it (s1-",
		"MEMBER m2 held by coordinator\n",
		"DECISION ",
		" hold member m2 by coordinator: --return: its work begun is handed back now\n",
		"RULE R1. A friend is working only when its cards go from working to done; awake is not working.\n",
		"FIRST nova-sprint where\n",
		"FIRST nova-hook hook --as <you> --role coordinator, under a Monitor (the judgments come on the bus)\n",
		"FIRST read docs/SPEC-SPRINT.md, \"Handing over the seat\"\n",
		"JUDGMENT machine:stopped",
		"HANDOVER OK judgments=1 sentinels=1 members=1 decisions=1\n",
	} {
		assert.Contains(t, out, want, out)
	}
	assert.Contains(t, out, "ROUTES ", "the routes line is there, disabled or none:\n%s", out)
	assert.Equal(t, len(coordinatorRules), strings.Count(out, "\nRULE R"), "every rule of section 10, by number:\n%s", out)

	var h struct {
		Seat      struct{ Holder string } `json:"seat"`
		Judgments []inboxJudgment         `json:"judgments"`
		Sentinels []struct {
			ID     string   `json:"id"`
			Behind []string `json:"behind"`
		} `json:"sentinels"`
		Decisions []struct {
			Verb   string `json:"verb"`
			Reason string `json:"reason"`
		} `json:"decisions"`
		First []string `json:"first"`
	}
	out = ta.ok("handover --json")
	require.NoError(t, json.Unmarshal([]byte(out), &h), out)
	assert.Equal(t, "coordinator", h.Seat.Holder)
	require.Len(t, h.Judgments, 1, "the stopped machine's")
	require.Len(t, h.Sentinels, 1)
	assert.Equal(t, "s1-stop", h.Sentinels[0].ID)
	assert.Len(t, h.Sentinels[0].Behind, 2)
	require.Len(t, h.Decisions, 1)
	assert.Equal(t, "hold", h.Decisions[0].Verb)
	assert.Len(t, h.First, 3)
}

// The server's coordinator push follows the seat (v1.3.6, in place of the retired seat
// push loop): a seat change moves the judgments to the new holder's name on the bus (every
// open one is sent there once), and the note of a taken seat, addressed to the old holder,
// goes to the old holder's name.
func TestThePushFollowsTheSeat(t *testing.T) {
	t.Parallel()
	ta, _ := heldAndWaiting(t)
	getenv := ta.a.getenv
	ta.a.getenv = func(k string) string {
		if k == busRedisEnv {
			return "bus.test:6379"
		}
		return getenv(k)
	}
	ta.a.serveAddr = "mem:0"
	// a judgment addressed to the seat (the held one is a timer, addressed to its actor): it
	// follows the seat
	ta.failOnce("m1", "s2-1.w1@1", "tests red")
	failed := ta.group(sprint.NWorkFailed, "s2").ID
	ta.ok("init --owner glenn") // the owner set once, on a sprint that named none
	first := ta.pass("coordinator")
	require.NotEmpty(t, first)
	assert.Contains(t, first[0].Body, failed, "the open judgment goes to the holder")

	ta.ok("coordinator rowan --take --approved-by glenn --reason 'coordinator is asleep' --actor rowan")
	ta.ok("tick")
	next := ta.pass("rowan")
	require.NotEmpty(t, next, "the new holder hears everything open")
	assert.Contains(t, next[0].Body, failed, "the open judgment follows the seat")
	var taken string
	for _, g := range ta.inboxGroups() {
		if g.Type == sprint.NSeatTaken {
			taken = g.ID
		}
	}
	require.NotEmpty(t, taken)
	var toOld bool
	for _, m := range ta.toSeat("coordinator") {
		toOld = toOld || strings.Contains(m.Body, taken)
	}
	assert.True(t, toOld, "the taken note is the old holder's")
	for _, m := range next {
		assert.NotContains(t, m.Body, taken, "the taken note is not the new holder's")
	}
}
