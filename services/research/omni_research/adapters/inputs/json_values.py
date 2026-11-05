"""JSON decoders retaining each input's number and encoding contract."""

from __future__ import annotations

import json
from typing import Any

from .validation import object_value


def _unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    value: dict[str, Any] = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("duplicate JSON field")
        value[key] = item
    return value


def _invalid_capture_number(_: str) -> None:
    raise ValueError("only integer JSON numbers are supported")


def _invalid_signal_number(_: str) -> None:
    raise ValueError("non-integer JSON number")


def _decode(raw: bytes) -> dict[str, Any]:
    value = json.loads(raw.decode("utf-8"), object_pairs_hook=_unique_object, parse_float=_invalid_capture_number, parse_constant=_invalid_capture_number)
    return object_value(value, "input")


def decode_signal_artifact(raw: bytes) -> dict[str, Any]:
    return json.loads(raw, object_pairs_hook=_unique_object, parse_float=_invalid_signal_number, parse_constant=_invalid_signal_number)
