"""Native captured observations decoded without inventing availability times."""

from __future__ import annotations

import re
from datetime import date, datetime, timedelta
from decimal import Decimal
from typing import Any

from ...domain.experiment import ExperimentConfig
from ...domain.values import Bar
from .config import parse_config
from .validation import object_value

_DECIMAL = re.compile(r"-?(?:0|[1-9][0-9]*)(?:\.[0-9]*[1-9])?")
_CLOCK = re.compile(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.([0-9]{1,9}))?Z")


def _closed(value: Any, fields: set[str], name: str) -> dict[str, Any]:
    result = object_value(value, name)
    if set(result) != fields:
        raise ValueError(f"{name} fields are invalid")
    return result


def _canonical_decimal(value: Any, field: str) -> None:
    if not isinstance(value, str) or value == "-0" or _DECIMAL.fullmatch(value) is None:
        raise ValueError(f"{field} must be a canonical decimal string")


def _provider_date(value: Any) -> date:
    if not isinstance(value, str) or re.fullmatch(r"[0-9]{8}", value) is None:
        raise ValueError("provider dates must be 8 ASCII digits")
    return date(int(value[:4]), int(value[4:6]), int(value[6:]))


def _fetch_clock(value: Any) -> tuple[datetime, int]:
    match = _CLOCK.fullmatch(value) if isinstance(value, str) else None
    if match is None:
        raise ValueError("fetch clocks must be UTC timestamps with at most 9 fractional digits")
    return datetime.fromisoformat(value[:19]), int((match[1] or "").ljust(9, "0"))


def _capture_bars(capture: dict[str, Any]) -> list[Bar]:
    _closed(capture, {
        "schema_version", "scope", "provider", "environment", "venue", "currency", "symbol", "interval",
        "request", "fetch_started_at", "fetched_at", "time_evidence", "bounds", "bars",
    }, "capture")
    constants = {
        "schema_version": "kiwoom-candle-capture.v1", "scope": "research_observation", "provider": "kiwoom",
        "environment": "mock", "venue": "XKRX", "currency": "KRW", "interval": "1d",
    }
    if any(capture[key] != expected for key, expected in constants.items()):
        raise ValueError("only native mock daily research captures are supported")
    symbol = capture["symbol"]
    if not isinstance(symbol, str) or re.fullmatch(r"[0-9]{6}", symbol) is None:
        raise ValueError("capture symbol must be 6 ASCII digits")
    request = _closed(capture["request"], {"api_id", "body"}, "request")
    body = _closed(request["body"], {"stk_cd", "base_dt", "upd_stkpc_tp"}, "request.body")
    if request["api_id"] != "ka10081" or body["stk_cd"] != symbol or body["upd_stkpc_tp"] != "1":
        raise ValueError("daily request does not match capture")
    base_date = _provider_date(body["base_dt"])
    started = _fetch_clock(capture["fetch_started_at"])
    fetched = _fetch_clock(capture["fetched_at"])
    if fetched < started:
        raise ValueError("fetch clock order is reversed")
    if base_date != (started[0] + timedelta(hours=9)).date():
        raise ValueError("request base date must match fetch start at fixed UTC+09")
    if capture["time_evidence"] != {
        "provider_field": "dt", "provider_format": "YYYYMMDD", "provider_timezone": "not_provided",
        "bar_boundaries": "not_provided", "publication_time": "not_provided", "bar_completion": "unverified",
    }:
        raise ValueError("daily time evidence is invalid")
    bounds = _closed(capture["bounds"], {"bars_limit", "pages_limit", "selection"}, "bounds")
    if (type(bounds["bars_limit"]) is not int or bounds["bars_limit"] != 500
        or type(bounds["pages_limit"]) is not int or bounds["pages_limit"] != 32
        or bounds["selection"] != "latest_up_to_limit_after_overlap_validation"):
        raise ValueError("capture bounds are invalid")
    rows = capture["bars"]
    if not isinstance(rows, list) or not 1 <= len(rows) <= 500:
        raise ValueError("capture requires 1 to 500 bars")
    bars = []
    previous = None
    for row in rows:
        _closed(row, {"provider_time", "open", "high", "low", "close", "volume"}, "bar")
        label = row["provider_time"]
        day = _provider_date(label)
        if day > base_date or (previous is not None and day <= previous):
            raise ValueError("provider dates must be unique, increasing and no later than request base date")
        previous = day
        values = {}
        for key in ("open", "high", "low", "close", "volume"):
            value = row[key]
            if not isinstance(value, str) or len(value) > 64:
                raise ValueError("source decimal tokens must be at most 64 characters")
            _canonical_decimal(value, f"bar.{key}")
            values[key] = Decimal(value)
        bar = Bar(label, symbol, **values)
        if (min(bar.open, bar.high, bar.low, bar.close) <= 0 or bar.volume < 0
            or bar.low > min(bar.open, bar.close) or bar.high < max(bar.open, bar.close) or bar.low > bar.high):
            raise ValueError("bars require valid positive OHLC and nonnegative volume")
        bars.append(bar)
    return bars


def _capture_config(raw: dict[str, Any]) -> ExperimentConfig:
    if not isinstance(raw, dict) or raw.get("schema_version") != "strategy-improvement.v2":
        raise ValueError("capture research requires strategy-improvement.v2 config")
    for section, fields in (
        ("strategy", ("quantity",)),
        ("execution", ("starting_cash", "fee", "tax", "slippage_bps", "delay_bars", "max_participation")),
        ("promotion", ("minimum_validation_after_cost_return", "minimum_holdout_after_cost_return", "maximum_holdout_max_drawdown")),
    ):
        value = object_value(raw.get(section), section)
        for field in fields:
            _canonical_decimal(value.get(field), f"{section}.{field}")
    return parse_config(raw)
