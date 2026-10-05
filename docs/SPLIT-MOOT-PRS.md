# Split PR audit: 2026-10-05

Reviewer: Stella, GPT-6 Astra / Codex. **HOLD: migration proofs and actual mixed-PR splits remain incomplete.**

Snapshot: GitHub read at 2026-10-05T17:49Z; nova-sprint main `69f5619c086e79313c74dcecca4a12bbb46178c1`.
The mandated predecessor `8e8b0d2f661947db958366533ecdfb5a16f23a3c` is retained by merge.
This refresh supersedes its stale dispositions, short heads, and unsupported queue-transfer claims.
No PR was closed, commented on, reopened, merged, or created in this run.

## Evidence and limits

All 43 entries below were read from the forge: the plan's 41 candidates, predecessor addition #5234,
and removal prerequisite #5309. Full paginated file inventories were fetched for the 26 individual
moved/mixed proposals (not the promotion or already-merged integration group). GraphQL file totals
over 100 are not treated as complete inventories. The companion [evidence manifest](SPLIT-MOOT-PRS-EVIDENCE.json)
records exact heads, states, full available file lists, patch digests and patch-check diagnostics.

For each available candidate patch, replace only the module import string
`github.com/mas-bandwidth/nova-tools` with `github.com/mas-bandwidth/nova-sprint`, then run
`git apply --reverse --check` against the unchanged main source projection. Every such full-patch
check refused. That means **unproved**, not absent: later edits, renamed files, extracted docs,
shared ledger changes and the one-binary fold can prevent an equivalent migrated change from
matching. No failed check authorizes closing a PR. The #5149 and #5300 diff endpoint requests
failed with no diff; their complete file inventories were still fetched. No empty patch was checked
or counted as proof. No commit-ancestry or semantic-equivalence proof is claimed for the open PRs.
The merged integration group is recorded from current forge state, not independently re-proven
as a complete nova-sprint migration.

#5309 remains OPEN at `b081bb0b84e917061916a7165b10c53d0528be4c`. The historical plan says
moved-only PRs become moot once it lands; its old “Done” label described queued work, not a merged
result. The coordinator must resolve sequence and migration proof before any further closure.
The historical #5306 and #5234 comments below were read and their URLs verified; they assert
seed-source inclusion at `15f13249e1474b2c943e88e75025b4bbe515d93b`. They are preserved as
historical receipts, not fresh full-patch proofs at today's main and not actions performed here.

## Current inventory

`retain-unproved` means leave open for migration review; `split-required` means a proposal follows,
not a completed split or queue transfer. Historical closed/merged states do not assert migration.
The offline test checks inventory integrity and refuses unsupported new-closure records; it cannot
prove live forge state, semantic migration, release order, dev landing, or installation.

