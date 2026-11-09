"""Legacy backtest request decoding."""

from __future__ import annotations

import hashlib
from decimal import Decimal
from typing import Any

from ...application.artifacts import canonical_hash
from ...domain.values import ZERO, decimal_input
from .validation import object_value, request_execution, timestamp


def parse_backtest_request(request: dict[str, Any], *, run_id: str, dataset_id: str, started_at: str, finished_at: str) -> Decimal:
    if not run_id or not dataset_id:
        raise ValueError("run_id and dataset_id are required")
    if timestamp(finished_at, "finished_at") < timestamp(started_at, "started_at"):
        raise ValueError("finished_at must not precede started_at")
    if not isinstance(request, dict) or request.get("schema_version") != "backtest-request.v1":
        raise ValueError("request must be a backtest-request.v1 object")
    strategy = object_value(request.get("strategy"), "strategy")
    data = object_value(request.get("data"), "data")
    engine = object_value(request.get("engine"), "engine")
    required_strategy = {"name", "version", "parameters", "parameter_hash"}
    required_data = {"path", "version", "input_sha256", "bar_interval", "timezone"}
    if set(strategy) != required_strategy or set(data) != required_data or set(engine) != {"name", "version"}:
        raise ValueError("request fields do not match backtest-run.v1")
    if engine != {"name": "omni-folio-reference", "version": "0.1.0"}:
        raise ValueError("request engine does not match this implementation")
    parameters = object_value(strategy["parameters"], "strategy.parameters")
    if strategy["name"] != "buy_and_hold" or set(parameters) != {"quantity", "liquidate_at_end"} or parameters["liquidate_at_end"] is not True:
        raise ValueError("only buy_and_hold with liquidate_at_end=true is supported")
    quantity = decimal_input(parameters["quantity"], "strategy.parameters.quantity")
    if quantity <= ZERO or strategy["parameter_hash"] != canonical_hash(parameters):
        raise ValueError("strategy parameters or parameter_hash are invalid")
    if data["bar_interval"] != "1d" or data["timezone"] != "UTC":
        raise ValueError("only UTC daily bars are supported")
    return quantity


def parse_backtest_execution(raw_bars: bytes, request: dict[str, Any]) -> tuple[dict[str, Any], Decimal, Decimal, Decimal, Decimal, int, Decimal]:
    if request["data"]["input_sha256"] != hashlib.sha256(raw_bars).hexdigest():
        raise ValueError("data.input_sha256 does not match bars")
    return request_execution(request)
