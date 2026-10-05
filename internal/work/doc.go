// Package work is the design of nova-sprint's durable work tree.
// The round-trip between Redis and that tree is not built.
// NOTE.md is the design: what the tree holds, what stays only in Redis,
// and when it is written. Nothing here reads or writes a file or a store.
package work

import _ "embed"

// Unbuilt is the whole of what a work verb does until the round-trip exists.
// The refusal says this, and the verb has read nothing and written nothing.
const Unbuilt = "the work-tree round-trip is not built; nothing was read and nothing was written"

//go:embed NOTE.md
var Note string
