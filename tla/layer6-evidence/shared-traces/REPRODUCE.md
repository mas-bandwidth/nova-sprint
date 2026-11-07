# Reproducing one recorded check

Run only on an authorized reserved Linux bench. Copy checked-spec/ into an
isolated specification directory and copy the chosen case directory into a
separate input directory. Do not place any TraceLog module in the specification
directory. With the pinned jar already available:

```
TRACE_LOG=/absolute/evidence/tlc.log bash /absolute/spec/check-trace.sh Run /absolute/input
```

Use RunClear, Setup, SetupChoices, or Install for the corresponding family.
The input contains that family's TraceLog module and config. Preserve the return
code and stdout separately; negative-install expects refusal at 4. Reserve
through nova-reserve using the bench's configured capacity before running.

The exact orchestration scripts from the two original sequential batches are
retained as run-first.py/run-second.py. They used a common staging directory with
spec/, inputs/, and separate evidence-first/ and evidence-second/ outputs.
The first batch's inputs were the 64 install attempts plus choices-configured,
choices-none and setup-frozen3. The second batch added final-clear, final-run0–2,
final-walk0–9 and negative-install. results.json records this partition. The
scripts are historical command evidence, not an instruction to run the first
batch over the subsequently expanded input directory.

Full source archives remain outside Git at the paths and hashes in
external-sources.json. Source manifests and emission logs remain here. The
archives bind those exact emission snapshots, not an inferred current Git head.
