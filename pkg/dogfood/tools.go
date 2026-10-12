package dogfood

import (
	"context"
)

// exeSuffix is the extension a built binary carries on Windows. It IS the
// executable bit there: Windows has no mode bits to speak of — every file in a
// directory reads back as 0666 — and what makes a file runnable is its
// extension. A discovery written for one platform's answer finds nothing on the
// other, which is exactly what happened: the Windows legs read a directory of
// real binaries as empty, because `nova-check.exe` is neither spelled
// `nova-check` nor executable by a bit that does not exist there.
const exeSuffix = ".exe"

type HelpRunner func(ctx context.Context, bin string) (string, error)

// RunHelp is the real one: `<bin> help`, under the context's deadline, stdout
// and stderr together, because a tool that prints its usage to stderr is still
// telling you its verbs and the parser ignores every line that is not a
