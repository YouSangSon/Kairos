"""Composition of local inputs with pure research workflows."""

from __future__ import annotations

from pathlib import Path
from typing import Any

from ...application.backtest import build_manifest as manifest_artifact
from ...application.capture import build_capture_report
from ...application.experiment import build_experiment
from ..inputs.backtest import parse_backtest_execution, parse_backtest_request
from ..inputs.bars import parse_bars
from ..inputs.capture import _capture_bars, _capture_config
from ..inputs.config import parse_config
from ..inputs.json_values import _decode
from ..inputs.proposals import generate_proposal_from_bytes
from .inputs import load_bars, read_capture_input, read_signal_input


def build_manifest(
    bars_path: Path, request: dict[str, Any], *, run_id: str, dataset_id: str,
    started_at: str, finished_at: str,
) -> dict[str, Any]:
    identity = dict(run_id=run_id, dataset_id=dataset_id, started_at=started_at, finished_at=finished_at)
    quantity = parse_backtest_request(request, **identity)
    raw_bars = bars_path.read_bytes()
    execution = parse_backtest_execution(raw_bars, request)
    # shortcut: legacy hash and calculation read separately, unify in the separately approved snapshot fix.
    bars = load_bars(bars_path)
    return manifest_artifact(bars, request, quantity, execution, **identity)


def run_experiment(bars_path: Path, raw_config: dict[str, Any]) -> dict[str, Any]:
    config = parse_config(raw_config)
    raw_bars = bars_path.read_bytes()
    bars = parse_bars(raw_bars)
    return build_experiment(bars, config, raw_config, raw_bars)


def run_capture_experiment(capture_path: Path, raw_v2_config: dict[str, Any]) -> dict[str, Any]:
    config = _capture_config(raw_v2_config)
    raw = read_capture_input(capture_path)
    capture = _decode(raw)
    bars = _capture_bars(capture)
    return build_capture_report(bars, config, capture, raw_v2_config, raw)


def generate_proposal(bars_path: Path, research_bars_path: Path, artifact: dict[str, Any]) -> dict[str, Any]:
    research_bytes = read_signal_input(research_bars_path)
    raw_bars = read_signal_input(bars_path)
    return generate_proposal_from_bytes(raw_bars, research_bytes, artifact)
