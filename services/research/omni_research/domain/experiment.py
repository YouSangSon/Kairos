"""SMA signals, folds, and deterministic candidate evaluation."""

from __future__ import annotations

from dataclasses import dataclass
from decimal import Decimal
from fractions import Fraction
from math import floor
from typing import Any

from . import paper_fill
from .backtest import fill
from .values import BPS_DENOMINATOR, ONE, ZERO, Bar, Order, decimal_string


@dataclass(frozen=True, order=True)
class Parameters:
    fast_window: int
    slow_window: int


@dataclass(frozen=True)
class ExperimentConfig:
    experiment_id: str
    data_version: str
    strategy_version: str
    quantity: Decimal
    candidates: tuple[Parameters, ...]
    execution: dict[str, Any]
    starting_cash: Decimal
    fee: Decimal
    tax_rate: Decimal
    slippage_bps: Decimal
    delay_bars: int
    participation: Decimal
    train_bars: int
    validation_bars: int
    holdout_bars: int
    minimum_fold_bars: int
    minimum_validation_return: Decimal
    minimum_validation_trades: int
    minimum_holdout_return: Decimal
    maximum_holdout_drawdown: Decimal
    minimum_holdout_trades: int
    paper_compatible: bool = False


@dataclass(frozen=True)
class Span:
    start: int
    stop: int


@dataclass(frozen=True)
class Candidate:
    parameters: Parameters
    metrics: dict[str, str]


@dataclass(frozen=True)
class ValidationFold:
    name: str
    train: Span
    validation: Span
    selected: Candidate
    validation_metrics: dict[str, str]


@dataclass(frozen=True)
class IndexedTrade:
    signal: int
    eligible: int
    fill: int
    side: str
    quantity: str
    price: str


@dataclass(frozen=True)
class Evaluation:
    candidates: list[Candidate]
    folds: list[ValidationFold]
    selected: Candidate
    validation_metrics: dict[str, str]
    holdout: Span
    holdout_metrics: dict[str, str]
    holdout_trades: list[IndexedTrade]
    baseline_metrics: dict[str, str]
    walk_forward_ok: bool
    holdout_ok: bool
    baseline_ok: bool


def average(closes: list[Decimal] | list[Fraction], end: int, window: int) -> Decimal | Fraction:
    return sum(closes[end - window + 1 : end + 1]) / window


def sma_crossover(closes: list[Decimal] | list[Fraction], end: int, parameters: Parameters) -> str:
    if end < parameters.slow_window:
        return "none"
    previous_fast = average(closes, end - 1, parameters.fast_window)
    previous_slow = average(closes, end - 1, parameters.slow_window)
    current_fast = average(closes, end, parameters.fast_window)
    current_slow = average(closes, end, parameters.slow_window)
    if previous_fast <= previous_slow and current_fast > current_slow:
        return "golden_cross"
    if previous_fast >= previous_slow and current_fast < current_slow:
        return "death_cross"
    return "none"


