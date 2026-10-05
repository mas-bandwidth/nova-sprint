# nova-tools PRs made moot or split by the repository split

This document records the disposition of nova-tools PRs affected by the split into nova-sprint:
both PRs touching only paths that moved to this repository, and mixed PRs touching both moved
and retained paths.

A moved-only PR is closed with a pointer only when its change is confirmed present in nova-sprint
main (seed 15f13249e1474b2c943e88e75025b4bbe515d93b); a PR whose change is absent stays open and
is listed for the coordinator. Checked 2026-10-04 by reverse-applying each open PR's patch to main
(`git apply -R --check`).

Mixed PRs touching both moved and retained paths are documented below with their live heads, split
dispositions (what stays in nova-tools vs what moves to nova-sprint), and explicit transfer pointers
for the coordinator queue.

## Moved-Only PRs

| PR | Head | Decision | Evidence |
| --- | --- | --- | --- |
| 5306 | 3b464c53ca89ede946e95189b735545c76973b8e | closed | present in main; pointer https://github.com/mas-bandwidth/nova-tools/pull/5306#issuecomment-5983544727 |
| 5234 | 32b4403a3f4bc17267a29f239f0d923fc61c3a7b | closed | present in main; pointer https://github.com/mas-bandwidth/nova-tools/pull/5234#issuecomment-5983545195 |
| 5222 | 415e0dfb99eafe02012c7f17d4de4188a374d58e | already-closed | closed before this card |
| 5227 | c6d3d2857d5c4a34a652767de44012d4eb74c16b | already-closed | closed before this card |
| 5236 | 1b8db5d7024105845577041e6294e68c9f56ac7b | already-closed | closed before this card |
| 5238 | 324df7eaa87645eb1314195df5db0c610fb31d1c | already-closed | closed before this card |
| 5015 | 495e32563353a11dbb92677066667204c8972c47 | open-absent | reverse patch does not apply to main |
| 5228 | 085c44211b1fd618bb5901ca7098d9abfb6a35bc | open-absent | reverse patch does not apply to main |
| 5230 | 8ac0c403ec90 | open-absent | reverse patch does not apply to main |
| 5233 | ddd7672f5416 | open-absent | reverse patch does not apply to main |
| 5235 | 44631600c60f | open-absent | reverse patch does not apply to main |
| 5239 | 7b3ba6f3b33b | open-absent | reverse patch does not apply to main |
| 5243 | e76d6fccc692 | open-absent | reverse patch does not apply to main |
| 5246 | 8c0b0c504c75 | open-absent | reverse patch does not apply to main |
| 5278 | bf86443e9a7c | open-absent | reverse patch does not apply to main |
| 5308 | 0b38f7582758 | open-absent | reverse patch does not apply to main |

## Mixed PR Splits

Mixed PRs touching both moved and retained paths with per-PR decisions, live heads, and explicit transfer pointers:

| PR | Head | Decision | Split Disposition | Transfer Pointer |
| --- | --- | --- | --- | --- |
| 5275 | 0e438acfbdbef502738deba0178b3fbd9e7184f5 | open-split | `nova-work` portion moves to nova-sprint; retained `internal/tool` remains in nova-tools | https://github.com/mas-bandwidth/nova-tools/pull/5275; transferred to coordinator queue for split landing |
| 5307 | f322683d4d4e5e914c64d1ff5efa3ed516e1a11e | open-split | Dealer half moves to nova-sprint under migration 0029; retained `nova-local`, `nova-config`, `nova-swarm` remain in nova-tools | https://github.com/mas-bandwidth/nova-tools/pull/5307; recorded for coordinator split landing under migration 0029 |

### Per-PR Split Details

- **PR #5275**:
  - **Head**: `0e438acfbdbef502738deba0178b3fbd9e7184f5`
  - **Scope**: Combines moved `nova-work` help with retained `internal/tool`.
  - **Split disposition**: The `nova-work` portion moves to nova-sprint; retained `internal/tool` remains in nova-tools.
  - **Explicit transfer pointer**: PR comment or disposition record transferred to coordinator queue for split landing (https://github.com/mas-bandwidth/nova-tools/pull/5275).

- **PR #5307**:
  - **Head**: `f322683d4d4e5e914c64d1ff5efa3ed516e1a11e`
  - **Scope**: Combines retained `nova-local`, `nova-config`, and `nova-swarm` changes with absent sprint dealer changes.
  - **Split disposition**: Dealer half moves to nova-sprint under migration 0029 (as required by PLAN.md, where the integration seed already holds 5299's `0028`); retained `nova-local`, `nova-config`, and `nova-swarm` remain in nova-tools.
  - **Explicit transfer pointer**: Recorded for coordinator split landing under migration 0029 (https://github.com/mas-bandwidth/nova-tools/pull/5307).
