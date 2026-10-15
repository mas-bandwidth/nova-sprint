package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/friend"
	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
)

// friendCardsAnswer is friend cards <friend> --json, read as her daemon reads it.
func friendCardsAnswer(t *testing.T, ta *testApp, who string) []friend.HeldCard {
	t.Helper()
	var a friend.HeldAnswer
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(ta.ok("friend cards "+who+" --json"))), &a))
	require.Equal(t, who, a.Friend, "the answer is the asked friend's")
	return a.Cards
}

// The server serves every card held on a friend's row with its packet (the card
// daemon-writes-every-taken-card3): the cards the deal put there in batch mode, working and
// ready, and a card taken back from one friend and dealt to another; on a twin store, through
// the verb, the worker's POST and GET /api/friend/<friend>/cards. The daemon that writes her
// inbox from this answer is nova-tools' nova-friend run (pkg/friend there).
func TestFriendCardsServesEveryHeldCardWithItsPacket(t *testing.T) {
	t.Parallel()
	ta, _ := takeApp(t, 20, nil, "friend-a", "friend-b")
	ta.ok("friend down friend-b")
	ta.ok("tick")
	ta.startFriend("friend-a", 8) // she starts what her width holds; the rest wait ready

	// every card on her row, working then the ready ones dealt behind them in batch mode
	held := friendCardsAnswer(t, ta, "friend-a")
	require.NotEmpty(t, held)
	cols := map[string]int{}
	for _, h := range held {
		cols[h.Col]++
		var c cardView
		ta.json("card "+strings.TrimSuffix(h.Card, ".w1"), &c)
		require.Len(t, c.Work, 1, h.Card)
		w := c.Work[0]
		assert.Equal(t, sprint.FriendRow("friend-a"), w.Row, h.Card)
		assert.Equal(t, string(w.Col), h.Col, h.Card)
		assert.Equal(t, "sprint/"+h.Card+".g1.e0", h.Branch, "the branch its brief names, a ready card's too")
		assert.Equal(t, h.Card, h.Job, "an epoch-0 card at its first generation is its own job")
		assert.Equal(t, "work", h.Kind)
		assert.Equal(t, "flash", h.Tier, "a brief that names no tier is the dealer's default")
		assert.Equal(t, 1, h.Attempt)
		assert.True(t, strings.HasPrefix(h.Brief, "STATUS: nova-sprint card "+h.Card+", epoch 0, attempt 1; push your work to the branch "+h.Branch+";"), h.Brief)
	}
	assert.Positive(t, cols["working"], "her working cards: %v", cols)
	assert.Positive(t, cols["ready"], "and the ones dealt ahead of her width in batch mode: %v", cols)
	assert.Contains(t, ta.ok("friend cards friend-a"), "FRIEND-CARDS OK friend=friend-a cards=")

	// a card taken back from her by the coordinator and dealt to the other friend
	taken := held[0]
	ta.ok("friend up friend-b")
	ta.ok("friend beat friend-b")
	ta.ok("friend take friend-a " + strings.TrimSuffix(taken.Card, ".w1") + " --working --reason 'the other friend has room'")
	ta.ok("tick")
	ta.ok("tick")
	heldA := friendCardsAnswer(t, ta, "friend-a")
	assert.False(t, slices.ContainsFunc(heldA, func(h friend.HeldCard) bool { return h.Card == taken.Card }), "a card taken back is not hers")
	heldB := friendCardsAnswer(t, ta, "friend-b")
	at := slices.IndexFunc(heldB, func(h friend.HeldCard) bool { return h.Card == taken.Card })
	require.GreaterOrEqual(t, at, 0, "dealt to the other friend: %+v", heldB)
	moved := heldB[at]
	assert.Equal(t, 3, moved.Gen)
	assert.Equal(t, taken.Card+".g3", moved.Job, "a card dealt again is a new job")
	assert.Contains(t, moved.Brief, "push your work to the branch "+moved.Branch)

	// the server answers it to the friend herself: the worker's POST and the GET
	ta.a.serveAddr = "mem:0"
	want := ta.ok("friend cards friend-b --json")
	res := ta.a.serveCtx(context.Background(), sprintwire.Request{Verbs: [][]string{{"friend", "cards", "friend-b", "--json"}}}, false)
	require.Equal(t, 0, res.Results[0].Code, res.Results[0].Stderr)
	assert.Equal(t, want, res.Results[0].Stdout)
	get := func(method, target string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		ta.a.ServeHTTP(w, httptest.NewRequest(method, target, nil))
		return w
	}
	w := get(http.MethodGet, "/api/friend/friend-b/cards")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, want, w.Body.String())
	var a friend.HeldAnswer
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &a))
	assert.Equal(t, "friend-b", a.Friend)
	for target, code := range map[string]int{
		"/api/friend/nobody/cards":  http.StatusNotFound,
		"/api/friend/no+body/cards": http.StatusBadRequest,
		"/api/friend/friend-b":      http.StatusNotFound,
		"/api/friend/friend-b/cmds": http.StatusNotFound,
	} {
		assert.Equal(t, code, get(http.MethodGet, target).Code, target)
	}
	assert.Equal(t, http.StatusMethodNotAllowed, get(http.MethodPost, "/api/friend/friend-b/cards").Code)

	// a worker's friend cards is its name and --json at most: no store, no actor, no list
	for _, argv := range [][]string{
		{"friend", "cards"},
		{"friend", "cards", "friend-a,friend-b"},
		{"friend", "cards", "friend-b", "--redis", "mem:/tmp/other.twin"},
		{"friend", "cards", "friend-b", "--actor", "coordinator"},
		{"friend", "cards", "friend-b", "--json", "--json"},
	} {
		res := ta.a.serveCtx(context.Background(), sprintwire.Request{Verbs: [][]string{argv}}, false)
		assert.Equal(t, 2, res.Results[0].Code, "%v", argv)
		assert.Contains(t, res.Results[0].Stderr, "nothing was changed", "%v", argv)
	}
	// and a friend not on the friends table is refused, exit 1
	code, _, errs := ta.do("friend cards nobody")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "no friend nobody on the friends table")
	ta.clean()
}
