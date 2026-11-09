"""CSV input decoded into market bars."""

from __future__ import annotations

import csv
import io

from ...domain.values import Bar, ZERO, decimal_input
from .validation import timestamp


def parse_bars(data: bytes) -> list[Bar]:
    with io.StringIO(data.decode("utf-8"), newline="") as source:
        rows = csv.DictReader(source)
        required = {"bar_at", "symbol", "open", "high", "low", "close", "volume"}
        if rows.fieldnames is None or not required.issubset(rows.fieldnames):
            raise ValueError("bars CSV requires bar_at,symbol,open,high,low,close,volume columns")
        if len(set(rows.fieldnames)) != len(rows.fieldnames):
            raise ValueError("bars CSV column names must be unique")
        bars = [
            Bar(
                at=row["bar_at"],
                symbol=row["symbol"],
                open=decimal_input(row["open"], "bar.open"),
                high=decimal_input(row["high"], "bar.high"),
                low=decimal_input(row["low"], "bar.low"),
                close=decimal_input(row["close"], "bar.close"),
                volume=decimal_input(row["volume"], "bar.volume"),
            )
            for row in rows
        ]
    if not bars or len({bar.symbol for bar in bars}) != 1:
        raise ValueError("buy_and_hold.v1 requires at least one bar for exactly one symbol")
    if any(
        not bar.symbol
        or min(bar.open, bar.high, bar.low, bar.close) <= ZERO
        or bar.low > min(bar.open, bar.close)
        or bar.high < max(bar.open, bar.close)
        or bar.low > bar.high
        or bar.volume < ZERO
        for bar in bars
    ):
        raise ValueError("bars require a symbol, valid positive OHLC prices, and non-negative volume")
    parsed_times = [timestamp(bar.at, "bar_at") for bar in bars]
    if parsed_times != sorted(parsed_times) or len(set(parsed_times)) != len(parsed_times):
        raise ValueError("bars must be strictly ordered by unique bar_at")
    return bars
