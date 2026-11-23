"""Capture research artifacts retain native row and clock evidence."""

from __future__ import annotations

import hashlib
from typing import Any

from ..domain import paper_fill
from ..domain.experiment import ExperimentConfig, Parameters, Span, _evaluate
from ..domain.values import Bar
from .artifacts import canonical_hash, canonical_json

MAX_BYTES = 1_048_576
_METRICS = (
    "after_cost_return", "max_drawdown", "trade_count", "trade_count_definition", "fill_count",
    "turnover", "fees", "taxes", "slippage_cost", "partial_fill_count",
)
LIMITATIONS = {
    "execution_delay": "delay_bars counts observation rows, not proved source availability or elapsed time",
    "capital_and_liquidation": "each fold resets hypothetical capital and requires terminal liquidation",
    "source_time": (
        "provider timezone, bar boundaries and publication time are not provided; completion is inferred only for "
        "offline research from a later-dated row in the same capture, and the last row is display-only and unverified"
    ),
    "adjustment": "adjustment request 1 does not prove applied adjustments or comparable price units",
    "authority": "offline research only; no strategy registration, paper proposal, investment advice or execution authority",
}


def _parameters(parameters: Parameters) -> dict[str, int]:
    return {"fast_window": parameters.fast_window, "slow_window": parameters.slow_window}


def _metrics(metrics: dict[str, str]) -> dict[str, str]:
    return {key: metrics[key] for key in _METRICS}


def _row(capture: dict[str, Any], index: int) -> dict[str, str | int]:
    return {"row": index, "provider_time": capture["bars"][index]["provider_time"]}


def _range(capture: dict[str, Any], span: Span) -> dict[str, Any]:
    return {"first": _row(capture, span.start), "last": _row(capture, span.stop - 1), "bar_count": span.stop - span.start}


def _eligibility(capture: dict[str, Any], research: Span) -> dict[str, Any]:
    return {
        "policy": "kiwoom-capture-bar-eligibility.v1",
        "evidence": {
            "available_at": {"status": "not_provided", "value": None},
            "completion": {
                "status": "later_bar_in_capture", "basis": "capture_order_inference",
                "unverified_row": _row(capture, research.stop),
            },
            "correction": {"status": "unverified"},
            "adjustment": {"status": "unverified"},
        },
        "research_rows": _range(capture, research),
        "decision": {"status": "not_eligible", "blockers": [
            "available_at_not_provided", "completion_not_provider_verified", "correction_unverified",
            "adjustment_unverified", "calendar_gaps_unverified", "mock_environment",
        ]},
    }


def build_capture_report(
    bars: list[Bar], config: ExperimentConfig, capture: dict[str, Any],
    raw_v2_config: dict[str, Any], raw: bytes,
) -> dict[str, Any]:
    # Decoded dates strictly increase, so only the last row lacks a later-dated row inferring its completion.
    research = Span(0, len(bars) - 1)
    evaluated = _evaluate(bars[: research.stop], config)
    body = {
        "schema_version": "captured-candle-research.v2",
        "scope": "offline_capture_research_only",
        "source": {"sha256": hashlib.sha256(raw).hexdigest(), "capture": capture},
        "config": raw_v2_config,
        "config_sha256": canonical_hash(raw_v2_config),
        "analysis": {
            "arithmetic": {"name": "omni-folio-paper-reference", "version": "1.0.0"},
            "evaluation_policy": {
                "version": paper_fill.EVALUATION_POLICY_VERSION,
                "method": "expanding_walk_forward_then_final_holdout",
                "minimum_fold_bars": config.minimum_fold_bars,
                "selection_metric": "highest training after_cost_return, then lowest training max_drawdown, then windows ascending",
                "final_holdout_evaluated_after_selection": True,
            },
            "pre_holdout": _range(capture, Span(0, evaluated.holdout.start)),
            "candidates": [
                {"parameters": _parameters(item.parameters), "pre_holdout_metrics": _metrics(item.metrics)}
                for item in evaluated.candidates
            ],
            "folds": [
                {
                    "name": fold.name, "train": _range(capture, fold.train), "validation": _range(capture, fold.validation),
                    "selected_parameters": _parameters(fold.selected.parameters), "train_metrics": _metrics(fold.selected.metrics),
                    "validation_metrics": _metrics(fold.validation_metrics),
                }
                for fold in evaluated.folds
            ],
            "selected_parameters": _parameters(evaluated.selected.parameters),
            "selected_pre_holdout_metrics": _metrics(evaluated.selected.metrics),
            "validation_metrics": _metrics(evaluated.validation_metrics),
            "final_holdout": {
                "range": _range(capture, evaluated.holdout), "metrics": _metrics(evaluated.holdout_metrics),
                "trades": [
                    {"signal": _row(capture, trade.signal), "eligible": _row(capture, trade.eligible), "fill": _row(capture, trade.fill),
                     "side": trade.side, "quantity": trade.quantity, "price": trade.price}
                    for trade in evaluated.holdout_trades
                ],
            },
            "buy_and_hold_metrics": _metrics(evaluated.baseline_metrics),
            "threshold_comparisons": {
                "walk_forward_thresholds_met": evaluated.walk_forward_ok,
                "final_holdout_thresholds_met": evaluated.holdout_ok,
                "challenger_exceeds_buy_and_hold": evaluated.baseline_ok,
            },
        },
        "data_eligibility": _eligibility(capture, research),
        "limitations": dict(LIMITATIONS),
    }
    report = {**body, "report_sha256": canonical_hash(body)}
    if len(canonical_json(report, allow_nan=False).encode("utf-8")) + 1 > MAX_BYTES:
        raise ValueError("capture research report exceeds 1 MiB")
    return report
