"""Validation of external request and execution values."""

from __future__ import annotations

from datetime import datetime
from decimal import Decimal
from typing import Any

from ...domain.values import BPS_DENOMINATOR, ONE, ZERO, decimal_input


def object_value(value: Any, field: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError(f"{field} must be an object")
    return value


def timestamp(value: str, field: str) -> datetime:
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        raise ValueError(f"{field} must be an RFC 3339 timestamp") from error
    if parsed.tzinfo is None:
        raise ValueError(f"{field} must include a timezone")
    return parsed


def request_execution(request: dict[str, Any]) -> tuple[dict[str, Any], Decimal, Decimal, Decimal, Decimal, int, Decimal]:
    execution = object_value(request.get("execution"), "execution")
    required = {"starting_cash", "fee", "tax", "slippage_bps", "delay_bars", "max_participation", "signal_price", "fill_price"}
    if set(execution) != required:
        raise ValueError("execution fields do not match backtest-run.v1")
    if execution["signal_price"] != "bar_close" or execution["fill_price"] != "next_eligible_bar_open":
        raise ValueError("only close signals and next eligible open fills are supported")
    starting_cash = decimal_input(execution["starting_cash"], "execution.starting_cash")
    fee = decimal_input(execution["fee"], "execution.fee")
    tax = decimal_input(execution["tax"], "execution.tax")
    slippage_bps = decimal_input(execution["slippage_bps"], "execution.slippage_bps")
    delay = decimal_input(execution["delay_bars"], "execution.delay_bars")
    participation = decimal_input(execution["max_participation"], "execution.max_participation")
    if starting_cash <= ZERO or fee < ZERO or not ZERO <= tax <= ONE or not ZERO <= slippage_bps < BPS_DENOMINATOR or not ZERO < participation <= ONE:
        raise ValueError("execution values are out of range")
    if delay < ONE or delay != delay.to_integral_value():
        raise ValueError("execution.delay_bars must be a whole number of at least 1 to prevent lookahead")
    return execution, starting_cash, fee, tax, slippage_bps, int(delay), participation
