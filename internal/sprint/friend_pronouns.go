package sprint

import "github.com/mas-bandwidth/nova-tools/pkg/friend"

// friendPronouns uses only an exact roster match. A missing or unrecognized
// setting takes the package default, they/them; identity is never inferred
// from a friend name.
func friendPronouns(seats []FriendSeat, name string) friend.Pronouns {
	for _, seat := range seats {
		if seat.Name == name {
			return parseFriendPronouns(seat.Pronouns)
		}
	}
	return friend.DefaultPronouns
}

func parseFriendPronouns(raw string) friend.Pronouns {
	p, _ := friend.ParsePronouns(raw)
	return p
}

func snapshotFriendPronouns(s *Snapshot, name string) friend.Pronouns {
	if s == nil {
		return friend.DefaultPronouns
	}
	if raw, ok := s.FriendTextPronouns[name]; ok {
		return parseFriendPronouns(raw)
	}
	return friendPronouns(s.Friends, name)
}