| PR | Head | State | Decision | Evidence | Pointer |
| --- | --- | --- | --- | --- | --- |
| 5015 | 495e32563353a11dbb92677066667204c8972c47 | OPEN | retain-unproved | normalized reverse patch refused; migration unproved, not absent | https://github.com/mas-bandwidth/nova-tools/pull/5015 |
| 5222 | 415e0dfb99eafe02012c7f17d4de4188a374d58e | CLOSED | historical-closed | forge closed before this run; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5222 |
| 5227 | c6d3d2857d5c4a34a652767de44012d4eb74c16b | CLOSED | historical-closed | forge closed before this run; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5227 |
| 5228 | 085c44211b1fd618bb5901ca7098d9abfb6a35bc | OPEN | retain-unproved | normalized reverse patch refused; migration unproved, not absent | https://github.com/mas-bandwidth/nova-tools/pull/5228 |
| 5230 | 8ac0c403ec901dc961ff87ffd8c2af11f4dd0212 | OPEN | retain-unproved | normalized reverse patch refused; migration unproved, not absent | https://github.com/mas-bandwidth/nova-tools/pull/5230 |
| 5233 | ddd7672f541641b8591f367514e1a3001185e436 | OPEN | retain-unproved | normalized reverse patch refused; migration unproved, not absent | https://github.com/mas-bandwidth/nova-tools/pull/5233 |
| 5234 | 32b4403a3f4bc17267a29f239f0d923fc61c3a7b | CLOSED | historical-closed | historical seed-inclusion closure comment verified; current migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5234#issuecomment-5983545195 |
| 5235 | 44631600c60ffc8fa6466ff6127d0d5a35018781 | OPEN | retain-unproved | normalized reverse patch refused; migration unproved, not absent | https://github.com/mas-bandwidth/nova-tools/pull/5235 |
| 5236 | 1b8db5d7024105845577041e6294e68c9f56ac7b | CLOSED | historical-closed | forge closed before this run; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5236 |
| 5238 | 324df7eaa87645eb1314195df5db0c610fb31d1c | CLOSED | historical-closed | forge closed before this run; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5238 |
| 5239 | 7b3ba6f3b33b2e28cba0b66b4355944034bae1e7 | OPEN | retain-unproved | normalized reverse patch refused; migration unproved, not absent | https://github.com/mas-bandwidth/nova-tools/pull/5239 |
| 5243 | e76d6fccc6925cabd0d05f562501f63ecb8dee0e | OPEN | retain-unproved | normalized reverse patch refused; migration unproved, not absent | https://github.com/mas-bandwidth/nova-tools/pull/5243 |
| 5246 | 8c0b0c504c75b9ba2471d43ea7fd1dbbd9e3a30d | OPEN | retain-unproved | normalized reverse patch refused; migration unproved, not absent | https://github.com/mas-bandwidth/nova-tools/pull/5246 |
| 5278 | bf86443e9a7c81957b834e2c61403458645e37d4 | OPEN | retain-unproved | normalized reverse patch refused; migration unproved, not absent | https://github.com/mas-bandwidth/nova-tools/pull/5278 |
| 5306 | 3b464c53ca89ede946e95189b735545c76973b8e | CLOSED | historical-closed | historical seed-inclusion closure comment verified; current migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5306#issuecomment-5983544727 |
| 5308 | 0b38f7582758f9b6d90eca2d4be972b2728abedc | OPEN | retain-unproved | normalized reverse patch refused; migration unproved, not absent | https://github.com/mas-bandwidth/nova-tools/pull/5308 |
| 4956 | d5b6995d72f668974e10d678a32edbb07719b02d | OPEN | split-required | split proposal below; no split branch, PR or queue receipt created | https://github.com/mas-bandwidth/nova-tools/pull/4956 |
| 5014 | 3d0c7a9ec9e71abd7e899afe0086ec6cf1fda92d | OPEN | split-required | split proposal below; no split branch, PR or queue receipt created | https://github.com/mas-bandwidth/nova-tools/pull/5014 |
| 5149 | c8b627022667cfe3d10d4bf7b6663100c26328e2 | CLOSED | historical-closed | forge closed before this run; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5149 |
| 5247 | a3df8e2aa144c561c822c70f330e5e4308336722 | OPEN | split-required | split proposal below; no split branch, PR or queue receipt created | https://github.com/mas-bandwidth/nova-tools/pull/5247 |
| 5266 | c369270d6c0204c0ac5f84fbb9badbb9eecb936e | OPEN | split-required | split proposal below; no split branch, PR or queue receipt created | https://github.com/mas-bandwidth/nova-tools/pull/5266 |
| 5268 | 5f90c1ddddc0f8468b4d89df608729a12011b024 | OPEN | split-required | split proposal below; no split branch, PR or queue receipt created | https://github.com/mas-bandwidth/nova-tools/pull/5268 |
| 5275 | 0e438acfbdbef502738deba0178b3fbd9e7184f5 | OPEN | split-required | split proposal below; no split branch, PR or queue receipt created | https://github.com/mas-bandwidth/nova-tools/pull/5275 |
| 5281 | b3085b4359b492ddc235606f6adee7386888df02 | OPEN | split-required | split proposal below; no split branch, PR or queue receipt created | https://github.com/mas-bandwidth/nova-tools/pull/5281 |
| 5300 | ab105fd9bb8c33c138dec4e256cf85d8ccf06e88 | OPEN | split-required | split proposal below; no split branch, PR or queue receipt created | https://github.com/mas-bandwidth/nova-tools/pull/5300 |
| 5307 | f322683d4d4e5e914c64d1ff5efa3ed516e1a11e | OPEN | split-required | split proposal below; no split branch, PR or queue receipt created | https://github.com/mas-bandwidth/nova-tools/pull/5307 |
| 5258 | ca8bb8cc36d5c8297aae0c082105a54dbb0c75b4 | OPEN | promotion-order | open dev-to-main promotion; sequence decision, not a split | https://github.com/mas-bandwidth/nova-tools/pull/5258 |
| 5282 | 8d3fb02c2b6fcbd1fb6d587fdbeeb49c53ba68fc | MERGED | historical-merged | forge merged; historical integration group; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5282 |
| 5283 | af251a1d3b88e7bcec3e200c9e7f605a25289d98 | MERGED | historical-merged | forge merged; historical integration group; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5283 |
| 5285 | ce033d4a2370370e9eaa8308e469d48c196b735d | MERGED | historical-merged | forge merged; historical integration group; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5285 |
| 5287 | b38835f6463a83233fbf8375fa871d0f9f4ba51f | MERGED | historical-merged | forge merged; historical integration group; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5287 |
| 5288 | 8ef49210eeabc0ee399c54ad11b3c848d2d8736c | MERGED | historical-merged | forge merged; historical integration group; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5288 |
| 5289 | fa98b6e35015ce9106bc53d9171c4d1c80f1e88c | MERGED | historical-merged | forge merged; historical integration group; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5289 |
| 5291 | 60c5bc07636878381b4fc06756ff05f7701617f5 | MERGED | historical-merged | forge merged; historical integration group; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5291 |
| 5293 | 321d8999da2d40fa17cd17eda62ee91dbfc94e3f | MERGED | historical-merged | forge merged; historical integration group; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5293 |
| 5297 | d680c98fb2748aa1c7fba11b0513f45f5440c25c | MERGED | historical-merged | forge merged; historical integration group; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5297 |
| 5298 | 533066ba0eac4ef7b8e17612fb81c79443351f9e | MERGED | historical-merged | forge merged; historical integration group; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5298 |
| 5299 | 31da27a008a331547afd6da7b124238df0282004 | MERGED | historical-merged | forge merged; historical integration group; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5299 |
| 5301 | dd2d245e6102ea29b7d828a3039478bb3496d683 | MERGED | historical-merged | forge merged; historical integration group; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5301 |
| 5303 | 4c681e0415f8fa01a15107592d736db6ebcf9d31 | MERGED | historical-merged | forge merged; historical integration group; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5303 |
| 5304 | 9b468920e5cf7c0e5885e275884b1828c19919d8 | MERGED | historical-merged | forge merged; historical integration group; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5304 |
| 5305 | a2aff60e99d3b286b2667dff6ee4e9d8bddd5088 | MERGED | historical-merged | forge merged; historical integration group; migration not re-proven | https://github.com/mas-bandwidth/nova-tools/pull/5305 |
| 5309 | b081bb0b84e917061916a7165b10c53d0528be4c | OPEN | dependency-open | removal still open; historical closure precondition unresolved | https://github.com/mas-bandwidth/nova-tools/pull/5309 |

