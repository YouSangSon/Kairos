"""Offline paper target proposals; never an execution authorization."""

from __future__ import annotations

import argparse
import signal
from pathlib import Path
from typing import Sequence

from .adapters.files.experiments import generate_proposal
from .adapters.files.inputs import read_signal_input
from .adapters.inputs.proposals import ARTIFACT_FIELDS, generate_paper_input_bundle, generate_proposal_from_bytes
from .adapters.signal_stream.producer import run


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Produce a local SMA paper target proposal without execution authority.")
    parser.add_argument("--bars", required=True, type=Path)
    parser.add_argument("--research-bars", required=True, type=Path)
    parser.add_argument("--artifact", required=True, type=Path)
    parser.add_argument("--watch", action="store_true", help="emit changed latest-close proposals as NDJSON; no execution authority")
    parser.add_argument("--bundle", action="store_true", help="emit a proposal with its exact validated CSV snapshots")
    parser.add_argument("--capture-dir", type=Path, help="archive each emitted bundle in an existing private directory before output; not an execution receipt")
    args = parser.parse_args(argv)
    if args.capture_dir is not None and not args.bundle:
        parser.error("--capture-dir requires --bundle")
    return run(args.bars, args.research_bars, args.artifact,
               watch=args.watch, bundle=args.bundle, capture_directory=args.capture_dir)


if __name__ == "__main__":
    # Background launchers can inherit an ignored SIGINT disposition.
    signal.signal(signal.SIGINT, signal.default_int_handler)
    raise SystemExit(main())
