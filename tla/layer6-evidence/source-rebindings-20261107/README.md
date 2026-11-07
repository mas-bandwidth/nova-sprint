# New source snapshots, identical checked trace inputs

Two later implementation changes produced exactly the same finite trace inputs
as retained accepted checks. No model/config changed and no TLC was rerun.
The receipts compare every input byte, pin the new full source archive and raw
emission logs, and name the existing raw TLC/harness log hashes. This establishes
new archived-source finite execution correspondence at the existing model scope;
it is not final integrated-head, whole-implementation or GitHub CI acceptance.

The role-aware handoff fixture adds a file-store-backed SnapshotRoles resolver,
a fixed clock, an injected environment identifying alice, and EnsureCoordinatorHook.
Its success and stale-epoch outputs are identical to the retained 13- and 9-record
inputs. Actual walker/private-FD fixture composition remains the scope; production
outer coordinator, PostgreSQL leases, live nova-config and secrets are not proved.
The exact archive is `f25896ba01df9cc91cc48f80bf85bb9ec64886d4cbbb2a60d24b0bdc247917e4`.

The flat installer consumes the release executable instead of a tar.gz payload.
Checksum, staged candidate, check/version and activation observations remain the
same in all 64 fixed-seed outputs. All 468 source manifest hashes were independently
verified against archive `ff4f5df2f9102b1acec1812aa2a54c12045bd54d91570089a27a72afb463da15`.
The existing endpoint/macro-step mapping and process-crash/atomic-rename assumptions
remain; these traces add no power-loss durability or deployment guarantee. Focused
race and trace output is retained; the empty vet output is retained as supplied by
the emitter owner, who reported a successful vet exit.

Complete source archives and fresh traces are preserved under
`/Volumes/nova/ai/stella/working/l6-model-development-evidence-20261106/` in
`handoff-role-20261107/` and `install-flat-20261107/`, respectively. Bench originals
remain on amd-1 under `/var/tmp/stella-handoff-role-20261107/` and
`/var/tmp/stella-l6-install-flat-combined-evidence-20261107/`.
Historical checked files and raw verdict logs are referenced, not overwritten or
duplicated. Later edits require their own exact-source validation.