def _simulate_indexed_fold(
    bars: list[Bar],
    config: ExperimentConfig,
    parameters: Parameters | None,
    *,
    warmup: list[Bar] | None = None,
) -> tuple[dict[str, str], list[IndexedTrade]]:
    warmup = warmup or []
    series = [*warmup, *bars]
    evaluation_start = len(warmup)
    if parameters is not None and len(series) < parameters.slow_window + config.delay_bars + 2:
        raise ValueError("fold has insufficient bars for the SMA window and delayed liquidation")

    number = Fraction if config.paper_compatible else Decimal
    render = paper_fill.exact_decimal if config.paper_compatible else decimal_string
    ratio = paper_fill.ratio_string if config.paper_compatible else decimal_string
    starting_cash = number(config.starting_cash)
    fee = number(config.fee)
    cash = starting_cash
    position = number(0)
    fees = taxes = slippage_cost = turnover = number(0)
    orders: list[Order] = []
    trade_trace: list[IndexedTrade] = []
    equity_marks = [cash]
    closes = [number(bar.close) for bar in series]
    liquidation_signal_index = len(series) - config.delay_bars - 1
    order_number = 0

    def schedule(side: str, quantity: Decimal | Fraction, signal_index: int) -> None:
        nonlocal order_number
        eligible_index = signal_index + config.delay_bars
        if eligible_index <= signal_index:
            raise ValueError("non-positive execution delay would permit lookahead")
        order_number += 1
        orders.append(Order(f"order_{order_number:03d}", side, quantity, signal_index, eligible_index, quantity, []))

    for index in range(evaluation_start, len(series)):
        bar = series[index]
        capacity = number(bar.volume) * number(config.participation)
        if config.paper_compatible:
            capacity = Fraction(floor(capacity))
        initial_capacity = capacity
        for order in orders:
            if order.remaining == ZERO or index < order.eligible_index or capacity <= ZERO:
                continue
            if config.paper_compatible:
                filled = paper_fill.calculate_fill(config.execution, {
                    "side": order.side, "open": decimal_string(bar.open), "volume": decimal_string(bar.volume),
                    "remaining_quantity": render(order.remaining), "cash": render(cash),
                    "position_quantity": render(position), "consumed_capacity": render(initial_capacity - capacity),
                })
                if filled is None:
                    continue
                price, quantity, notional, tax, slippage = (Fraction(filled[key]) for key in ("price", "quantity", "notional", "tax", "slippage"))
                order.remaining -= quantity
                order.fills.append({"fill_state": "partial" if order.remaining else "complete"})
            else:
                price = bar.open * (ONE + config.slippage_bps / BPS_DENOMINATOR if order.side == "BUY" else ONE - config.slippage_bps / BPS_DENOMINATOR)
                quantity = min(order.remaining, capacity)
                if order.side == "BUY":
                    quantity = min(quantity, max(ZERO, (cash - fee) / price))
                else:
                    quantity = min(quantity, position)
                if quantity <= ZERO:
                    continue
                notional, tax, slippage = fill(order, bar, quantity, fee, config.tax_rate, config.slippage_bps)
            capacity -= quantity
            fees += fee
            taxes += tax
            slippage_cost += slippage
            turnover += notional
            if order.side == "BUY":
                cash -= notional + fee
                position += quantity
            else:
                cash += notional - fee - tax
                position -= quantity
            trade_trace.append(IndexedTrade(
                order.signal_index, order.eligible_index, index, order.side, render(quantity), render(price),
            ))
        equity_marks.append(cash + position * number(bar.close))
        pending = any(order.remaining != ZERO for order in orders)
        if index == liquidation_signal_index:
            if not pending and position > ZERO:
                schedule("SELL", position, index)
            continue
        if index >= liquidation_signal_index or pending:
            continue
        if parameters is None:
            if index == evaluation_start and position == ZERO:
                schedule("BUY", number(config.quantity), index)
            continue
        crossover = sma_crossover(closes, index, parameters)
        if position == ZERO and crossover == "golden_cross":
            schedule("BUY", number(config.quantity), index)
        elif position > ZERO and crossover == "death_cross":
            schedule("SELL", position, index)

    if any(order.remaining != ZERO for order in orders) or position != ZERO:
        raise ValueError("fold cannot complete every delayed long-only order")
    peak = equity_marks[0]
    max_drawdown = number(0)
    for equity in equity_marks:
        peak = max(peak, equity)
        max_drawdown = max(max_drawdown, (peak - equity) / peak)
    partial_fills = sum(1 for order in orders for item in order.fills if item["fill_state"] == "partial")
    completed_round_trips = sum(1 for order in orders if order.side == "BUY" and order.remaining == ZERO)
    return {
        "after_cost_return": ratio((cash - starting_cash) / starting_cash),
        "max_drawdown": ratio(max_drawdown),
        "trade_count": str(completed_round_trips),
        "trade_count_definition": "completed_long_only_round_trips",
        "fill_count": str(len(trade_trace)),
        "turnover": ratio(turnover / starting_cash),
        "fees": render(fees),
        "taxes": render(taxes),
        "slippage_cost": render(slippage_cost),
        "partial_fill_count": str(partial_fills),
        "lookahead_violations": "0",
    }, trade_trace


def simulate_fold(
    bars: list[Bar],
    config: ExperimentConfig,
    parameters: Parameters | None,
    *,
    warmup: list[Bar] | None = None,
) -> tuple[dict[str, str], list[dict[str, str]]]:
    metrics, trades = _simulate_indexed_fold(bars, config, parameters, warmup=warmup)
    series = [*(warmup or []), *bars]
    return metrics, [
        {
            "signal_at": series[trade.signal].at,
            "eligible_at": series[trade.eligible].at,
            "fill_at": series[trade.fill].at,
            "side": trade.side,
            "quantity": trade.quantity,
            "price": trade.price,
        }
        for trade in trades
    ]


