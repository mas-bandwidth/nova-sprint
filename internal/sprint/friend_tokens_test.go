package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFriendTokensSumPerFriendFromTheResultUsage is the friends table's tokens
// column from its source: each friend card's usage record (as her finish's
// --usage carries it), summed over her cards' `usage` fields. The unit is no
// store and no clock.
func TestFriendTokensSumPerFriendFromTheResultUsage(t *testing.T) {
	t.Parallel()

	// two of one friend's cards sum, each card's usage field read once
	cards := []*Card{
		{Fields: map[string]string{FieldUsage: "input=1000 output=2000 cache_read=300"}},
		{Fields: map[string]string{FieldUsage: "input=400 output=500 cache_read=400 cache_write=100"}},
	}
	assert.Equal(t, int64(4700), FriendTokensFromCards(cards), "1000+2000+300 and 400+500+400+100")
	assert.Equal(t, "4.7K", CompactTokens(FriendTokensFromCards(cards)))

	// the cell: a subscription friend reads the compact count, an api-billed friend
	// the dollars her records charged rounded up to the cent
	assert.Equal(t, "1.2M", FriendTokensCell(1_200_000, "", ""), "no billing is a subscription")
	assert.Equal(t, "1.2M", FriendTokensCell(1_200_000, "9.99", "subscription"))
	assert.Equal(t, "$1.24", FriendTokensCell(1_200_000, "1.231", "api"))
	assert.Equal(t, "$0.01", FriendTokensCell(0, "0.001", "metered"))
	assert.Equal(t, "0", FriendTokensCell(0, "", ""))
	assert.Equal(t, "1.3M", CompactTokens(1_250_000), "one decimal, half up")
}
