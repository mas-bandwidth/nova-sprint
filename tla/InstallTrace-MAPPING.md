# Install callback trace refinement

The input comes from `verbs/install_trace_test.go` running the real
`ReleaseInstaller.Install` with injected HTTP/check/version dependencies and a
real temporary filesystem. The fixed seed selects 64 independent attempts:
checksum rejection, check rejection, version mismatch, cancellation inside the
version callback, and successful activation, with or without a previous link.
This is scenario selection, not a long arbitrary action walk.

Every record observes the active symlink, exact candidate bytes, and whether the
previous binary remains intact. The final emitter refuses a present nonregular
or unexpected candidate instead of projecting it as absent. Callback metadata
records the result the callback is about to return; it is not a claim that the
caller has processed that return yet.

| Observed record | Model relation |
| --- | --- |
| Init | `Install.Init`, actual initial disk, no callback result |
| CheckCall | Hidden `Begin; FinishWrite`, then `Publish`; actual published complete candidate and unchanged active link must match. The callback result is recorded metadata; `checked` is still false. |
| VersionCall | `CheckPass`, requiring the preceding actual check callback returned success. The version callback's impending return is metadata; `reported` remains unread. |
| ReturnSuccess | Hidden `ReadVersion` with exactly the recorded version, then `PrepareLink`, then observed `Activate`; final actual disk and success metadata must match. |
| ReturnFailure/check | `CheckFail`, requiring the recorded failed check outcome. |
| ReturnFailure/version | Hidden `ReadVersion` of the recorded wrong version, then an unchanged terminal model state bound to the actual failure snapshot. |
| ReturnFailure/external | `Fail`; terminal checked/version fields must equal actual callback history. Thus cancellation after the version callback cannot skip processing that recorded version. |

Hidden actions cannot select different bytes, versions, or outcomes. They express
an endpoint macro-step relation, not an observation of the private Go write and
rename order. There are no hidden crash transitions. These finite runs do not
prove arbitrary implementation behavior, concurrency, process-crash coverage,
power-loss durability, or installer liveness. The isolated Install model retains
its stated atomic same-filesystem rename assumption and separate crash witnesses.

A deliberately corrupted copy of successful attempt 4 retains `active=none` in
its ReturnSuccess record. The unchanged model refuses record 4. This fixture is
checker validation, not an implementation-emitted failure.
