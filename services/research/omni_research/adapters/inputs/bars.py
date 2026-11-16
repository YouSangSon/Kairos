"""CSV input decoded into market bars."""

from __future__ import annotations

import csv
import io

from ...domain.values import Bar, decimal_input, validate_bars, validate_bar_times
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
    validate_bars(bars)
    parsed_times = [timestamp(bar.at, "bar_at") for bar in bars]
    validate_bar_times(parsed_times)
    return bars
