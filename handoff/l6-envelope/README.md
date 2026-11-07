# Layer 6 envelope handoff

These are unintegrated, unaccepted review artifacts. They do not modify shipping packages. Apply only after reviewing against the current integration head and preserving its changes; the combined integrated gate remains open.

- `scoped.patch` is based on c4507c286740816f076435aa7e055e893bad5c6c and contains the reviewed one-envelope pusher, observer, focused tests, Clear/walk trace emitters and design wording.
- `history-owner-relative.patch` and `runtrace-owner-relative.patch` are narrow assertion changes against the integration owner's then-current uncommitted files. Their headers refer to prior worktree paths. Rebase them manually onto the current owner files, retaining the row-schema and physical-sequence work.
- Model wrapper and evidence are at commit 4eecaaddae2dcbbff3120963e305394cba465c82. Model evidence covers bounded no-overflow fixtures, not general delivery.

Focused fake pusher race checks passed on amd-1; frozen combined source archive SHA-256 `89e6c88c3246bfb4150519ddf5d24814f9edb627bb4a828573f6b76516d91fee`. Clear/walk emitter archive SHA-256 `7163a783147675feeaae7b92d2f08c2b84d83c799352f4e792a6fe4af01ae963`; Run emitter archive SHA-256 `735d548a826a99b31b918fd974b483d6c83aaaaf1d9cb7dfde1b92a2e551e3b5`. The 64-walk emission loop exited successfully and files were retained, but individual Go stdout was discarded. The model owner reports 65 input files mapped to 12 distinct checked Clear traces; the four emitted Run traces matched previously checked inputs. The overflow `next` command finds current actionable obligations, not every historical informational notice.
