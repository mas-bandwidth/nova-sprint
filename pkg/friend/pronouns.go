package friend

import (
	"github.com/mas-bandwidth/nova-sprint/pkg/pronouns"
)

type Pronouns = pronouns.Pronouns

const (
	SheHer          = pronouns.SheHer
	HeHim           = pronouns.HeHim
	TheyThem        = pronouns.TheyThem
	DefaultPronouns = pronouns.DefaultPronouns
)

var AllPronouns = pronouns.AllPronouns

func ParsePronouns(s string) (Pronouns, bool) { return pronouns.ParsePronouns(s) }
func PronounList() string                     { return pronouns.PronounList() }
