package roster

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// FriendPrefix is the name prefix of a friend's sprint settings in the
// store: one data version named friend:<name> per change (nova-sprint
// friend set), the newest in force. The policy table holds them because
// they are global, as the policy is (a clear keeps them), every change is
// logged with its actor and time, and the newest of each is read by name
// without the log (store.NewestData).
const FriendPrefix = "friend:"

// FriendSetting is a friend's sprint settings: how many cards it works at
// once and the tiers it serves.
type FriendSetting struct {
	Width int      `json:"width"`
	Tiers []string `json:"tiers"`
}

// DefaultFriend is a friend with nothing recorded: width 1, every tier
// (nova-config's friend defaults before the settings moved here).
func DefaultFriend() FriendSetting {
	return FriendSetting{Width: 1, Tiers: slices.Clone(store.Tiers)}
}

// Problem is why s is no friend's settings ("" when it is): a width below 1,
// no tier, or a tier not in store.Tiers.
func (s FriendSetting) Problem() string {
	switch i := slices.IndexFunc(s.Tiers, func(t string) bool { return !slices.Contains(store.Tiers, t) }); {
	case s.Width < 1:
		return fmt.Sprintf("width %d is below 1: a friend works at least one card at once", s.Width)
	case len(s.Tiers) == 0:
		return "it serves no tier: name one or more of " + strings.Join(store.Tiers, ",")
	case i >= 0:
		return fmt.Sprintf("tier %q is not one of %s", s.Tiers[i], strings.Join(store.Tiers, ","))
	}
	return ""
}

// Body is s as its data version holds it: JSON, the tiers in store.Tiers'
// order, each once.
func (s FriendSetting) Body() string {
	s.Tiers = store.SplitTiers(store.JoinTiers(s.Tiers))
	b, _ := json.Marshal(s) // an int and strings: never fails
	return string(b)
}

// ParseFriend reads a friend:<name> data version's body, refusing one that
// is not a FriendSetting with no Problem.
func ParseFriend(body string) (FriendSetting, error) {
	var s FriendSetting
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return FriendSetting{}, fmt.Errorf("its body is not {\"width\":<n>,\"tiers\":[...]}: %w", err)
	}
	if p := s.Problem(); p != "" {
		return FriendSetting{}, errors.New(p)
	}
	return s, nil
}

// FriendSettings is the friends' settings from the newest friend:<name>
// data versions (store.NewestData(FriendPrefix)), by friend name: each the
// parsed setting, or why it cannot be read.
func FriendSettings(versions []store.PolicyVersion) map[string]FriendRecord {
	out := map[string]FriendRecord{}
	for _, v := range versions {
		name, ok := strings.CutPrefix(v.Name, FriendPrefix)
		if !ok || name == "" {
			continue
		}
		s, err := ParseFriend(v.Body)
		r := FriendRecord{Setting: s, Version: v.Version}
		if err != nil {
			r.Unread = fmt.Sprintf("policy v%d (%s%s): %s", v.Version, FriendPrefix, name, err)
		}
		out[name] = r
	}
	return out
}

// FriendRecord is one friend's recorded settings: the setting, the data
// version that holds it, and why it cannot be read ("" when it can).
type FriendRecord struct {
	Setting FriendSetting
	Version int
	Unread  string
}

// Settings is f with its sprint settings from recs: its record's, the
// defaults when it has none, or none and Unread when its record cannot be
// read.
func (f Friend) Settings(recs map[string]FriendRecord) Friend {
	rec, ok := recs[f.Name]
	switch {
	case !ok:
		d := DefaultFriend()
		f.Width, f.Tiers, f.Defaulted = d.Width, d.Tiers, true
	case rec.Unread != "":
		f.Width, f.Tiers, f.Unread = 0, nil, rec.Unread
	default:
		f.Width, f.Tiers = rec.Setting.Width, slices.Clone(rec.Setting.Tiers)
	}
	return f
}
