package main

import (
	"flag"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// verbOwnFlags is every flag of fs that the verb itself declares: every flag fs holds but
// the common ones (common, the set a bare verbSetup registers).
func verbOwnFlags(fs, common *flag.FlagSet) []string {
	var out []string
	fs.VisitAll(func(f *flag.Flag) {
		if common.Lookup(f.Name) == nil {
			out = append(out, f.Name)
		}
	})
	slices.Sort(out)
	return out
}

// setVerbFlags is the set verb's own flags, read at run time from the very FlagSet cmdSet
// builds (app.onSetFlags), so a flag added by any path (a SetReq field or a With* wrapper)
// is seen and none is missed.
func setVerbFlags(t *testing.T) []string {
	t.Helper()
	ta := newTestApp(t)
	var got *flag.FlagSet
	ta.a.onSetFlags = func(fs flagSet) { got = fs }
	ta.do("set") // cmdSet builds its flags, hands them over, then refuses: nothing to set
	require.NotNil(t, got, "cmdSet handed its flag set over")
	common, _ := ta.a.verbSetup("set")
	return verbOwnFlags(got, common)
}

// undeclaredFlags is every flag that is neither a listed non-setting nor maps to a
// property row (HomeProp or HomeCarry) of sprint.Defaults.
func undeclaredFlags(flags []string) []string {
	var out []string
	for _, fl := range flags {
		if slices.Contains(sprint.NotSettings, "set --"+fl) {
			continue
		}
		prop, ok := sprint.SetFlagProp[fl]
		if !ok {
			prop = strings.ReplaceAll(fl, "-", "_")
		}
		d, ok := sprint.DefaultOf(prop)
		if !ok || (d.Home != sprint.HomeProp && d.Home != sprint.HomeCarry) {
			out = append(out, "--"+fl+" ("+prop+")")
		}
	}
	return out
}

// TestEverySetFlagHasADeclaredDefault is the class test over the `set` verb's full flag
// list, enumerated from its real FlagSet at run time (its SetReq fields and its With*
// wrappers alike: friend-finish, pegged-bound, base): each flag maps to a Defaults row the
// clear's set-defaults step writes or carries, or is a listed non-setting. Reversed
// witnesses: the enumeration, fed a fake flag set with one flag more, names it; a new flag
// with no row is red.
func TestEverySetFlagHasADeclaredDefault(t *testing.T) {
	t.Parallel()
	flags := setVerbFlags(t)
	for _, want := range []string{"friend-finish", "pegged-bound", "base", "read-cards", "alarm-ready", "unserved-tier-falls-to"} {
		require.Contains(t, flags, want, "the wrapper flags are read too")
	}
	assert.Empty(t, undeclaredFlags(flags), "a set flag with no declared default")

	// the enumeration's witness: a fake verb flag set, the common flags plus two of its own
	common := flag.NewFlagSet("set", flag.ContinueOnError)
	common.String("actor", "", "")
	fake := flag.NewFlagSet("set", flag.ContinueOnError)
	fake.String("actor", "", "")
	fake.String("read-cards", "", "")
	fake.String("new-knob", "", "")
	assert.Equal(t, []string{"new-knob", "read-cards"}, verbOwnFlags(fake, common), "the enumeration reads every own flag and no common one")
	assert.Equal(t, []string{"--new-knob (new_knob)"}, undeclaredFlags(verbOwnFlags(fake, common)), "the witness is red")
}
