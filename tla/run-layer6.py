#!/usr/bin/env python3
"""Run layer 6 TLC on a reserved bench.

Use nova-reserve run with the host capacity (hetzner2: 8; vision: 16),
--wait 60s --why layer6-tlc -- python3 PATH.
Each invocation is sequential with two TLC workers, isolated tmp/state paths,
and a two-minute timeout. No parse error or timeout counts as a witness.
"""
import argparse
import hashlib
import json
from pathlib import Path
import socket
import subprocess
import sys
import time
HERE = Path(__file__).resolve().parent
SHA = "7a0a1e90628ebff2e3e8733375f8b684fa59504d2e02bddc4cb51a7679bb7e04"

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--only", nargs="*")
    parser.add_argument("--manifest", default="layer6-runs.json")
    parser.add_argument("--tag", default=time.strftime("%Y%m%d-%H%M%S"))
    args = parser.parse_args()
    if not socket.gethostname().startswith(("hetzner2", "vision")):
        sys.exit("TLC must run on reserved hetzner2 or authorized vision, never the Studio")
    jar = Path.home() / ".cache/nova-sprint/tla2tools-v1.8.0.jar"
    assert hashlib.sha256(jar.read_bytes()).hexdigest() == SHA, "TLC jar hash mismatch"
    java = Path.home() / "sdk/bin/java"
    out = HERE / "layer6-results" / args.tag
    out.mkdir(parents=True, exist_ok=False)
    results = []
    for case in json.loads((HERE / args.manifest).read_text()):
        if args.only and case["name"] not in args.only:
            continue
        name = case["name"]
        tmp = out / (name + "-tmp")
        tmp.mkdir()
        cmd = [str(java), "-Xmx1024m", "-XX:+UseParallelGC", "-Djava.io.tmpdir=" + str(tmp),
               "-cp", str(jar), "tlc2.TLC", "-noGenerateSpecTE", "-workers", "2",
               "-metadir", str(out / (name + "-states")), "-config", case["cfg"], case["module"]]
        start = time.monotonic()
        with (out / (name + ".log")).open("w") as log:
            try:
                code = subprocess.run(cmd, cwd=HERE, stdout=log, stderr=subprocess.STDOUT, timeout=120).returncode
            except subprocess.TimeoutExpired:
                code = 124
        text = (out / (name + ".log")).read_text()
        expect = case["expect"]
        good = (code == 0 and "Model checking completed. No error has been found." in text) if expect == "pass" else (
            code == 12 and "Invariant " + case["target"] + " is violated" in text) if expect == "invariant" else (
            code == 13 and "Temporal property " + case["target"] + " was violated" in text and "PROPERTY " + case["target"] in (HERE / case["cfg"]).read_text())
        result = dict(case, host=socket.gethostname(), exit=code, seconds=round(time.monotonic()-start, 3), checked=good,
                      module_sha256=hashlib.sha256((HERE / case["module"]).read_bytes()).hexdigest(),
                      cfg_sha256=hashlib.sha256((HERE / case["cfg"]).read_bytes()).hexdigest())
        results.append(result)
        print(json.dumps(result), flush=True)
    (out / "results.json").write_text(json.dumps(results, indent=2) + "\n")
    print("results=" + str(out), flush=True)
    return 0 if results and all(result["checked"] for result in results) else 1

if __name__ == "__main__":
    sys.exit(main())
