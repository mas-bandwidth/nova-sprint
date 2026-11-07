package pgbin

import (
	"errors"
	"path"
	"testing"
)

// fakeFS answers the lookups from a set of files, as filepath.Glob would for
// the patterns the package asks (one * per pattern, in a path element).
type fakeFS struct{ files []string }

func (f fakeFS) glob(pattern string) ([]string, error) {
	var out []string
	for _, p := range f.files {
		ok, err := path.Match(pattern, p)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, p)
		}
	}
	// A directory pattern matches the directory of a file below it.
	if len(out) == 0 {
		seen := map[string]bool{}
		for _, p := range f.files {
			d := path.Dir(p)
			if ok, _ := path.Match(pattern, d); ok && !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	return out, nil
}

func lookup(on string) func(string) (string, error) {
	return func(name string) (string, error) {
		if on == "" {
			return "", errors.New("not found")
		}
		return on + "/" + name, nil
	}
}

func TestFindOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		path  string
		files []string
		want  string
		found bool
	}{
		{"PATH first", "/opt/pg/bin", []string{"/usr/lib/postgresql/17/bin/initdb"}, "/opt/pg/bin", true},
		{"highest Debian version", "", []string{"/usr/lib/postgresql/9/bin/initdb", "/usr/lib/postgresql/17/bin/initdb", "/usr/lib/postgresql/16/bin/initdb"}, "/usr/lib/postgresql/17/bin", true},
		{"Debian before Homebrew", "", []string{"/opt/homebrew/opt/postgresql@18/bin/initdb", "/usr/lib/postgresql/16/bin/initdb"}, "/usr/lib/postgresql/16/bin", true},
		{"Homebrew highest version", "", []string{"/opt/homebrew/opt/postgresql@16/bin/initdb", "/opt/homebrew/opt/postgresql@18/bin/initdb"}, "/opt/homebrew/opt/postgresql@18/bin", true},
		{"Homebrew Apple before Intel", "", []string{"/usr/local/opt/postgresql@18/bin/initdb", "/opt/homebrew/opt/postgresql@16/bin/initdb"}, "/opt/homebrew/opt/postgresql@16/bin", true},
		{"Intel Homebrew", "", []string{"/usr/local/opt/postgresql@17/bin/initdb"}, "/usr/local/opt/postgresql@17/bin", true},
		{"directory without initdb is passed over", "", []string{"/usr/lib/postgresql/17/bin/psql", "/usr/lib/postgresql/16/bin/initdb"}, "/usr/lib/postgresql/16/bin", true},
		{"none", "", []string{"/usr/bin/psql"}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, ok := Find(lookup(c.path), fakeFS{c.files}.glob)
			if got != c.want || ok != c.found {
				t.Fatalf("Find = %q, %v; want %q, %v", got, ok, c.want, c.found)
			}
		})
	}
}
