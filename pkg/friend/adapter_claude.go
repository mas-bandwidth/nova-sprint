package friend

// ClaudeTrim is the trimmed call of a headless Claude Code run (the owner's
// finding of 2026-10-04: no MCP servers, no slash commands, no browser, six
// tools, which cut the context of each call from about 50k tokens to 12.7k).
// --tools takes every argument after it, so it is last, after the prompt.
var ClaudeTrim = []string{"--strict-mcp-config", "--disable-slash-commands", "--no-chrome", "--tools", "Bash", "Read", "Write", "Edit", "Grep", "Glob"}
