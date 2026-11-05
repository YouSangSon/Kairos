from __future__ import annotations

import contextlib
import hashlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import test_capture_research as capture_fixtures
import test_engine as engine_fixtures
import test_signal_proposal as signal_fixtures
from omni_research.capture_cli import main as capture_main
from omni_research.cli import main as backtest_main
from omni_research.engine import build_manifest
from omni_research.improve_cli import main as experiment_main
from omni_research.signal_cli import main as signal_main

FIXTURES = Path(__file__).parents[3] / "contracts" / "fixtures"


class RefactorContractTest(unittest.TestCase):
    def cli_outputs(self, directory: Path) -> dict[str, bytes]:
        capture = directory / "capture.json"
        capture.write_text(json.dumps(capture_fixtures.synthetic_capture()), encoding="utf-8")
        bars, research, artifact = signal_fixtures.SignalProposalTest().inputs(directory, line_ending=b"\r\n")
        artifact_path = directory / "artifact.json"
        artifact_path.write_text(json.dumps(artifact), encoding="utf-8")
        cases = {
            "backtest": (backtest_main, ["--bars", str(FIXTURES / "market-bars.csv"), "--request", str(FIXTURES / "backtest-request.json"),
                                        *[item for key, value in engine_fixtures.RUN_ARGS.items() for item in ("--" + key.replace("_", "-"), value)]]),
            "experiment_v1": (experiment_main, ["--bars", str(FIXTURES / "strategy-market-bars.csv"), "--config", str(FIXTURES / "strategy-improvement-config.json")]),
            "experiment_v2": (experiment_main, ["--bars", str(FIXTURES / "strategy-market-bars.csv"), "--config", str(FIXTURES / "strategy-improvement-paper-config.json")]),
            "capture": (capture_main, ["--capture", str(capture), "--config", str(FIXTURES / "strategy-improvement-paper-config.json")]),
            "proposal": (signal_main, ["--bars", str(bars), "--research-bars", str(research), "--artifact", str(artifact_path)]),
            "bundle": (signal_main, ["--bars", str(bars), "--research-bars", str(research), "--artifact", str(artifact_path), "--bundle"]),
        }
        outputs = {}
        for name, (entrypoint, arguments) in cases.items():
            stdout, stderr = io.StringIO(), io.StringIO()
            with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
                self.assertEqual(entrypoint(arguments), 0, name)
            self.assertEqual(stderr.getvalue(), "", name)
            outputs[name] = stdout.getvalue().encode("utf-8")
        return outputs

    def test_all_cli_artifact_bytes_match_pre_refactor(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            outputs = self.cli_outputs(Path(temporary))
        self.assertEqual({name: hashlib.sha256(raw).hexdigest() for name, raw in outputs.items()}, {
            "backtest": "8a5121c733ef07f8998d4789c6e2308f29bad5d03cb36bac12b57dc9723ea8c4",
            "experiment_v1": "004fd55c0bc939f3a9394902cf0b6972590ec15f33f6dba726e0344e8882682f",
            "experiment_v2": "143c2690d0a139c95d5ed46d902e0e02644858444fb35e3dcd47b2d2fd5ce846",
            "capture": "ff5c974014ae6e55bb322d939b9a08459402f4cd343137606f0fc95f622237c5",
            "proposal": "485a311f2e87b8d30866c39df93da6d4c6826db14e677b30e9d7775040e9b869",
            "bundle": "652fb9030ec2bc92ec09a090f3672743170a0ac709af47db77068acb72a5b6eb",
        })

    def test_legacy_backtest_keeps_separate_hash_and_bar_reads(self) -> None:
        request = json.loads((FIXTURES / "backtest-request.json").read_text(encoding="utf-8"))
        original = (FIXTURES / "market-bars.csv").read_bytes()
        replacement = original.replace(b",AAPL,", b",SECOND,")
        self.assertNotEqual(original, replacement)
        with patch.object(Path, "read_bytes", side_effect=[original, replacement]) as read:
            manifest = build_manifest(FIXTURES / "market-bars.csv", request, **engine_fixtures.RUN_ARGS)
        self.assertEqual(read.call_count, 2)
        self.assertEqual(manifest["manifest"]["data"]["input_sha256"], hashlib.sha256(original).hexdigest())
        self.assertEqual({order["symbol"] for order in manifest["result"]["orders"]}, {"SECOND"})


if __name__ == "__main__":
    unittest.main()
