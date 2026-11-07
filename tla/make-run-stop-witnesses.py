#!/usr/bin/env python3
"""Stopped-run contract checks; atomic captured AtSeq is the existing store assumption."""
from pathlib import Path
import importlib.util
import json

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('base', HERE / 'make-run-witnesses.py')
base = importlib.util.module_from_spec(spec)
spec.loader.exec_module(base)


def config(stopped=False, stops=1, target=None):
    text = base.config('Run', work=1, budget=1, reports=1, failures=0, processes=1, temporal=False)
    text = text.replace('MaxStops = 0', 'MaxStops = ' + str(stops))
    text = text.replace('InitiallyStopped = FALSE', 'InitiallyStopped = ' + ('TRUE' if stopped else 'FALSE'))
    if target:
        text = '\n'.join(line for line in text.splitlines() if not line.startswith('INVARIANT ')) + '\n'
        return text + 'INVARIANT ' + target + '\n'
    return text + 'PROPERTY WorkWhenRunning\nPROPERTY AllEventsSent\n'


def main():
    source = (HERE / 'Run.tla').read_text()
    cases = []
    for name, stopped, stops in [('RunStopStart', False, 1), ('RunInitiallyStopped', True, 0)]:
        (HERE / (name + '.cfg')).write_text(config(stopped, stops))
        cases.append(dict(name=name, module='Run.tla', cfg=name + '.cfg', expect='pass'))
    faults = [
        ('RunStoppedTickGateDropped', 'NoTickAfterObservedStop', True, 0,
         'Fold(p) ==\n  /\\ Active(p) /\\ local[p].phase = "ready"\n  /\\ local[p].control = "running"',
         'Fold(p) ==\n  /\\ Active(p) /\\ local[p].phase = "ready"'),
        ('RunStoppedFenceDropped', 'NoAcceptedTickWhileStopped', False, 1,
         '/\\ sequenceFresh[p] \\* Atomic store AtSeq condition',
         '/\\ TRUE \\* FAULT: precheck cannot fence an in-flight write'),
    ]
    for name, target, stopped, stops, before, after in faults:
        assert source.count(before) == 1, name
        text = source.replace('MODULE Run ', 'MODULE ' + name + ' ', 1).replace(before, after, 1)
        (HERE / 'witnesses/layer6' / (name + '.tla')).write_text(text)
        (HERE / 'witnesses/layer6' / (name + '.cfg')).write_text(config(stopped, stops, target))
        cases.append(dict(name=name, module='witnesses/layer6/' + name + '.tla',
                          cfg='witnesses/layer6/' + name + '.cfg', expect='invariant', target=target))
    (HERE / 'RunStop-runs.json').write_text(json.dumps(cases, indent=2) + '\n')


if __name__ == '__main__':
    main()
