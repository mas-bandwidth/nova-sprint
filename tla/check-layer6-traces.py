#!/usr/bin/env python3
"""Emit current-code fixed-seed L6 traces and check them on reserved Linux benches.

Output must be outside the clean Git checkout. Use --mode emit on the Go bench,
transfer the complete output, then --mode check on an authorized TLC bench.
The caller owns the nova-reserve reservation. No performance claim is made.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import signal
import socket
import subprocess
import sys
import tarfile

SPEC_FILES = (
    'Run.tla RunTrace.tla RunClear.tla RunClearTrace.tla Setup.tla SetupWalk.tla '
    'SetupTrace.tla SetupChoices.tla SetupChoicesWalk.tla SetupChoicesTrace.tla '
    'SetupHandoff.tla SetupHandoffTrace.tla check-trace.sh '
    'check-layer6-traces.py'
).split()


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')


def git(repo, *args):
    return subprocess.check_output(['git', '-C', str(repo), *args]).decode()


def snapshot(repo):
    if git(repo, 'status', '--porcelain', '--untracked-files=normal'):
        raise RuntimeError('emission requires a clean Git checkout')
    files = git(repo, 'ls-files', '-z').split('\0')[:-1]
    for name in files:
        if (repo / name).is_symlink():
            raise RuntimeError('source snapshot does not support symlink: ' + name)
    return {name: digest(repo / name) for name in sorted(files)}



def run_process(command, cwd, env, timeout, output=None):
    process = subprocess.Popen(command, cwd=cwd, env=env, stdout=output or subprocess.PIPE,
                               stderr=subprocess.STDOUT, text=True, start_new_session=True)
    try:
        text, _ = process.communicate(timeout=timeout)
        return process.returncode, text or ''
    except subprocess.TimeoutExpired:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        text, _ = process.communicate()
        return 124, (text or '') + 'TIMEOUT\n'


def expected_cases():
    return dict([('run-' + str(s), 'Run') for s in (0, 1, 2)]
                + [('run-stop', 'Run')]
                + [('setup', 'Setup')]
                + [('handoff-' + str(s), 'SetupHandoff') for s in (0, 1)]
                + [('choices-' + s, 'SetupChoices') for s in ('none', 'configured')])


def run_emit(repo, out, name, package, test, env_values, args):
    env = {k: v for k, v in os.environ.items() if not (k.startswith('NOVA_') and 'TRACE' in k)}
    env['GOMAXPROCS'] = '2'
    env.update({k: str(v) for k, v in env_values.items()})
    command = [args.go, 'test', '-race', '-count=1', '-p', '2', '-parallel', '2',
               '-timeout', '5m', '-run', '^' + test + '$', package]
    log = out / 'emit-logs' / (name + '.log')
    with log.open('w') as stream:
        code, _ = run_process(command, repo, env, args.emit_timeout, stream)
    save(out / 'emit-logs' / (name + '.json'), dict(name=name, command=command,
         environment=env_values, exit=code, log_sha256=digest(log)))
    if code != 0:
        raise RuntimeError(f'{name}: emitter exit {code}; see {log}')


def case(repo, out, name, model, trace):
    directory = out / 'inputs' / name
    directory.mkdir()
    shutil.copy2(trace, directory / (model + 'TraceLog.tla'))
    shutil.copy2(repo / 'tla' / (model + 'Trace.cfg'), directory / (model + 'Trace.cfg'))
    return dict(name=name, model=model)


def emit_simple(repo, out, args, cases):
    jobs = [('run-' + str(s), 'Run', './run', 'TestEmitRunTrace',
             'NOVA_RUN_TRACE_PATH', {'NOVA_RUN_TRACE_SEED': str(s)}) for s in (0, 1, 2)]
    jobs += [('run-stop', 'Run', './run', 'TestEmitRunTrace', 'NOVA_RUN_TRACE_PATH',
              {'NOVA_RUN_TRACE_SEED': '0', 'NOVA_RUN_TRACE_STOP': '1'})]
    # The RunClear cases (clear, clear-walk-0..9) are owed: their emitters went
    # with the old pusher (f697b05f), and run.Pusher has no clear summary to bind.
    jobs += [('setup', 'Setup', './verbs', 'TestSetupActualTrace', 'NOVA_SETUP_TRACE_OUT', {})]
    jobs += [('handoff-' + str(s), 'SetupHandoff', './cmd/nova-sprint', 'TestFunctionalEmitHandoffTrace',
              'NOVA_HANDOFF_TRACE_PATH', {'NOVA_HANDOFF_TRACE_STALE': str(s)}) for s in (0, 1)]
    for name, model, package, test, key, env in jobs:
        trace = out / 'emitted' / (name + '.tla')
        env[key] = str(trace)
        run_emit(repo, out, name, package, test, env, args)
        cases.append(case(repo, out, name, model, trace))


def emit_groups(repo, out, args, cases):
    choices = out / 'emitted' / 'choices'
    choices.mkdir()
    run_emit(repo, out, 'choices', './verbs', 'TestSetupChoicesActualTrace',
             {'NOVA_SETUP_CHOICES_TRACE_DIR': str(choices)}, args)
    for name in ('none', 'configured'):
        cases.append(case(repo, out, 'choices-' + name, 'SetupChoices', choices / (name + '.tla')))


def emit(repo, out, args):
    if out == repo or repo in out.parents:
        raise RuntimeError('output must be outside the source checkout')
    if digest(Path(__file__).resolve()) != digest(repo / 'tla/check-layer6-traces.py'):
        raise RuntimeError('executing helper differs from the source checkout helper')
    before = snapshot(repo)
    head = git(repo, 'rev-parse', 'HEAD').strip()
    out.mkdir(parents=True, exist_ok=False)
    for name in ('spec', 'inputs', 'emitted', 'emit-logs'):
        (out / name).mkdir()
    save(out / 'source-files.json', before)
    with tarfile.open(out / 'source.tar', 'w') as archive:
        for name in before:
            archive.add(repo / name, arcname=name, recursive=False)
    for name in SPEC_FILES:
        shutil.copy2(repo / 'tla' / name, out / 'spec' / name)
    cases = []
    emit_simple(repo, out, args, cases)
    emit_groups(repo, out, args, cases)
    if snapshot(repo) != before or git(repo, 'rev-parse', 'HEAD').strip() != head:
        raise RuntimeError('source changed during trace emission')
    if len(cases) != len(expected_cases()):
        raise RuntimeError(f'expected exactly {len(expected_cases())} actual trace cases')
    hashes = {str(p.relative_to(out)): digest(p) for p in sorted(out.rglob('*')) if p.is_file()}
    save(out / 'emission.json', dict(head=head, host=socket.gethostname(), cases=cases, hashes=hashes))
    print(f'EMITTED head={head} cases={len(cases)}', flush=True)



def tlc_host_allowed(host, env):
    if 'studio' in host.lower():
        return False
    if host.startswith(('vision', 'hetzner2')):
        return True
    # Any CI runner: the workflow's runs-on label and nova-reserve already
    # place this job on a bench, never on the working machine.
    return env.get('GITHUB_ACTIONS') == 'true'


def check(out, args):
    if not tlc_host_allowed(socket.gethostname(), os.environ):
        raise RuntimeError('TLC runs in CI or on reserved vision/hetzner2, never on the studio')
    manifest_hash = digest(out / 'emission.json')
    manifest = json.loads((out / 'emission.json').read_text())
    cases = manifest['cases']
    if len(cases) != len(expected_cases()) or {c['name']: c['model'] for c in cases} != expected_cases():
        raise RuntimeError(f'manifest must contain all {len(expected_cases())} fixed trace cases exactly once')
    for name, want in manifest['hashes'].items():
        if digest(out / name) != want:
            raise RuntimeError('artifact changed before check: ' + name)
    if digest(Path(__file__).resolve()) != manifest['hashes'].get('spec/check-layer6-traces.py'):
        raise RuntimeError('check must use the exact helper retained with the emission')
    evidence = out / 'check-logs'
    evidence.mkdir(exist_ok=False)
    results = []
    for item in manifest['cases']:
        name, model = item['name'], item['model']
        directory = out / 'inputs' / name
        records = sum(line.startswith('  [i |-> ') for line in
                      (directory / (model + 'TraceLog.tla')).read_text().splitlines())
        env = dict(os.environ, TRACE_LOG=str(evidence / (name + '.tlc.log')))
        code, text = run_process(['bash', str(out / 'spec/check-trace.sh'), model, str(directory)],
                                 None, env, args.check_timeout)
        (evidence / (name + '.harness.log')).write_text(text)
        raw = evidence / (name + '.tlc.log')
        raw_text = raw.read_text() if raw.is_file() else ''
        raw_ok = ('Model checking completed. No error has been found.' in raw_text
                  and 'Warning:' not in raw_text and 'Error:' not in raw_text)
        matched = code == 0 and raw_ok and records > 0 and re.search(r'^TRACE OK steps=' + str(records) + r'$', text, re.M)
        results.append(dict(name=name, model=model, exit=code, records=records, matched=bool(matched),
                            raw_sha256=digest(raw) if raw.is_file() else None,
                            raw_ok=raw_ok, harness_sha256=digest(evidence / (name + '.harness.log'))))
        save(out / 'check-results.json', results)
        print(f'{name}: {text.strip()}', flush=True)
        if not matched:
            raise RuntimeError(f'{name}: trace did not match; preserve {evidence}')
    if digest(out / 'emission.json') != manifest_hash:
        raise RuntimeError('emission manifest changed during checking')
    for name, want in manifest['hashes'].items():
        if digest(out / name) != want:
            raise RuntimeError('artifact changed during checking: ' + name)
    save(out / 'check-hashes.json', {str(p.relative_to(out)): digest(p) for p in sorted(evidence.iterdir())})
    print(f'MATCHED head={manifest["head"]} cases={len(results)}', flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mode', choices=('emit', 'check', 'all'), default='all')
    parser.add_argument('--repo', type=Path, default=Path(__file__).resolve().parent.parent)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--go', default=shutil.which('go') or 'go')
    parser.add_argument('--emit-timeout', type=int, default=600)
    parser.add_argument('--check-timeout', type=int, default=120)
    args = parser.parse_args()
    if platform.system() != 'Linux':
        parser.error('run on a reserved Linux bench, never the Studio')
    repo, out = args.repo.resolve(), args.out.resolve()
    if args.mode in ('emit', 'all'):
        emit(repo, out, args)
    if args.mode in ('check', 'all'):
        check(out, args)


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, OSError, subprocess.SubprocessError) as error:
        sys.exit(str(error))
