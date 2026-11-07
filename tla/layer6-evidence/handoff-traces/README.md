# Two-child setup finite trace checks

Both source-emitted traces matched the unchanged SetupHandoff protocol through
SetupHandoffTrace on reserved vision with the shared check-trace.sh, one TLC
worker, and pinned v1.8.0 jar. Raw logs were checked: no warnings or errors.

| Case | Actual records | TLC generated / distinct | Result |
| --- | ---: | ---: | --- |
| successful phase1 and phase2 | 13 | 14 / 14 | matched |
| Clear between children, prevalidation refuses | 9 | 10 / 10 | matched |

The extra state is the deterministic hidden SaveCredential substep before the
actual combined PreparedRole endpoint. All other records correspond to the
fixture's observed walker, record helper, lease callback or real child-channel
boundary. MaxCrashes is zero; these traces do not cover crash/retry, record reuse,
competing up, lost PG sessions, real secret infrastructure or production role
readiness. The fixture's lease/role/schema/secrets dependencies are injected.
The test composes two real runUpChild calls and constructs its own Engine; it
does not execute the production outer coordinator/executeUpPhase constructor.
No claim of arbitrary implementation safety, liveness or process secrecy follows
from two finite matches. See SetupHandoffTrace-MAPPING.md for the precise relation.

The emitted source is preserved in the full archive identified by
external-source.json. All 758 archived source files were independently compared
to the frozen tree and are hashed in source-files.json. The exact emitter is also
retained as text; input, model, config, runner and raw log hashes are in artifacts.json.
The success input SHA-256 is
`f7068d413c14ea320df546f0657e3a7adb61a3f6156af2ccf75ea91d299795a9`;
the stale input is
`dff8ad15d35e57b715790a66628e6df42b7d26f2e1f4e384e774123e2e4a6afa`.

Source review first found that prevalidation happened only after acquisition.
That mismatch was corrected in the implementation before these artifacts were
emitted; no Prevalidate record was fabricated and model Next was not relaxed.
The stale trace actually clears the file-backed store and verifies that the real
walker refuses before bootstrap, acquisition, or phase2 ordinary-walk activity.

To reproduce, copy checked-spec into an isolated spec directory (no TraceLog
module there), and a case directory into a separate input directory. Under an
authorized reservation run:

```
TRACE_LOG=/absolute/result.log bash /absolute/spec/check-trace.sh SetupHandoff /absolute/input
```

The original command runner is retained as run.py; its historical layout used
spec/, inputs/{success,stale}/ and evidence/. The original isolated SetupHandoff
12 cases and all earlier finite trace acceptance artifacts remain unchanged.
