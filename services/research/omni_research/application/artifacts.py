"""Canonical artifact encoding and hashes."""

from __future__ import annotations

import hashlib
import json
from typing import Any


def canonical_json(value: Any, *, allow_nan: bool = True) -> str:
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=allow_nan)


def canonical_hash(value: Any) -> str:
    return hashlib.sha256(canonical_json(value).encode("utf-8")).hexdigest()
