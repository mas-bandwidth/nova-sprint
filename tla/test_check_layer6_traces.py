#!/usr/bin/env python3
"""Focused failure checks for the Layer6 gate; run on the Linux bench."""
import importlib.util
import json
import os
from pathlib import Path
import platform
import sys
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('gate', Path(__file__).with_name('check-layer6-traces.py'))
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)


@unittest.skipUnless(platform.system() == 'Linux', 'Linux bench only')
class GateFailures(unittest.TestCase):
    def incomplete_cases(self, root):
        cases = [dict(name=n, model=m) for n, m in gate.expected_cases().items()]
        (root / 'emission.json').write_text(json.dumps(dict(head='fixture', cases=cases,
             hashes={'spec/check-layer6-traces.py': gate.digest(Path(gate.__file__))})))
        directory = root / 'inputs' / cases[0]['name']
        directory.mkdir(parents=True)
        (directory / 'RunTraceLog.tla').write_text('TraceLog == <<\n  [i |-> 1, action |-> "Init"]\n>>\n')
        (root / 'spec').mkdir()
        (root / 'spec/check-layer6-traces.py').write_bytes(Path(gate.__file__).read_bytes())
        return root / 'spec/check-trace.sh'

    def check_rejection(self, shell, reason):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.incomplete_cases(root).write_text(shell)
            with patch.object(gate.socket, 'gethostname', return_value='vision-test'):
                with self.assertRaisesRegex(RuntimeError, 'trace did not match'):
                    gate.check(root, SimpleNamespace(check_timeout=5))
            result = json.loads((root / 'check-results.json').read_text())[0]
            self.assertFalse(result['matched'])
            self.assertFalse(result['raw_ok'])
            self.assertIsNotNone(result['harness_sha256'])
            self.assertEqual(result['raw_sha256'] is None, reason == 'missing')

    def test_success_stdout_without_raw_log_is_refused(self):
        self.check_rejection('echo "TRACE OK steps=1"\n', 'missing')

    def test_success_stdout_with_warning_is_refused(self):
        self.check_rejection('printf "%s\\n" "Model checking completed. No error has been found." "Warning: fixture" > "$TRACE_LOG"\necho "TRACE OK steps=1"\n', 'warning')

    def test_success_stdout_with_error_is_refused(self):
        self.check_rejection('printf "%s\\n" "Model checking completed. No error has been found." "Error: fixture" > "$TRACE_LOG"\necho "TRACE OK steps=1"\n', 'error')

    def test_inherited_trace_knobs_are_removed(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'emit-logs').mkdir()
            fake_go = root / 'fake-go'
            fake_go.write_text('#!' + sys.executable + '\nimport os,json\nprint(json.dumps([os.getenv("NOVA_UNUSED_TRACE_SEED"),os.getenv("NOVA_RUN_TRACE_SEED"),os.getenv("NOVA_RUN_TRACE_STOP")]))\n')
            fake_go.chmod(0o700)
            with patch.dict(os.environ, {'NOVA_UNUSED_TRACE_SEED': 'stray', 'NOVA_RUN_TRACE_SEED': 'wrong', 'NOVA_RUN_TRACE_STOP': '1'}):
                gate.run_emit(root, root, 'fixture', './modules/run', 'TestFixture',
                              {'NOVA_RUN_TRACE_SEED': '1'}, SimpleNamespace(go=str(fake_go), emit_timeout=5))
            self.assertEqual(json.loads((root / 'emit-logs/fixture.log').read_text()), [None, '1', None])

    def test_stopped_case_has_explicit_seed_and_mode(self):
        self.assertEqual(len(gate.expected_cases()), 73)
        self.assertEqual(gate.expected_cases()['run-stop'], 'Run')
        cases = []
        with patch.object(gate, 'run_emit') as emit, patch.object(
                gate, 'case', side_effect=lambda repo, out, name, model, trace: dict(name=name, model=model)):
            gate.emit_simple(Path('/source'), Path('/output'), SimpleNamespace(), cases)
        calls = {call.args[2]: call.args for call in emit.call_args_list}
        stopped = calls['run-stop']
        self.assertEqual(stopped[3:5], ('./modules/run', 'TestEmitRunTrace'))
        self.assertEqual(stopped[5], {'NOVA_RUN_TRACE_SEED': '0', 'NOVA_RUN_TRACE_STOP': '1',
                                      'NOVA_RUN_TRACE_PATH': '/output/emitted/run-stop.tla'})
        for seed in range(3):
            self.assertNotIn('NOVA_RUN_TRACE_STOP', calls['run-' + str(seed)][5])
        self.assertEqual(sum(c['name'] == 'run-stop' for c in cases), 1)

    def test_empty_case_manifest_cannot_pass(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'emission.json').write_text(json.dumps(dict(cases=[], hashes={})))
            with patch.object(gate.socket, 'gethostname', return_value='vision-test'):
                with self.assertRaisesRegex(RuntimeError, 'all 73'):
                    gate.check(root, SimpleNamespace(check_timeout=5))

    def test_ci_runners_and_reserved_hosts_are_allowed_never_studio(self):
        ci = {'GITHUB_ACTIONS': 'true', 'RUNNER_NAME': 'any-bench-runner'}
        self.assertTrue(gate.tlc_host_allowed('amd-2', ci))
        self.assertFalse(gate.tlc_host_allowed('amd-2', {}))
        self.assertFalse(gate.tlc_host_allowed('studio', ci))
        self.assertTrue(gate.tlc_host_allowed('vision', {}))
        self.assertTrue(gate.tlc_host_allowed('hetzner2', {}))

    def test_timeout_stops_descendant_process(self):
        with tempfile.TemporaryDirectory() as tmp:
            marker = Path(tmp) / 'escaped'
            child = 'import time;from pathlib import Path;time.sleep(0.5);Path(' + repr(str(marker)) + ').write_text("escaped")'
            parent = 'import subprocess,sys,time;subprocess.Popen([sys.executable,"-c",' + repr(child) + ']);print("started",flush=True);time.sleep(10)'
            code, text = gate.run_process([sys.executable, '-c', parent], None, dict(os.environ), 0.2)
            self.assertEqual(code, 124)
            self.assertIn('TIMEOUT', text)
            time.sleep(0.7)
            self.assertFalse(marker.exists(), 'timed-out job left a live descendant')


if __name__ == '__main__':
    unittest.main()
