# Setup durable-choice trace refinement

SetupChoices is a new contract model; the historical Setup model and its checked
artifacts remain unchanged. Step 6 has a durable services choice of unknown,
none, or configured. None has no key obligation. Configured requires each key
identity in the bounded `KeyQuestions` constant. Prompt counts are per identity,
not an aggregate count pretending that several providers share one prompt.

`Supply` persists a coordinator/dashboard answer without a prompt. `ChooseServices`
persists the observed durable services value and records whether an actual prompt
occurred. `Ask(key)` samples a successful prompt plus durable secret save; a crash
before save is outside that atomic sampled boundary and can require another
prompt. Initial durable answers are distinct from prompt counters, which begin
at zero for this execution.

The two source-bound fixtures invoke the actual walker, Choices.Save and
RuntimeSetup.servicesAct/EnsureKey with an injected store, terminal, secret sink
and workload. They sample the terminal's real prompt count, reload initial
persisted answers, read the saved services value through Choices.Get, and validate
the saved key name. World state comes from the injected dependency; verification
history comes from actual walker observations. No secret bytes enter the trace.
Both fixtures run four actual up invocations and three seeded external regressions.
The none/configured choice is deliberately selected, not randomly chosen.

The explicit under-lock WalkInit admits observed world bits and durable answers,
with empty verified history, pc=1, phase=probe, and zero execution-local prompts.
A separate bounded check explores its admitted initial states. The trace relation
uses unchanged SetupChoices.Next and checks the ordinary safety invariants.
It does not relabel lease acquisition as a crash or fabricate prehistory.

Current code-bound scope: none and one provider, saved coordinator/dashboard
answers, successful key save, actual step probes/actions and regressions. No
Supply path, multiple-provider execution, failed secret save/crash, plaintext
transport secrecy, bootstrap/lease handoff, or arbitrary implementation behavior
is established by these finite traces. The two-provider isolated model check is
model evidence only. Full typed two-phase handoff still needs its own code trace.
