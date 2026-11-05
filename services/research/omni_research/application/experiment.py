"""Walk-forward experiment artifacts from validated bars and configuration."""

from __future__ import annotations

import hashlib
from typing import Any

from .. import ENGINE_VERSION
from ..domain import paper_fill
from ..domain.experiment import ExperimentConfig, _evaluate
from ..domain.values import Bar, decimal_string
from .artifacts import canonical_hash

EVALUATION_POLICY_VERSION = "sma-expanding-walk-forward.v1"
DISCLAIMER = "Research-only experiment; not investment advice or a production trading authorization."


def fold_description(name: str, bars: list[Bar]) -> dict[str, str | int]:
    return {"name": name, "first_bar_at": bars[0].at, "last_bar_at": bars[-1].at, "bar_count": len(bars)}


def build_experiment(bars: list[Bar], config: ExperimentConfig, raw_config: dict[str, Any], raw_bars: bytes) -> dict[str, Any]:
    evaluated = _evaluate(bars, config)
    walk_forward_folds = [
        {
            "name": fold.name,
            "train": fold_description(f"{fold.name}_train", bars[fold.train.start:fold.train.stop]),
            "validation": fold_description(f"{fold.name}_validation", bars[fold.validation.start:fold.validation.stop]),
            "selected_parameters": {"fast_window": fold.selected.parameters.fast_window, "slow_window": fold.selected.parameters.slow_window},
            "train_metrics": fold.selected.metrics,
            "validation_metrics": fold.validation_metrics,
        }
        for fold in evaluated.folds
    ]
    challenger_parameters = {"fast_window": evaluated.selected.parameters.fast_window, "slow_window": evaluated.selected.parameters.slow_window}
    holdout_trades = [
        {
            "signal_at": bars[trade.signal].at,
            "eligible_at": bars[trade.eligible].at,
            "fill_at": bars[trade.fill].at,
            "side": trade.side,
            "quantity": trade.quantity,
            "price": trade.price,
        }
        for trade in evaluated.holdout_trades
    ]
    walk_forward_ok, holdout_ok, baseline_ok = evaluated.walk_forward_ok, evaluated.holdout_ok, evaluated.baseline_ok
    target = "paper_candidate" if walk_forward_ok and holdout_ok and baseline_ok else "no_promotion"
    input_sha256 = hashlib.sha256(raw_bars).hexdigest()
    selected_parameters = {
        "quantity": decimal_string(config.quantity),
        **challenger_parameters,
    }
    body = {
        "schema_version": "strategy-improvement-result.v2" if config.paper_compatible else "strategy-improvement-result.v1",
        "experiment_id": config.experiment_id,
        "input_sha256": input_sha256,
        "config_sha256": canonical_hash(raw_config),
        "manifest": {
            "strategy": {
                "name": "long_only_sma_crossover",
                "version": config.strategy_version,
                "parameters": selected_parameters,
                "parameter_hash": canonical_hash(selected_parameters),
            },
            "data": {"version": config.data_version, "input_sha256": input_sha256},
            "engine": {"name": "omni-folio-paper-reference", "version": "1.0.0"} if config.paper_compatible else {"name": "omni-folio-reference", "version": ENGINE_VERSION},
            "evaluation_policy": {"version": paper_fill.EVALUATION_POLICY_VERSION if config.paper_compatible else EVALUATION_POLICY_VERSION},
        },
        "execution": config.execution,
        "evaluation": {
            "method": "expanding_walk_forward_then_final_holdout",
            "minimum_fold_bars": config.minimum_fold_bars,
            "folds": walk_forward_folds,
            "final_holdout": fold_description("final_holdout", bars[evaluated.holdout.start:evaluated.holdout.stop]),
            "selection_metric": "highest training after_cost_return, then lowest training max_drawdown, then windows ascending",
            "final_holdout_evaluated_after_selection": True,
        },
        "candidates": [
            {"parameters": {"fast_window": item.parameters.fast_window, "slow_window": item.parameters.slow_window}, "pre_holdout_metrics": item.metrics}
            for item in evaluated.candidates
        ],
        "challenger": {
            "parameters": challenger_parameters,
            "pre_holdout_metrics": evaluated.selected.metrics,
            "walk_forward_metrics": evaluated.validation_metrics,
            "walk_forward_folds": walk_forward_folds,
            "final_holdout_metrics": evaluated.holdout_metrics,
            "final_holdout_trades": holdout_trades,
        },
        "promotion": {
            "target": target,
            "baseline": {
                "name": "buy_and_hold",
                "final_holdout_metrics": evaluated.baseline_metrics,
                "challenger_exceeds_baseline": baseline_ok,
            },
            "walk_forward_gate_passed": walk_forward_ok,
            "final_holdout_gate_passed": holdout_ok,
            "baseline_gate_passed": baseline_ok,
            "failed_gates": [
                name
                for name, passed in (
                    ("walk_forward", walk_forward_ok),
                    ("final_holdout", holdout_ok),
                    ("buy_and_hold_baseline", baseline_ok),
                )
                if not passed
            ],
        },
        "disclaimer": DISCLAIMER,
    }
    return {**body, "result_sha256": canonical_hash(body)}