def aggregate_validation(folds: list[dict[str, str]], *, paper_compatible: bool = False) -> dict[str, str]:
    if paper_compatible:
        totals = {key: sum(Fraction(fold[key]) for fold in folds) for key in folds[0] if key != "trade_count_definition"}
        return {
            **{key: paper_fill.exact_decimal(value) for key, value in totals.items()},
            "after_cost_return": paper_fill.ratio_string(totals["after_cost_return"] / len(folds)),
            "max_drawdown": paper_fill.ratio_string(max(Fraction(fold["max_drawdown"]) for fold in folds)),
            "trade_count_definition": "sum_of_completed_long_only_round_trips_across_walk_forward_folds",
        }
    count = Decimal(len(folds))
    return {
        "after_cost_return": decimal_string(sum((Decimal(fold["after_cost_return"]) for fold in folds), ZERO) / count),
        "max_drawdown": decimal_string(max(Decimal(fold["max_drawdown"]) for fold in folds)),
        "trade_count": decimal_string(sum((Decimal(fold["trade_count"]) for fold in folds), ZERO)),
        "trade_count_definition": "sum_of_completed_long_only_round_trips_across_walk_forward_folds",
        "fill_count": decimal_string(sum((Decimal(fold["fill_count"]) for fold in folds), ZERO)),
        "turnover": decimal_string(sum((Decimal(fold["turnover"]) for fold in folds), ZERO)),
        "fees": decimal_string(sum((Decimal(fold["fees"]) for fold in folds), ZERO)),
        "taxes": decimal_string(sum((Decimal(fold["taxes"]) for fold in folds), ZERO)),
        "slippage_cost": decimal_string(sum((Decimal(fold["slippage_cost"]) for fold in folds), ZERO)),
        "partial_fill_count": decimal_string(sum((Decimal(fold["partial_fill_count"]) for fold in folds), ZERO)),
        "lookahead_violations": decimal_string(sum((Decimal(fold["lookahead_violations"]) for fold in folds), ZERO)),
    }


def _evaluate(bars: list[Bar], config: ExperimentConfig) -> Evaluation:
    number = Fraction if config.paper_compatible else Decimal
    if config.paper_compatible and any(bar.close >= Decimal("1000000000000") or -bar.close.as_tuple().exponent > 8 for bar in bars):
        raise ValueError("paper SMA closes must be below 1e12 with at most 8 decimal places")
    if len(bars) != config.train_bars + config.validation_bars + config.holdout_bars:
        raise ValueError("bars must exactly match the configured chronological train, validation, and holdout folds")
    validation_end = config.train_bars + config.validation_bars
    pre_holdout = bars[:validation_end]
    holdout = bars[validation_end:]

    def evaluate_candidates(training: list[Bar]) -> list[Candidate]:
        evaluated = []
        for parameters in config.candidates:
            metrics, _ = _simulate_indexed_fold(training, config, parameters)
            evaluated.append(Candidate(parameters, metrics))
        return evaluated

    def select(evaluated: list[Candidate]) -> Candidate:
        return min(
            evaluated,
            key=lambda item: (
                -number(item.metrics["after_cost_return"]),
                number(item.metrics["max_drawdown"]),
                item.parameters.fast_window,
                item.parameters.slow_window,
            ),
        )

    walk_forward_folds: list[ValidationFold] = []
    for index, offset in enumerate(range(0, config.validation_bars, config.minimum_fold_bars), start=1):
        train_end = config.train_bars + offset
        fold_train = bars[:train_end]
        fold_validation = bars[train_end : train_end + config.minimum_fold_bars]
        selected = select(evaluate_candidates(fold_train))
        validation_metrics, _ = _simulate_indexed_fold(
            fold_validation,
            config,
            selected.parameters,
            warmup=fold_train,
        )
        walk_forward_folds.append(ValidationFold(
            f"walk_forward_{index}", Span(0, train_end), Span(train_end, train_end + config.minimum_fold_bars),
            selected, validation_metrics,
        ))

    candidates = evaluate_candidates(pre_holdout)
    challenger = select(candidates)
    holdout_metrics, holdout_trades = _simulate_indexed_fold(
        holdout,
        config,
        challenger.parameters,
        warmup=pre_holdout,
    )
    baseline_metrics, _ = _simulate_indexed_fold(holdout, config, None, warmup=pre_holdout)
    walk_forward_metrics = aggregate_validation([item.validation_metrics for item in walk_forward_folds], paper_compatible=config.paper_compatible)
    walk_forward_ok = (
        Decimal(walk_forward_metrics["after_cost_return"]) >= config.minimum_validation_return
        and Decimal(walk_forward_metrics["trade_count"]) >= config.minimum_validation_trades
        and all(
            Decimal(item.validation_metrics["after_cost_return"]) >= config.minimum_validation_return
            and Decimal(item.validation_metrics["trade_count"]) >= config.minimum_validation_trades
            for item in walk_forward_folds
        )
    )
    holdout_ok = (
        Decimal(holdout_metrics["after_cost_return"]) >= config.minimum_holdout_return
        and Decimal(holdout_metrics["max_drawdown"]) <= config.maximum_holdout_drawdown
        and Decimal(holdout_metrics["trade_count"]) >= config.minimum_holdout_trades
    )
    baseline_ok = Decimal(holdout_metrics["after_cost_return"]) > Decimal(baseline_metrics["after_cost_return"])
    return Evaluation(
        candidates=candidates,
        folds=walk_forward_folds,
        selected=challenger,
        validation_metrics=walk_forward_metrics,
        holdout=Span(validation_end, len(bars)),
        holdout_metrics=holdout_metrics,
        holdout_trades=holdout_trades,
        baseline_metrics=baseline_metrics,
        walk_forward_ok=walk_forward_ok,
        holdout_ok=holdout_ok,
        baseline_ok=baseline_ok,
    )
