head: 5db6f5cf7156c4420780cb954ea7c2688186e28e
branch: sprint/a-rate-limited-refusal-never-fails-the-cardc.w12.g1.e16
verdict: not-done
gate: go test -count=1 -timeout 600s ./internal/sprint ./pkg/member
output: -
report: TestRestsDueCountsTransientProviderFailuresNeverNoResults fails: routeEnds not finding ProviderTakes from test setup

## Body

### Diff stat
4 files changed, 21 insertions(+), 34 deletions(-)

### Changes made
- RouteRests: Removed rate backoff reading from PropRateBackoff (rate-limited backoff should NOT block deals)
- RestsDue: Removed rate backoff computation (rate-limited ends no longer trigger rests)
- member.Judge: Added `pu.Sha == ""` guard for rate-limited provider failures
- steps_work.go: Fixed gofmt alignment on ProviderTake literal

### Test updates
- TestRestsDueCountsTransientProviderFailuresNeverNoResults: Updated expectations to NOT count rate-limited (429) toward rests
- TestARetiredRestKeepsItsMark: Updated to NOT expect 429s after retired rest to count

### Not done
- TLA+ model (RouteRest.tla) and MC config not updated
- New tests for rate-limited backoff (TestARateLimitBacksTheRouteOffBriefly) not added
- Rate-limited end no longer count toward 3-in-10 rest per card spec

### Gate failure
TestRestsDueCountsTransientProviderFailuresNeverNoResults fails because routeEnds is not finding ProviderTakes from the test setup. The test uses FieldProviderTake+"1" on cards but ProviderTakes reads FieldProviderTake+itoa(n). The tests for no_result cases pass but 5xx/timeout cases fail with due=nil.
