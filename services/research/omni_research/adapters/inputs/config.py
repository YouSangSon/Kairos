"""Strict experiment configuration decoded into domain values."""

from __future__ import annotations

from typing import Any

from ...domain import experiment
from ...domain.experiment import MINIMUM_FOLD_BARS, ExperimentConfig, Parameters
from ...domain.values import decimal_input, minimum_integer
from .validation import object_value, request_execution


def integer(value: Any, field: str, *, minimum: int = 0) -> int:
    if not isinstance(value, int) or isinstance(value, bool):
        raise ValueError(f"{field} must be an integer of at least {minimum}")
    return minimum_integer(value, field, minimum)


def parameter_values(value: Any, field: str) -> tuple[int, ...]:
    if not isinstance(value, list) or not value:
        raise ValueError(f"{field} must be a non-empty list")
    values = tuple(integer(item, field, minimum=experiment.MINIMUM_SMA_WINDOW) for item in value)
    return experiment.parameter_windows(values, field)


def parse_config(config: dict[str, Any]) -> ExperimentConfig:
    required = {"schema_version", "experiment_id", "data", "strategy", "execution", "splits", "promotion"}
    if not isinstance(config, dict) or set(config) != required or config["schema_version"] not in ("strategy-improvement.v1", "strategy-improvement.v2"):
        raise ValueError("config must be a strategy-improvement.v1 or v2 object")
    paper_compatible = config["schema_version"] == "strategy-improvement.v2"
    experiment_id = config["experiment_id"]
    if not isinstance(experiment_id, str) or not experiment_id:
        raise ValueError("experiment_id is required")
    data = object_value(config["data"], "data")
    if set(data) != {"version"} or not isinstance(data["version"], str) or not data["version"]:
        raise ValueError("data.version is required")
    strategy = object_value(config["strategy"], "strategy")
    if set(strategy) != {"name", "version", "quantity", "fast_windows", "slow_windows"}:
        raise ValueError("strategy fields are invalid")
    if not isinstance(strategy["version"], str) or not strategy["version"]:
        raise ValueError("only versioned long_only_sma_crossover is supported")
    experiment.validate_sma_strategy(strategy["name"])
    quantity = decimal_input(strategy["quantity"], "strategy.quantity")
    experiment.validate_quantity(quantity)
    fast_windows = parameter_values(strategy["fast_windows"], "strategy.fast_windows")
    slow_windows = parameter_values(strategy["slow_windows"], "strategy.slow_windows")
    candidates = tuple(Parameters(fast, slow) for fast in fast_windows for slow in slow_windows)
    experiment.validate_grid(candidates)
    if paper_compatible:
        experiment.validate_paper_strategy(strategy["version"], quantity, slow_windows)

    execution, starting_cash, fee, tax_rate, slippage_bps, delay_bars, participation = request_execution({"execution": config["execution"]})
    splits = object_value(config["splits"], "splits")
    if set(splits) != {"train_bars", "validation_bars", "holdout_bars", "minimum_fold_bars"}:
        raise ValueError("splits fields are invalid")
    minimum_fold_bars = integer(splits["minimum_fold_bars"], "splits.minimum_fold_bars", minimum=MINIMUM_FOLD_BARS)
    train_bars = integer(splits["train_bars"], "splits.train_bars", minimum=minimum_fold_bars)
    validation_bars = integer(splits["validation_bars"], "splits.validation_bars", minimum=minimum_fold_bars)
    holdout_bars = integer(splits["holdout_bars"], "splits.holdout_bars", minimum=minimum_fold_bars)
    experiment.validate_validation_folds(validation_bars, minimum_fold_bars)

    promotion = object_value(config["promotion"], "promotion")
    promotion_fields = {
        "baseline",
        "minimum_validation_after_cost_return",
        "minimum_validation_trade_count",
        "minimum_holdout_after_cost_return",
        "maximum_holdout_max_drawdown",
        "minimum_holdout_trade_count",
    }
    if set(promotion) != promotion_fields:
        raise ValueError("promotion requires the declared buy_and_hold baseline and explicit gates")
    experiment.validate_promotion_baseline(promotion["baseline"])
    minimum_validation_return = decimal_input(promotion["minimum_validation_after_cost_return"], "promotion.minimum_validation_after_cost_return")
    minimum_holdout_return = decimal_input(promotion["minimum_holdout_after_cost_return"], "promotion.minimum_holdout_after_cost_return")
    maximum_holdout_drawdown = decimal_input(promotion["maximum_holdout_max_drawdown"], "promotion.maximum_holdout_max_drawdown")
    experiment.validate_drawdown(maximum_holdout_drawdown)
    return ExperimentConfig(
        experiment_id=experiment_id,
        data_version=data["version"],
        strategy_version=strategy["version"],
        quantity=quantity,
        candidates=candidates,
        execution=execution,
        starting_cash=starting_cash,
        fee=fee,
        tax_rate=tax_rate,
        slippage_bps=slippage_bps,
        delay_bars=delay_bars,
        participation=participation,
        train_bars=train_bars,
        validation_bars=validation_bars,
        holdout_bars=holdout_bars,
        minimum_fold_bars=minimum_fold_bars,
        minimum_validation_return=minimum_validation_return,
        minimum_validation_trades=integer(promotion["minimum_validation_trade_count"], "promotion.minimum_validation_trade_count", minimum=experiment.MINIMUM_TRADE_COUNT),
        minimum_holdout_return=minimum_holdout_return,
        maximum_holdout_drawdown=maximum_holdout_drawdown,
        minimum_holdout_trades=integer(promotion["minimum_holdout_trade_count"], "promotion.minimum_holdout_trade_count", minimum=experiment.MINIMUM_TRADE_COUNT),
        paper_compatible=paper_compatible,
    )
