"""Market values and legacy decimal arithmetic."""

from __future__ import annotations

from dataclasses import dataclass
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
