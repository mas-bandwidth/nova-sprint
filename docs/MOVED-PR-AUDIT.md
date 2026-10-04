# Moved-path PR audit

Snapshot: nova-sprint/main 15f13249e1474b2c943e88e75025b4bbe515d93b; seed source nova-tools a3c6359d42b8a81f8414958c684bbd94aa16948a.

Two presence-verified PRs were closed with repository pointers after the root rechecked their live heads. The other 39 audited candidates were retained. Mixed PRs have not been split; this card remains HOLD for that outstanding work. No Go command ran. The clone is on the exact card branch, sprint/split-close-moot-prsbf2.w1.g1.e15. There is no AGENTS.md in this seed.

## Two completed closures

The root verified both live heads against this snapshot before closing. GitHub subsequently confirmed #5306 CLOSED at 2026-10-04T19:24:38Z and #5234 CLOSED at 2026-10-04T19:24:41Z, with the same heads.

### #5306: nova-sprint: friends get what machines have

Head: `3b464c53ca89ede946e95189b735545c76973b8e`. 32 moved paths, plus the extracted docs/CLI.md synopsis. Proof: the presence appendix below. [Closure pointer](https://github.com/mas-bandwidth/nova-tools/pull/5306#issuecomment-5983544727).

The sprint-specific change is already carried by [mas-bandwidth/nova-sprint](https://github.com/mas-bandwidth/nova-sprint) at [15f13249e147](https://github.com/mas-bandwidth/nova-sprint/commit/15f13249e1474b2c943e88e75025b4bbe515d93b). PR head 3b464c53ca89ede946e95189b735545c76973b8e is in seed-source commit a3c6359d42b8a81f8414958c684bbd94aa16948a; the 32 moved files were checked against that source, and the friend CLI synopsis additions are present in the extracted CLI reference. Closed this nova-tools PR as superseded by the repository split. Further sprint changes belong in nova-sprint.

### #5234: nova-sprint release: a sentinel whose needs are all in flight or landed is released

Head: `32b4403a3f4bc17267a29f239f0d923fc61c3a7b`. 7 moved paths. Proof: the presence appendix below. [Closure pointer](https://github.com/mas-bandwidth/nova-tools/pull/5234#issuecomment-5983545195).

The sprint-specific change is already carried by [mas-bandwidth/nova-sprint](https://github.com/mas-bandwidth/nova-sprint) at [15f13249e147](https://github.com/mas-bandwidth/nova-sprint/commit/15f13249e1474b2c943e88e75025b4bbe515d93b). PR head 32b4403a3f4bc17267a29f239f0d923fc61c3a7b is in seed-source commit a3c6359d42b8a81f8414958c684bbd94aa16948a; the 7 moved files were checked against that source. Closed this nova-tools PR as superseded by the repository split. Further sprint changes belong in nova-sprint.

## Every candidate and decision

102 open PRs were inventoried. 41 touch the explicitly moved path set. Full pinned Git diffs, rather than the truncated API file list, establish the counts below. In particular #5258 has 6,214 files, although the API stops at 3,000. Shared-file-only PRs, including generic CLI or ledger edits with no explicit moved path, are not declared moot by this audit.

| PR | Moved / remaining paths | Decision | Evidence or gap |
|---|---:|---|---|
| [#5325](https://github.com/mas-bandwidth/nova-tools/pull/5325) | 29 / 14 | KEEP; split/port evidence incomplete | cmd/nova-sprint/seriallock.go: file added or modified by PR is absent from seed. |
| [#5323](https://github.com/mas-bandwidth/nova-tools/pull/5323) | 44 / 6 | KEEP; split/port evidence incomplete | cmd/nova-sprint/where_tiers_test.go: file added or modified by PR is absent from seed. |
| [#5322](https://github.com/mas-bandwidth/nova-tools/pull/5322) | 254 / 442 | KEEP; split/port evidence incomplete | cmd/nova-sprint/base_gate_rule_test.go: file added or modified by PR is absent from seed. |
| [#5321](https://github.com/mas-bandwidth/nova-tools/pull/5321) | 35 / 285 | KEEP; split/port evidence incomplete | cmd/nova-sprint/answer_cover_test.go: file added or modified by PR is absent from seed. |
| [#5320](https://github.com/mas-bandwidth/nova-tools/pull/5320) | 238 / 409 | KEEP; split/port evidence incomplete | cmd/nova-sprint/view.go: file added or modified by PR is absent from seed. |
| [#5318](https://github.com/mas-bandwidth/nova-tools/pull/5318) | 235 / 417 | KEEP; split/port evidence incomplete | internal/sprint/store/readers_behind_functional_test.go: file added or modified by PR is absent from seed. |
| [#5317](https://github.com/mas-bandwidth/nova-tools/pull/5317) | 236 / 419 | KEEP; split/port evidence incomplete | internal/sprint/store/readers_behind_functional_test.go: file added or modified by PR is absent from seed. |
| [#5316](https://github.com/mas-bandwidth/nova-tools/pull/5316) | 235 / 407 | KEEP; split/port evidence incomplete | internal/sprint/store/readers_behind_functional_test.go: file added or modified by PR is absent from seed. |
| [#5313](https://github.com/mas-bandwidth/nova-tools/pull/5313) | 123 / 554 | KEEP; split/port evidence incomplete | internal/sprint/fleet_sync_cover_test.go: file added or modified by PR is absent from seed. |
| [#5311](https://github.com/mas-bandwidth/nova-tools/pull/5311) | 232 / 406 | KEEP; split/port evidence incomplete | Contextful reverse check failed; no full-presence claim. |
| [#5309](https://github.com/mas-bandwidth/nova-tools/pull/5309) | 815 / 46 | KEEP; removal / ordering | This removes the old paths; it is not a superseded feature PR. |
| [#5308](https://github.com/mas-bandwidth/nova-tools/pull/5308) | 33 / 3 | KEEP; split/port evidence incomplete | cmd/nova-sprint/auto_sentinel_test.go: file added or modified by PR is absent from seed. |
| [#5307](https://github.com/mas-bandwidth/nova-tools/pull/5307) | 8 / 45 | KEEP; split/port evidence incomplete | cmd/nova-sprint/local_route_test.go: file added or modified by PR is absent from seed. |
| [#5306](https://github.com/mas-bandwidth/nova-tools/pull/5306) | 32 / 1 | CLOSED with pointer | Source ancestry and destination byte comparison; proof above. |
| [#5305](https://github.com/mas-bandwidth/nova-tools/pull/5305) | 45 / 74 | KEEP; split/port evidence incomplete | cmd/nova-sprint/friendcards.go: added function declaration absent or changed (func writeQueueFile(dir string, states map[string]string) error {). |
| [#5303](https://github.com/mas-bandwidth/nova-tools/pull/5303) | 1 / 310 | KEEP; mixed PR needs split | Moved half carried by seed source and verified in destination; remaining changes stay in nova-tools. |
| [#5300](https://github.com/mas-bandwidth/nova-tools/pull/5300) | 188 / 28 | KEEP; split/port evidence incomplete | cmd/nova-sprint/where_tiers_test.go: file added or modified by PR is absent from seed. |
| [#5299](https://github.com/mas-bandwidth/nova-tools/pull/5299) | 44 / 289 | KEEP; mixed PR needs split | Moved half carried by seed source and verified in destination; remaining changes stay in nova-tools. |
| [#5297](https://github.com/mas-bandwidth/nova-tools/pull/5297) | 31 / 280 | KEEP; mixed PR needs split | Moved half carried by seed source and verified in destination; remaining changes stay in nova-tools. |
| [#5293](https://github.com/mas-bandwidth/nova-tools/pull/5293) | 42 / 274 | KEEP; mixed PR needs split | Moved half carried by seed source and verified in destination; remaining changes stay in nova-tools. |
| [#5291](https://github.com/mas-bandwidth/nova-tools/pull/5291) | 31 / 272 | KEEP; mixed PR needs split | Moved half carried by seed source and verified in destination; remaining changes stay in nova-tools. |
| [#5288](https://github.com/mas-bandwidth/nova-tools/pull/5288) | 31 / 265 | KEEP; mixed PR needs split | Moved half carried by seed source and verified in destination; remaining changes stay in nova-tools. |
| [#5283](https://github.com/mas-bandwidth/nova-tools/pull/5283) | 147 / 262 | KEEP; mixed PR needs split | Moved half carried by seed source and verified in destination; remaining changes stay in nova-tools. |
| [#5282](https://github.com/mas-bandwidth/nova-tools/pull/5282) | 39 / 269 | KEEP; mixed PR needs split | Moved half carried by seed source and verified in destination; remaining changes stay in nova-tools. |
| [#5281](https://github.com/mas-bandwidth/nova-tools/pull/5281) | 13 / 7 | KEEP; split/port evidence incomplete | cmd/nova-sprint/machinery.go: file added or modified by PR is absent from seed. |
| [#5278](https://github.com/mas-bandwidth/nova-tools/pull/5278) | 3 / 0 | KEEP; port evidence incomplete | Destination still declares --pulse with inherits: true and a root animation; the proposed category pulse and layout changes are not fully present. |
| [#5275](https://github.com/mas-bandwidth/nova-tools/pull/5275) | 2 / 1 | KEEP; split/port evidence incomplete | Destination lacks the proposed dry-run/no tree S-expression and --max help text. |
| [#5268](https://github.com/mas-bandwidth/nova-tools/pull/5268) | 25 / 1 | KEEP; split/port evidence incomplete | cmd/nova-sprint/friend_cards_test.go: added function declaration absent or changed (func TestBoundGenerationPreventsStaleReportAndFreshReportSucceeds(t *testing.T) {). |
| [#5266](https://github.com/mas-bandwidth/nova-tools/pull/5266) | 4 / 5 | KEEP; split/port evidence incomplete | Destination lacks the proposed first-third draft instruction in quackBrief. |
| [#5258](https://github.com/mas-bandwidth/nova-tools/pull/5258) | 860 / 5354 | KEEP; promotion / ordering | Dev-to-main promotion cannot be closed as a moved-path-only PR. |
| [#5247](https://github.com/mas-bandwidth/nova-tools/pull/5247) | 4 / 9 | KEEP; split/port evidence incomplete | cmd/nova-sprint/admission_test.go: file added or modified by PR is absent from seed. |
| [#5246](https://github.com/mas-bandwidth/nova-tools/pull/5246) | 2 / 0 | KEEP; port evidence incomplete | cmd/nova-sprint/friend_card_lifecycle_e2e_test.go: file added or modified by PR is absent from seed. |
| [#5243](https://github.com/mas-bandwidth/nova-tools/pull/5243) | 5 / 0 | KEEP; port evidence incomplete | internal/sprint/decide_test.go: added function declaration absent or changed (func TestCardTiersIsTheCeilingBeforeADealAndWhenNothingPinsARoute(t *testing.T) {). |
| [#5239](https://github.com/mas-bandwidth/nova-tools/pull/5239) | 7 / 2 | KEEP; split/port evidence incomplete | tla/MCDirtyTickFriend.cfg: file added or modified by PR is absent from seed. |
| [#5235](https://github.com/mas-bandwidth/nova-tools/pull/5235) | 8 / 0 | KEEP; port evidence incomplete | cmd/nova-sprint/finishhead.go: file added or modified by PR is absent from seed. |
| [#5234](https://github.com/mas-bandwidth/nova-tools/pull/5234) | 7 / 0 | CLOSED with pointer | Source ancestry and destination byte comparison; proof above. |
| [#5233](https://github.com/mas-bandwidth/nova-tools/pull/5233) | 2 / 0 | KEEP; port evidence incomplete | internal/sprint/store/redis_sprint_functional_test.go: added function declaration absent or changed (func TestRedisTwinCatchesUpFromARowOrderWithoutAWholeRead(t *testing.T) {). |
| [#5230](https://github.com/mas-bandwidth/nova-tools/pull/5230) | 1 / 0 | KEEP; port evidence incomplete | The actual final wait still uses time.After(30 * time.Second), not the proposed ctx.Done/cancel/failure block (destination lines 373–379). |
| [#5228](https://github.com/mas-bandwidth/nova-tools/pull/5228) | 22 / 0 | KEEP; port evidence incomplete | internal/sprint/balance.go: added function declaration absent or changed (func paid(rest RouteRest, was, b ProviderBalance, start time.Time) bool {). |
| [#5015](https://github.com/mas-bandwidth/nova-tools/pull/5015) | 4 / 0 | KEEP; port evidence incomplete | internal/sprint/store/presence_long_tick_test.go: file added or modified by PR is absent from seed. |
| [#5014](https://github.com/mas-bandwidth/nova-tools/pull/5014) | 2 / 3 | KEEP; split/port evidence incomplete | cmd/nova-sprint/view_lock_test.go: file added or modified by PR is absent from seed. |

## Evidence and limits

`audit-final.json` records every exact PR head, merge base, path list, source-ancestry result, reverse-apply result, and observed gap. `evidence/pr-N-pinned.patch` is the normalized moved-path patch. `evidence/pr-N-metadata.json` records the API snapshot. `classified.json` and `open-prs.json` preserve the initial inventory; some live heads changed during that inventory, so the pinned final results govern.

Presence proofs accept either: (a) exact PR head ancestry in the source plus every affected moved file matching the destination after repository relocation, or (b) the complete moved-only patch reverse-applying with context. A failed check is NOT proof that every behavior is absent. The table names missing files/text where observed and otherwise leaves presence unconfirmed. No unconfirmed PR was closed.

Zero-context reverse application was tested and rejected as a general proof: for #5230 it matched a different existing ctx.Done block while the actual final wait remained unchanged. It is not used for a closure decision.

Mixed PRs have not been physically split. The exact pinned diff commands and moved-path classification rules are recorded in MOVED-PR-SPLIT-SCOPES.json beside this document; copied shared packages and generic ledgers need owner review. The record does not claim new branches or replacement PRs exist. Required next steps: choose the destination base/order with the coordinator; port each absent sprint half to nova-sprint; preserve and validate each remaining nova-tools half; then link both replacements before superseding a mixed PR. The removal #5309 and promotion #5258 remain separate ordering work.

No TestMootPRsAreClosedWithAPointer exists in the seed, and internal/ci has no root Go package (only shrinkonly). No Go gate was attempted; the brief explicitly permits the audited record on the bus. The report records the unexecuted gate and the outstanding split work.

Prepared by Stella using OpenAI Codex; exact model ID unavailable.

## Pinned PR heads

| PR | Head |
|---|---|
| #5325 | `d11eeef8fe1bc2c93dda45c02f48eb6dfc5c403d` |
| #5323 | `0064cdb0aeccc8f8c1710159d7d8b8c8771afb7a` |
| #5322 | `d8659e2466b60522c84d87702faf2f5c06b7727b` |
| #5321 | `7e652dae832cb84a3d697569b004b2d3add98ae5` |
| #5320 | `b22bbbefe5465a0f7b4848ec7af31bee08d6e083` |
| #5318 | `df506a4722dca73fbc7e610e3ab2377d05896013` |
| #5317 | `0e0212ad8377753085962deb09c6e1b8931ac4d0` |
| #5316 | `37f4363c62567c17c386782e3d2759281b34027b` |
| #5313 | `6e6d02e33d504cd1ff6d6d67d44fae34125580e6` |
| #5311 | `bcc709d4d608b10e4421caa18725159358d1c71d` |
| #5309 | `b081bb0b84e917061916a7165b10c53d0528be4c` |
| #5308 | `0b38f7582758f9b6d90eca2d4be972b2728abedc` |
| #5307 | `f322683d4d4e5e914c64d1ff5efa3ed516e1a11e` |
| #5306 | `3b464c53ca89ede946e95189b735545c76973b8e` |
| #5305 | `a2aff60e99d3b286b2667dff6ee4e9d8bddd5088` |
| #5303 | `4c681e0415f8fa01a15107592d736db6ebcf9d31` |
| #5300 | `ab105fd9bb8c33c138dec4e256cf85d8ccf06e88` |
| #5299 | `31da27a008a331547afd6da7b124238df0282004` |
| #5297 | `d680c98fb2748aa1c7fba11b0513f45f5440c25c` |
| #5293 | `321d8999da2d40fa17cd17eda62ee91dbfc94e3f` |
| #5291 | `60c5bc07636878381b4fc06756ff05f7701617f5` |
| #5288 | `8ef49210eeabc0ee399c54ad11b3c848d2d8736c` |
| #5283 | `af251a1d3b88e7bcec3e200c9e7f605a25289d98` |
| #5282 | `8d3fb02c2b6fcbd1fb6d587fdbeeb49c53ba68fc` |
| #5281 | `b3085b4359b492ddc235606f6adee7386888df02` |
| #5278 | `bf86443e9a7c81957b834e2c61403458645e37d4` |
| #5275 | `0e438acfbdbef502738deba0178b3fbd9e7184f5` |
| #5268 | `5f90c1ddddc0f8468b4d89df608729a12011b024` |
| #5266 | `c369270d6c0204c0ac5f84fbb9badbb9eecb936e` |
| #5258 | `3a28fe6268637e1729b3c42d37ca631348071ca8` |
| #5247 | `a3df8e2aa144c561c822c70f330e5e4308336722` |
| #5246 | `8c0b0c504c75b9ba2471d43ea7fd1dbbd9e3a30d` |
| #5243 | `e76d6fccc6925cabd0d05f562501f63ecb8dee0e` |
| #5239 | `7b3ba6f3b33b2e28cba0b66b4355944034bae1e7` |
| #5235 | `44631600c60ffc8fa6466ff6127d0d5a35018781` |
| #5234 | `32b4403a3f4bc17267a29f239f0d923fc61c3a7b` |
| #5233 | `ddd7672f541641b8591f367514e1a3001185e436` |
| #5230 | `8ac0c403ec901dc961ff87ffd8c2af11f4dd0212` |
| #5228 | `085c44211b1fd618bb5901ca7098d9abfb6a35bc` |
| #5015 | `495e32563353a11dbb92677066667204c8972c47` |
| #5014 | `3d0c7a9ec9e71abd7e899afe0086ec6cf1fda92d` |

## Presence proof for #5306

```text
PR #5306
Head: 3b464c53ca89ede946e95189b735545c76973b8e
Merge base: 2d720d219ff507e613417233eef72ae1b853e13b
Seed source: a3c6359d42b8a81f8414958c684bbd94aa16948a
Destination: 15f13249e1474b2c943e88e75025b4bbe515d93b
git merge-base --is-ancestor 3b464c53ca89ede946e95189b735545c76973b8e a3c6359d42b8a81f8414958c684bbd94aa16948a: exit 0

Every moved file is byte-identical to the seed source after only the documented module-path / fleet relocation transformations (or already identical without transformation):
7bfb7180b1570f51a7832b82a90fd7a395b07f32a2904bfc045e1a6eda93ccd3  cmd/nova-sprint/coordinator.go  [module/fleet relocation]
879be5ba204833de106b1a91f6c1be7d08b6b630d25ae016129b9c676179b389  cmd/nova-sprint/coordinator_test.go  [exact]
c8f18c3852fd1b23e5cc7c1cbd490ad6f8f15b8ba9c461f905e924ee17db6202  cmd/nova-sprint/friend_cards_test.go  [module/fleet relocation]
c4e326cdf5db21a395f30feced7eac9717b37afaaad50f1ec8fcb96236b0ef75  cmd/nova-sprint/friend_take_test.go  [module/fleet relocation]
05672645dc0469e70e251cd8e6ffd849b3275bb5cb1e2ca4f07f8e6d56b029bb  cmd/nova-sprint/friend_verbs_test.go  [module/fleet relocation]
b545b27107f52ba0ebe4be990e17dca652ef6de7cdecbab42323be55c5845966  cmd/nova-sprint/friendcards.go  [module/fleet relocation]
ec3dac469069b7e3d742150ede35b24350112c19f2b4e5ae7aa010b90a11aee7  cmd/nova-sprint/friends.go  [module/fleet relocation]
17c34d4da7717e6aed07861bdb80447ba09f36517b8c7a5d55b75c3313cfca73  cmd/nova-sprint/friendtake.go  [module/fleet relocation]
16740b52f9edf43cd97289ed68ad2938c1075a94d13ff2db5e31cbb0df0d1233  cmd/nova-sprint/reads.go  [module/fleet relocation]
2206f211be710fc4339858f8ec4908a69c961029d006031964f5b5f622d9ed9a  cmd/nova-sprint/serve.go  [module/fleet relocation]
310936559ab69314ee20d4f9cdc1bf060168120d8b2a074964570d0e5edd3d9f  cmd/nova-sprint/stats.go  [module/fleet relocation]
f85f67d009030a486f35842996c090b8cab59f989e730ad2e6d76530ad141e59  cmd/nova-sprint/verbhelp.go  [module/fleet relocation]
8908063057fa8eb88c7471ad23a61e952e800a88e8d4d3e0627fe7a52a099dd7  cmd/nova-sprint/verbs.go  [module/fleet relocation]
4f6f2d18891ad9324f36b5d3bf9ff148c616d54c1afb319812e8c96fc218deff  docs/SPEC-SPRINT.md  [exact]
7534f0425f8ea41b8e0ce4151696d25a3974ab77561f14ed4eefd338dab60ac5  internal/sprint/friend_deadline.go  [exact]
dcbd96b5c503c13d89fd8fde30baf3422bf835566288f984897d7e167874b58f  internal/sprint/friend_deadline_test.go  [exact]
d7a6d69f246835932b840b1789256c578d2ccf641bc1fd01254dc706ef95ecf0  internal/sprint/friend_deal.go  [module/fleet relocation]
0c8b85de1559103075170f3adb4eed9602d063266fc8d5b2bbc13bed19445985  internal/sprint/friend_deal_test.go  [exact]
b225233405cb633d1c763f8c979e77e869c7788071bded4ff309e7c2e522290e  internal/sprint/friend_level.go  [exact]
1997dfacad4ddd73dd3472320e529069a73969a267c8f715215dfd9936e56c64  internal/sprint/friend_level_test.go  [exact]
fb6c4098215fd46efe3558f4497146391b02a227a904210016a712a7e68eb1fb  internal/sprint/friend_take.go  [exact]
3debd357aeed0787095db1d0dc728b2e7102ded8a578cb10a9f90a26be4b4ed8  internal/sprint/friend_take_test.go  [exact]
518b9c93edfc461e126a79b047dcfda95d41f66608dbf27a4befb24583f042ad  internal/sprint/held.go  [exact]
4b13785dafab88d831770e8b862c3dd814eb4565653484c8888a2a7dbe9c3521  internal/sprint/presence.go  [module/fleet relocation]
f70c55d38d4e8ae975577265a12f5d71a5c856bafbb92ab30a2dd938fd83b316  internal/sprint/stats.go  [module/fleet relocation]
e33944a8007f70f666b4421d14f928b2c2db21d4f2a75ef419c1dfc074e78305  internal/sprint/steps_tick.go  [exact]
7cb37521ff32ee0a752f06e42e54bbd4993ffa2cb90207925241cebe63d5b192  internal/sprint/steps_work.go  [module/fleet relocation]
337fc1dea471b0b4879975a74f6f46ebd77a864de09752291821b704d9a72d13  internal/sprint/store/friends.go  [module/fleet relocation]
5ebb5341acb5507e9f55beb600872c8a403f2016ff6426eb3b7db06beb1f2a0a  internal/sprint/store/friends_cover_test.go  [exact]
73c85de0d42943a31bf52cedf2b8af1852882a9cbdb9586a4321d904f20e6c8e  internal/sprint/store/friends_test.go  [module/fleet relocation]
e0053df61b2cc12ebefa90878e2d471d41cecb9e2882aab97d5e8602a37ab4a7  internal/sprint/store/steps.go  [module/fleet relocation]
29f58afec1cef3b2fdb5e2f38f8d5f20c7df0b2f5c2f55450567ffb78a34324b  internal/sprint/store/teardown_test.go  [module/fleet relocation]

The only remaining path is docs/CLI.md. Its four added synopsis lines are present uniquely and in the same friend section at destination lines 87,89,90,91. The replaced bare beat/up lines are absent as full lines in that section. Later seed changes add reason/until to friend down and add friend health; these surrounding additions explain why contextful reverse application fails. This was inspected directly, not inferred from a context-free patch.
```

## Presence proof for #5234

```text
PR #5234
Head: 32b4403a3f4bc17267a29f239f0d923fc61c3a7b
Merge base: 4296165394dad2d092aa0423f3ba9017a48514f0
Seed source: a3c6359d42b8a81f8414958c684bbd94aa16948a
Destination: 15f13249e1474b2c943e88e75025b4bbe515d93b
git merge-base --is-ancestor 32b4403a3f4bc17267a29f239f0d923fc61c3a7b a3c6359d42b8a81f8414958c684bbd94aa16948a: exit 0

Every moved file is byte-identical to the seed source after only the documented module-path / fleet relocation transformations (or already identical without transformation):
8b36f0ecf250a3c1c30404844aae198e173a02ba3718943f20bcab1c46126f02  cmd/nova-sprint/release_unreached_test.go  [module/fleet relocation]
8908063057fa8eb88c7471ad23a61e952e800a88e8d4d3e0627fe7a52a099dd7  cmd/nova-sprint/verbs.go  [module/fleet relocation]
4f6f2d18891ad9324f36b5d3bf9ff148c616d54c1afb319812e8c96fc218deff  docs/SPEC-SPRINT.md  [exact]
ef7ae04aeb54050e6890416492c916c67f11b89139b65ffa5c4356d36dfcf5b3  docs/SPRINT-COORDINATOR.md  [exact]
8a6353223e0a1650ec730ff5143b5dc394c6f899586ce4b2b65217894440fc1b  internal/sprint/sentinel_test.go  [exact]
06bc9fefa63537565e60bd2cc57aecb36fbddaae6091c9728183478374cd54c5  internal/sprint/steps_sentinel.go  [exact]
e0053df61b2cc12ebefa90878e2d471d41cecb9e2882aab97d5e8602a37ab4a7  internal/sprint/store/steps.go  [module/fleet relocation]
```
