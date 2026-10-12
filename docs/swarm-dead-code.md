# pkg/swarm dead-code candidates

Every `pkg/swarm` file whose exported symbols are never called by nova-sprint's
own packages (`cmd/nova-sprint`, `cmd/nova-card`, `internal/card`,
`internal/cardgen`, `internal/sprint`). Determined with:

```
grep -rhoE 'swarm\.[A-Z][A-Za-z0-9]+' --include='*.go' cmd internal \
  | sed 's/^swarm\.//' | sort -u
```

and per-file exported-symbol extraction. A file is listed when none of its
exported symbols appears in that set.

## Primary candidates (exported symbols, no external caller)

activity.go, benchsize.go, cardbudget.go, cardresult.go, contract.go,
deadline.go, declaremodel.go, fence.go, headless.go, inputlimit.go, key.go,
launchfail.go, lease.go, lintcontract.go, mirror.go, nativeasked.go,
nativebudget.go, nativeidle.go, opencode.go, pathcase.go, prewarm.go,
providercause.go, providererr.go, providerproxy.go, providerread.go,
providers.go, publicclass.go, result.go, resultformat.go, slot.go,
slotweight.go, staging.go, timeline.go, timeout.go, toolchain.go, usage.go,
usagecard.go, wall.go, wallread.go, worker.go, slots.go

## Helper files (no exported surface; build-tagged or unexported helpers)

fileretry.go, finalize.go, lintdepends.go, lock.go, pool.go, regular.go,
restage.go, signature.go

## Note

`lintbase.go` exports only `LintCardBase`, `ReadFleetLegs`, `ReadKindP95`;
none is called by cmd/internal today. It is not listed above because it is a
named, tested file of this card, but `LintCardBase` has no external caller and
is a follow-up candidate.
