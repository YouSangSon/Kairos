"""Validated signal and bundle inputs composed with pure artifacts."""

from __future__ import annotations

import hashlib
import re
from decimal import Decimal
from typing import Any

from ...application.artifacts import canonical_hash
from ...application.proposals import build_paper_input_bundle, build_proposal
from ...domain import experiment
from ...domain.experiment import Parameters
from ...domain.values import validate_signal_time
from .bars import parse_bars
from .config import integer
from .validation import request_execution, timestamp

ARTIFACT_FIELDS = {
    "schema_version", "experiment_id", "input_sha256", "config_sha256", "manifest", "execution",
    "evaluation", "candidates", "challenger", "promotion", "disclaimer", "result_sha256",
}


def generate_proposal_from_bytes(raw_bars: bytes, research_bytes: bytes, artifact: dict[str, Any]) -> dict[str, Any]:
    # This validates the calculation inputs, not the complete research proof.
    # Self-hashes and claimed promotion gates remain untrusted until Go admission.
    if not isinstance(artifact, dict) or set(artifact) != ARTIFACT_FIELDS:
        raise ValueError("research artifact fields are invalid")
    body = {key: value for key, value in artifact.items() if key != "result_sha256"}
    if artifact["schema_version"] != "strategy-improvement-result.v1" or canonical_hash(body) != artifact["result_sha256"]:
        raise ValueError("research artifact hash is invalid")
    manifest = artifact["manifest"]
    strategy = manifest["strategy"]
    parameters = strategy["parameters"]
    if (set(manifest) != {"strategy", "data", "engine", "evaluation_policy"}
        or set(strategy) != {"name", "version", "parameters", "parameter_hash"}
        or not experiment.supports_proposal_strategy(strategy["name"], strategy["version"])
        or manifest["engine"] != {"name": "omni-folio-reference", "version": "0.1.0"}
        or manifest["evaluation_policy"] != {"version": "sma-expanding-walk-forward.v1"}
        or set(parameters) != {"quantity", "fast_window", "slow_window"}
        or canonical_hash(parameters) != strategy["parameter_hash"]):
        raise ValueError("unsupported strategy contract")
    request_execution({"execution": artifact["execution"]})
    promotion = artifact["promotion"]
    experiment.validate_paper_promotion(promotion)
    fast = integer(parameters["fast_window"], "fast_window", minimum=experiment.MINIMUM_SMA_WINDOW)
    slow = integer(parameters["slow_window"], "slow_window", minimum=experiment.MINIMUM_SMA_WINDOW)
    experiment.validate_proposal_windows(fast, slow, artifact["challenger"])
    quantity = parameters["quantity"]
    if (not isinstance(quantity, str) or len(quantity) > 64
        or not re.fullmatch(r"-?(?:0|[1-9][0-9]*)(?:\.[0-9]*[1-9])?", quantity)):
        raise ValueError("paper quantity must be positive whole shares")
    experiment.validate_proposal_quantity(Decimal(quantity))

    # Hash and parsed bars must refer to the same immutable byte snapshots.
    research_sha = hashlib.sha256(research_bytes).hexdigest()
    if artifact["input_sha256"] != research_sha or manifest["data"]["input_sha256"] != research_sha:
        raise ValueError("research data hash mismatch")
    research_bars = parse_bars(research_bytes)
    bars = parse_bars(raw_bars)
    experiment.validate_signal_history(len(bars), slow, re.fullmatch(r"[0-9]{6}", bars[-1].symbol) is not None,
                                       bars[-1].symbol, research_bars[-1].symbol)
    for bar in [*research_bars, *bars]:
        if not re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z", bar.at):
            raise ValueError("daily signal timestamps must be UTC whole seconds")
    validate_signal_time(timestamp(bars[-1].at, "signal time"), timestamp(research_bars[-1].at, "research end"))
    return build_proposal(bars, Parameters(fast, slow), quantity, artifact, raw_bars)


def generate_paper_input_bundle(proposal: dict[str, Any], raw_bars: bytes, research_bytes: bytes) -> dict[str, Any]:
    if len(raw_bars) > 1_048_576 or len(research_bytes) > 1_048_576:
        raise ValueError("bundle CSV input exceeds 1 MiB")
    try:
        bars_csv = raw_bars.decode("utf-8")
        research_csv = research_bytes.decode("utf-8")
    except UnicodeDecodeError as error:
        raise ValueError("bundle CSV must be UTF-8") from error
    return build_paper_input_bundle(proposal, bars_csv, research_csv)
