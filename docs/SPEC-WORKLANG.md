# Reading work-tree files

Work-tree files look a little like Lisp, but they are data. The reader never
runs them as a program. `internal/worklang` accepts five kinds of value and
enforces size, nesting, and node limits while reading.

You only need this reference if you are writing a parser integration or
working on the file format. For everyday import and verification, use
[the command guide](CLI.md#nova-work).

`internal/workfile` calls `worklang.Read(file, data, limits)` and then checks
that the returned `Form` has the record shape defined in
[SPEC-WORK-V1.md](SPEC-WORK-V1.md). The parser handles the grammar; the caller
handles what each record means.

## 1. The reader

`worklang.Read(file, data, limits)` reads exactly one form from `data`. It
accepts exactly these five kinds of form:

| form | written | held as |
| --- | --- | --- |
| list | `( ... )`, possibly empty | `List`, its members in order |
| keyword | `:name` | `Keyword`, the name without the colon |
| string | `"..."` | `String`, decoded; a backslash takes the next byte literally |
| integer | decimal digits, optionally signed with `+` or `-` | `Integer` |
| symbol | any other bare token, such as `go-fix` or `false` | `Symbol`, verbatim |

A `;` starts a comment that runs to the end of its line. Comments and whitespace are text,
never syntax. A keyword that is empty or holds a second `:` is refused as a forbidden token
at its byte. **Every token that starts with a digit, `+` or `-` goes to the integer reader**, so
it must be an integer: a sign with no digits after it (`-x`, a bare `+`) or digits followed
directly by a non-boundary byte (`12abc`) is refused as a forbidden token at its byte, never read
as a symbol.

**Nothing is evaluated.** Before parsing, a lexical pass rejects syntax that
could be confused with evaluation or escape mechanisms, at its byte offset:
`#` (a dispatch macro such as `#.`), `|`,
`'`, `` ` ``, `,` and `\`. Inside a string or a comment the same bytes are text.

**Three bounds, all enforced while reading.** `Limits` carries `MaxBytes`, `MaxDepth` and
`MaxNodes`; the names are the flags a caller exposes (`--max-bytes`, `--max-depth`,
`--max-nodes`). The tree file reader sets depth 16 and lets nodes equal bytes (`workfile.Limits(maxBytes)`). A file longer than
`MaxBytes` is refused before a byte of it is parsed. A list nested past `MaxDepth` is refused
at its opening byte. Every atom (keyword, string, integer, symbol) counts as one node, and the
atom past `MaxNodes` is refused at its byte. A limits value with any bound at zero or below is
refused rather than guessed. Past a bound, the input is refused whole and never truncated.

**One form per file.** Bytes after the first form, an unbalanced list and an unterminated
string are each refused naming the byte.

**Refusals.** Every refusal is a `*worklang.Refusal` whose `ExitCode()` is 2 and whose message is
`plan file=<file>: <reason>`. The reason names the byte or the bound it refuses.

**Byte ranges.** A form carries `Offset` (its first byte), and a list, string, keyword or
integer also carries `End` (the byte just past it). A caller names a byte in its own refusals
from `Offset`.
