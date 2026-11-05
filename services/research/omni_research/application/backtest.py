"""Backtest manifest construction from validated input."""

from __future__ import annotations

from decimal import Decimal
from typing import Any

from ..domain.backtest import simulate_backtest
from ..domain.values import Bar

DISCLAIMER = "Research fixture only; not investment advice."


def build_manifest(
    bars: list[Bar], request: dict[str, Any], quantity: Decimal,
    execution_values: tuple[dict[str, Any], Decimal, Decimal, Decimal, Decimal, int, Decimal],
    *, run_id: str, dataset_id: str, started_at: str, finished_at: str,
) -> dict[str, Any]:
    strategy, data, engine = request["strategy"], request["data"], request["engine"]
    execution, starting_cash, fee, tax_rate, slippage_bps, delay, participation = execution_values
    result = simulate_backtest(bars, quantity, starting_cash, fee, tax_rate, slippage_bps, delay, participation)
    return {
        "schema_version": "backtest-run.v1",
        "run_id": run_id,
        "status": "complete",
        "manifest": {
            "strategy": strategy,
            "data": {
                "dataset_id": dataset_id,
                "version": data["version"],
                "input_sha256": data["input_sha256"],
                "bar_interval": data["bar_interval"],
                "timezone": data["timezone"],
            },
            "engine": engine,
            "execution": execution,
            "started_at": started_at,
            "finished_at": finished_at,
        },
        "result": result,
        "disclaimer": DISCLAIMER,
    }
