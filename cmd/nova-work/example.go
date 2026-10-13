package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/nova-sprint/pkg/tool"
)

// exampleFixture is the recorded GraphQL conversation example --dir writes:
// one repository with no issues, read scoped (repoDoc then one issues page
// at the default page size of 50). import --fixture replays it with no gh
// login and no network (workgh.Replay).
const exampleCall01 = `{
 "vars": {"owner": "acme", "repo": "widgets"},
 "reply": {"data": {"rateLimit": {"cost": 1, "remaining": 4999, "resetAt": "2026-10-02T12:30:00Z"},
  "repository": {"nameWithOwner": "acme/widgets", "url": "https://github.com/acme/widgets", "isArchived": false, "issues": {"totalCount": 0}}}}
}
`

const exampleCall02 = `{
 "vars": {"owner": "acme", "repo": "widgets", "n": 50},
 "reply": {"data": {"rateLimit": {"cost": 1, "remaining": 4998, "resetAt": "2026-10-02T12:30:00Z"},
  "repository": {"issues": {"totalCount": 0, "pageInfo": {"hasNextPage": false, "endCursor": null}, "nodes": []}}}}
}
`

// example writes the offline first-run fixture: a pair of identical tree
// files (verify --tree tree.lisp --against tree-again.lisp proves them with
// no network) and the recorded GraphQL pages import --fixture replays.
func example(c *tool.Call) *tool.Out {
	dir := c.Str("dir")
	files := []struct{ name, body string }{
		{"tree.lisp", minimalTree + "\n"},
		{"tree-again.lisp", minimalTree + "\n"},
		{filepath.Join("calls", "call-01.json"), exampleCall01},
		{filepath.Join("calls", "call-02.json"), exampleCall02},
	}
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return tool.Refuse(fmt.Sprintf("example: %v", err))
		}
		if err := os.WriteFile(p, []byte(f.body), 0o600); err != nil {
			return tool.Refuse(fmt.Sprintf("example: %v", err))
		}
	}
	o := tool.Done().Fact("dir", dir)
	for _, f := range files {
		o.Item("wrote", "file", f.name)
	}
	return o
}
