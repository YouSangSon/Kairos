"""Market values and legacy decimal arithmetic."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import date, datetime
from decimal import Decimal, InvalidOperation
from typing import Any

ZERO = Decimal("0")
ONE = Decimal("1")
BPS_DENOMINATOR = Decimal("10000")


@dataclass(frozen=True)
class Bar:
    at: str
    symbol: str
    open: Decimal
    high: Decimal
    low: Decimal
    close: Decimal
    volume: Decimal


def minimum_integer(value: int, field: str, minimum: int) -> int:
    if value < minimum:
        raise ValueError(f"{field} must be an integer of at least {minimum}")
    return value


def valid_ohlcv(bar: Bar) -> bool:
    return (min(bar.open, bar.high, bar.low, bar.close) > ZERO and bar.volume >= ZERO
            and bar.low <= min(bar.open, bar.close) and bar.high >= max(bar.open, bar.close) and bar.low <= bar.high)


def validate_bars(bars: list[Bar]) -> None:
    if not bars or len({bar.symbol for bar in bars}) != 1:
        raise ValueError("buy_and_hold.v1 requires at least one bar for exactly one symbol")
    if any(not bar.symbol or not valid_ohlcv(bar) for bar in bars):
        raise ValueError("bars require a symbol, valid positive OHLC prices, and non-negative volume")


def validate_bar_times(times: list[datetime]) -> None:
    if times != sorted(times) or len(set(times)) != len(times):
        raise ValueError("bars must be strictly ordered by unique bar_at")


def validate_fetch_order(started: tuple[datetime, int], fetched: tuple[datetime, int]) -> None:
    if fetched < started:
        raise ValueError("fetch clock order is reversed")


def validate_observation_day(day: date, previous: date | None, base_date: date) -> None:
    if day > base_date or (previous is not None and day <= previous):
        raise ValueError("provider dates must be unique, increasing and no later than request base date")


def validate_signal_time(signal_time: datetime, research_end: datetime) -> None:
    if signal_time <= research_end:
        raise ValueError("signal must follow the research sample")


@dataclass
class Order:
    order_id: str
    side: str
    requested_quantity: Decimal
    signal_index: int
    eligible_index: int
    remaining: Decimal
    fills: list[dict[str, str]]


def decimal_string(value: Decimal) -> str:
    rendered = format(value, "f")
    if "." in rendered:
        rendered = rendered.rstrip("0").rstrip(".")
    return rendered or "0"


def decimal_input(value: Any, field: str) -> Decimal:
    if not isinstance(value, str):
        raise ValueError(f"{field} must be a decimal string")
    try:
        result = Decimal(value)
    except InvalidOperation as error:
        raise ValueError(f"{field} must be a decimal string") from error
    if not result.is_finite() or (result.is_zero() and result.is_signed()) or decimal_string(result) != value:
        raise ValueError(f"{field} must be a canonical decimal string")
    return result
