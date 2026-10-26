package cardgen

import (
	"path"
	"strings"
)

// IsTestFile says p is a test file in any language the sprint lands: a basename
// <stem>_test.<ext>, <stem>.test.<ext> or test_<rest> with a non-empty stem or rest, or
// any path with a directory segment exactly tests or __tests__. It is the one home of
// "is this path a test file" for the sprint (docs/SPEC-SPRINT.md section 7, the always
// inside PATHS rule and the scope amendment). p is a path relative to the repository
// root, cleaned as AlwaysInPaths cleans one; a path that climbs out with .. or starts at
// the filesystem root is outside the repository and is refused. A testdata/ directory
// stays AlwaysInPaths' own fixture rule; IsTestFile does not take it.
func IsTestFile(p string) bool {
	p = strings.TrimPrefix(path.Clean(p), "./")
	if p == "" || p == "." || p == ".." || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") {
		return false
	}
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if seg == "tests" || seg == "__tests__" {
			return true
		}
	}
	base := path.Base(p)
	if strings.HasPrefix(base, "test_") && len(base) > len("test_") {
		return true
	}
	if ext := path.Ext(base); ext != "" && ext != "." {
		stem := strings.TrimSuffix(base, ext)
		if strings.HasSuffix(stem, "_test") && len(stem) > len("_test") {
			return true
		}
		if strings.HasSuffix(stem, ".test") && len(stem) > len(".test") {
			return true
		}
	}
	return false
}
