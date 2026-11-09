"""Paper proposal and exact input bundle artifacts without execution authority."""

from __future__ import annotations

import hashlib
from typing import Any

from ..domain.experiment import Parameters, sma_crossover
from ..domain.values import Bar
from .artifacts import canonical_hash, canonical_json


def build_proposal(
    bars: list[Bar], parameters: Parameters, quantity: str, artifact: dict[str, Any], raw_bars: bytes,
) -> dict[str, Any]:
    strategy = artifact["manifest"]["strategy"]
    signal = sma_crossover([bar.close for bar in bars], len(bars) - 1, parameters)
    proposal = {
        "schema_version": "paper-signal-proposal.v1", "mode": "paper_proposal_only",
        "strategy_result_sha256": artifact["result_sha256"], "strategy_parameter_sha256": strategy["parameter_hash"],
        "input_sha256": hashlib.sha256(raw_bars).hexdigest(), "symbol": bars[-1].symbol,
        "data_as_of": bars[-1].at, "signal": signal,
        "target_quantity": quantity if signal == "golden_cross" else "0" if signal == "death_cross" else None,
    }
    return {**proposal, "proposal_sha256": canonical_hash(proposal)}


def build_paper_input_bundle(proposal: dict[str, Any], bars_csv: str, research_csv: str) -> dict[str, Any]:
    bundle = {
        "schema_version": "paper-input-bundle.v1", "mode": "paper_bundle_only", "proposal": proposal,
        "bars_csv": bars_csv, "research_csv": research_csv,
    }
    # JSON escaping can expand valid one-MiB snapshots beyond the Go ingress cap.
    if len(canonical_json(bundle, allow_nan=False).encode("utf-8")) + 1 > 4 * 1_048_576:
        raise ValueError("bundle exceeds 4 MiB")
    return bundle
