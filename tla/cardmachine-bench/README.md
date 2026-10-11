# cardmachine-bench

The larger `CardMachine` runs live here, outside `CASES.tsv` (as
`sprintevents-bench/` and `dirtytick-bench/` hold their larger runs).

- `MCCardMachine.cfg` — the original instance (Cards {c1,c2} with c2
  needing c1, Consumers {k1,k2} one slot each, MaxCopies 3, HeadBound(2),
  nine invariants and three temporal properties under SF). It does not
  finish: it timed out at the 110 s cap and a `--manual` rerun at 1800 s
  timed out too. The small instance (`MCCardMachineSmall*.cfg`) is the
  record in `CASES.tsv`.
