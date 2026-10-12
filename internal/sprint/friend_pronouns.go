package sprint

import "github.com/mas-bandwidth/nova-sprint/pkg/pronouns"

// friendPronouns uses only an exact roster match. A missing or unrecognized
// setting takes the package default, they/them; identity is never inferred
// from a friend name.
func friendPronouns(seats []FriendSeat, name string) pronouns.Pronouns {
	for _, seat := range seats {
		if seat.Name == name {
			return parseFriendPronouns(seat.Pronouns)
		}
	}
	return pronouns.DefaultPronouns
}

func parseFriendPronouns(raw string) pronouns.Pronouns {
	p, _ := pronouns.ParsePronouns(raw)
	return p
}

func snapshotFriendPronouns(s *Snapshot, name string) pronouns.Pronouns {
	if s == nil {
		return pronouns.DefaultPronouns
	}
	if raw, ok := s.FriendTextPronouns[name]; ok {
		return parseFriendPronouns(raw)
	}
	return friendPronouns(s.Friends, name)
}