## Mixed split proposals: not executed

These are bounded destination proposals based on the plan and current complete path inventories.
The manifest supplies every source path. Shared files require hunk-level separation and tests;
copying whole mixed patches or deleting coverage to make them apply would be unsafe.
No coordinator acknowledgement or split landing is implied.

- **#4956**: Ledger shards: retain generic internal/ci, shared ledgers and SPEC-CI; transfer only sprint-owned ledger entries after destination CI design is agreed.
- **#5014**: Move cmd/nova-sprint/view_lock_test.go and internal/sprint/VIEW.lock; retain AGENTS.md, SPEC-CI and STANDARD portions.
- **#5149**: Historically closed; sprint SetKeys changes and SPEC-SPRINT belong here, while FLEET and generic ledger portions require separate consideration. Do not reopen automatically.
- **#5247**: Move sprint admission code and sprint docs; retain cmd/nova-swarm and internal/swarm admission/lint and SPEC-SWARM. Split shared CLI text by verb.
- **#5266**: Move sprint lint/quack/seat code; retain fleet/child-rules.txt, internal/docs/agentsmap.go, AGENTS, STANDARD and SPEC-CARD-CONTRACT.
- **#5268**: Move sprint friend-take epoch fencing and SPEC-SPRINT; resolve shared docs/FRIENDS.md independently in nova-tools.
- **#5275**: Port former cmd/nova-work help/tests to the current nova-sprint work verb; retain internal/tool/tool.go. The old cmd/nova-work path is gone after the one-binary fold.
- **#5281**: Move sprint machinery/seat/coordinator code and SPEC-SPRINT; retain internal/seatcheck, infra/functional-image, generic CI/docs/catalog and instruction changes.
- **#5300**: Separate sprint read/deal/cost/deadline/store/driver changes from nova-config, nova-swarm, internal/config/harness/member/cardhdr, shared CI/docs and migration changes. Complete diff endpoint unavailable; no migration assertion.
- **#5307**: Move sprint dealer local-lane caps, routes/store/tests and sprint docs; retain nova-local/config/swarm/local/member and shared docs. Historical plan requests migration0029 because0028 was occupied; recheck current migration sequence before porting.

## Remaining coordinator decisions

- Re-prove each open moved-only change at the target main by commit inclusion or a reviewed equivalent diff; retain it open meanwhile.
- Assign and validate the nine still-open mixed splits above, preserving both halves. #5149 is already closed and needs a historical disposition review, not another closure.
- Resolve #5258 promotion and #5309 removal order separately. Do not infer release or installation from this audit.
- This card forbids new PR creation and restricts source changes to docs and this record test. Actual source ports/split PRs need separate authorized work. These proposals are not completed splits.
