# SetupChoices contract checks

Reserved vision, two TLC workers, pinned v1.8.0 jar. Existing Setup and its
historical acceptance artifacts are unchanged. New SetupChoices models durable
none/configured service choice, per-provider key identities, and unprompted
Supply/ChooseServices separately from prompts.

| Case | Outcome | Generated / distinct |
| --- | --- | ---: |
| SetupChoices, one key | safety and EventuallyDone pass | 5449 / 3035 |
| SetupChoicesTwoKeys | safety and EventuallyDone pass | 5825 / 3259 |
| SetupChoicesNoneNeedsKey | expected EventuallyDone violation, exit 13 | 3177 / 1819 |
| SetupChoicesReprompt | expected AskedOnce violation, exit 12 | 51 / 36 |
| SetupChoicesWalk, separate sibling evidence directory | admitted initial states safety/liveness pass; 6144 initial states | 210374 / 80580 |

Counterexamples were handchecked: NoneNeedsKey reaches step 6 with durable
services=none, all prior steps verified, no key answer, and exhausted disruption
budget. Requiring a key while refusing a configured-provider prompt leaves it
stuttering forever. Reprompt asks coordinator twice while already durably answered;
state 10 has asked[coordinator]=2. These are reversed model defects, not claims
that current source exhibits them. The actual none/configured traces separately
exercise servicesAct/EnsureKey and durable choice reads.

Prompt+successful-save is one sampled action; crash-before-save can reprompt.
Two-key coverage here is isolated model evidence, not multiple-provider code
execution. Fair progress assumes returning external operations and bounded
disruptions. Exact case/module/config hashes are in results.json; the witness
generator reproduces the checked bytes.
