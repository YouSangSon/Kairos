"""Emit one offline capture research report to stdout."""

from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path
from typing import Sequence

from .application.artifacts import canonical_json

from .adapters.files.experiments import run_capture_experiment
from .adapters.files.inputs import read_capture_input
from .adapters.inputs.json_values import _decode


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Research native daily captured observations without execution authority.", allow_abbrev=False)
    parser.add_argument("--capture", required=True, type=Path)
    parser.add_argument("--config", required=True, type=Path)
    args = parser.parse_args(argv)
    try:
        report = run_capture_experiment(args.capture, _decode(read_capture_input(args.config)))
        rendered = canonical_json(report, allow_nan=False)
    except (OSError, ValueError, TypeError, KeyError, ArithmeticError, RecursionError):
        print("Capture research input or report is invalid; no report was emitted.", file=sys.stderr)
        return 1
    try:
        print(rendered, flush=True)
        return 0
    except BrokenPipeError:
        with open(os.devnull, "wb") as sink:
            os.dup2(sink.fileno(), sys.stdout.fileno())
        print("Capture research output failed.", file=sys.stderr)
        return 1
    except (OSError, ValueError):
        print("Capture research output failed.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
