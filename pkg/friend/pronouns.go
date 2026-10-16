package friend

import (
	toolsfriend "github.com/mas-bandwidth/nova-tools/pkg/friend"
)

type Pronouns = toolsfriend.Pronouns

const (
	SheHer          = toolsfriend.SheHer
	HeHim           = toolsfriend.HeHim
	TheyThem        = toolsfriend.TheyThem
	DefaultPronouns = toolsfriend.DefaultPronouns
)

var AllPronouns = toolsfriend.AllPronouns

func ParsePronouns(s string) (Pronouns, bool) { return toolsfriend.ParsePronouns(s) }
func PronounList() string                     { return toolsfriend.PronounList() }
