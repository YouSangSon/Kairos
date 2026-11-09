"""Local file inputs."""

from __future__ import annotations

import os
import stat
from pathlib import Path

from ...domain.values import Bar
from ..inputs.bars import parse_bars


def load_bars(path: Path) -> list[Bar]:
    return parse_bars(path.read_bytes())


def _read_bounded_input(path: Path, kind: str) -> bytes:
    # O_NONBLOCK prevents FIFO waits; regular-file validation uses the opened descriptor.
    with open(path, "rb", opener=lambda name, flags: os.open(name, flags | os.O_NONBLOCK)) as source:
        if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
            raise ValueError(f"{kind} input must be a regular file")
        raw = source.read(1_048_577)
        if len(raw) > 1_048_576:
            raise ValueError(f"{kind} input exceeds 1 MiB")
        return raw


def read_signal_input(path: Path) -> bytes:
    return _read_bounded_input(path, "signal")


def read_capture_input(path: Path) -> bytes:
    return _read_bounded_input(path, "capture research")
