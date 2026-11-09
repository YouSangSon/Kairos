"""Legacy deterministic buy-and-hold calculation."""

from __future__ import annotations

from decimal import Decimal
from typing import Any

from .values import BPS_DENOMINATOR, ONE, ZERO, Bar, Order, decimal_string


def fill(order: Order, bar: Bar, quantity: Decimal, fee: Decimal, tax_rate: Decimal, slippage_bps: Decimal) -> tuple[Decimal, Decimal, Decimal]:
    price = bar.open * (ONE + slippage_bps / BPS_DENOMINATOR if order.side == "BUY" else ONE - slippage_bps / BPS_DENOMINATOR)
    notional = price * quantity
    tax = notional * tax_rate if order.side == "SELL" else ZERO
    order.remaining -= quantity
    fill_number = len(order.fills) + 1
    order.fills.append({
        "fill_id": f"fill_{order.side.lower()}_{fill_number:03d}",
        "bar_at": bar.at,
        "quantity": decimal_string(quantity),
        "price": decimal_string(price),
        "notional": decimal_string(notional),
        "fee": decimal_string(fee),
        "tax": decimal_string(tax),
        "fill_state": "complete" if order.remaining == ZERO else "partial",
    })
    return notional, tax, abs(price - bar.open) * quantity


def simulate_backtest(
    bars: list[Bar], quantity: Decimal, starting_cash: Decimal, fee: Decimal,
    tax_rate: Decimal, slippage_bps: Decimal, delay: int, participation: Decimal,
) -> dict[str, Any]:
    if len(bars) <= delay:
        raise ValueError("not enough bars for delayed execution")

    buy = Order("order_buy_001", "BUY", quantity, 0, delay, quantity, [])
    sell_signal_index = len(bars) - delay - 1
    sell = Order("order_sell_001", "SELL", quantity, sell_signal_index, sell_signal_index + delay, quantity, [])
    orders = [buy, sell]
    cash = starting_cash
    position = ZERO
    fees = taxes = slippage_cost = turnover = ZERO
    equity_marks = [cash]
    for index, bar in enumerate(bars):
        capacity = bar.volume * participation
        for order in orders:
            if order.remaining == ZERO or index < order.eligible_index or capacity <= ZERO:
                continue
            fill_quantity = min(order.remaining, capacity)
            price = bar.open * (ONE + slippage_bps / BPS_DENOMINATOR if order.side == "BUY" else ONE - slippage_bps / BPS_DENOMINATOR)
            if order.side == "BUY":
                fill_quantity = min(fill_quantity, max(ZERO, (cash - fee) / price))
            else:
                fill_quantity = min(fill_quantity, position)
            if fill_quantity <= ZERO:
                continue
            notional, fill_tax, fill_slippage = fill(order, bar, fill_quantity, fee, tax_rate, slippage_bps)
            capacity -= fill_quantity
            fees += fee
            taxes += fill_tax
            slippage_cost += fill_slippage
            turnover += notional
            if order.side == "BUY":
                cash -= notional + fee
                position += fill_quantity
            else:
                cash += notional - fee - fill_tax
                position -= fill_quantity
        equity_marks.append(cash + position * bar.close)
    if any(order.remaining != ZERO for order in orders) or position != ZERO:
        raise ValueError("insufficient eligible bars, cash, or position for a complete liquidating run")
    peak = equity_marks[0]
    max_drawdown = ZERO
    for equity in equity_marks:
        peak = max(peak, equity)
        if peak:
            max_drawdown = max(max_drawdown, (peak - equity) / peak)
    partial_fills = sum(1 for order in orders for item in order.fills if item["fill_state"] == "partial")
    realized_pnl = cash - starting_cash
    result_orders = [
        {
            "order_id": order.order_id,
            "symbol": bars[0].symbol,
            "side": order.side,
            "requested_quantity": decimal_string(order.requested_quantity),
            "signal_at": bars[order.signal_index].at,
            "first_eligible_at": bars[order.eligible_index].at,
            "first_fill_at": order.fills[0]["bar_at"],
            "fills": order.fills,
        }
        for order in orders
    ]
    return {
        "currency": "USD",
        "metrics": {
            "starting_cash": decimal_string(starting_cash),
            "ending_cash": decimal_string(cash),
            "ending_equity": decimal_string(cash),
            "total_return": decimal_string(realized_pnl / starting_cash),
            "realized_pnl": decimal_string(realized_pnl),
            "unrealized_pnl": "0",
            "fees": decimal_string(fees),
            "taxes": decimal_string(taxes),
            "slippage_cost": decimal_string(slippage_cost),
            "turnover": decimal_string(turnover / starting_cash),
            "max_drawdown": decimal_string(max_drawdown),
            "partial_fill_count": decimal_string(Decimal(partial_fills)),
            "lookahead_violations": "0",
        },
        "orders": result_orders,
        "positions": [],
    }
