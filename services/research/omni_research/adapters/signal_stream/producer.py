"""Watch lifecycle, durable-before-stdout publication, and producer exits."""

from __future__ import annotations

import csv
import os
import select
import stat
import sys
import time
from pathlib import Path

from ...application.artifacts import canonical_json
from ..files.capture import capture_bundle
from ..files.inputs import read_signal_input
from ..inputs.json_values import decode_signal_artifact
from ..inputs.proposals import generate_paper_input_bundle, generate_proposal_from_bytes


def _raise_if_stdout_pipe_closed() -> None:
    try:
        fd = sys.stdout.fileno()
        if not stat.S_ISFIFO(os.fstat(fd).st_mode):
            return
    except (AttributeError, OSError, ValueError):
        return
    poller = select.poll()
    poller.register(fd, select.POLLHUP | select.POLLERR)
    if any(event & (select.POLLHUP | select.POLLERR) for _, event in poller.poll(0)):
        raise BrokenPipeError


def run(
    bars_path: Path, research_bars_path: Path, artifact_path: Path,
    *, watch: bool, bundle: bool, capture_directory: Path | None,
) -> int:
    try:
        raw = read_signal_input(artifact_path)
        artifact = decode_signal_artifact(raw)
        previous = None
        # shortcut: polling captures only observed snapshots; use source-owned delivery for every close.
        while True:
            if watch:
                _raise_if_stdout_pipe_closed()
            if previous is not None and read_signal_input(artifact_path) != raw:
                raise ValueError("watch research artifact changed")
            research_bytes = read_signal_input(research_bars_path)
            raw_bars = read_signal_input(bars_path)
            proposal = generate_proposal_from_bytes(raw_bars, research_bytes, artifact)
            if proposal != previous:
                if previous is not None and proposal["data_as_of"] <= previous["data_as_of"]:
                    raise ValueError("watch requires a new close, not rewritten history")
                output = generate_paper_input_bundle(proposal, raw_bars, research_bytes) if bundle else proposal
                encoded = canonical_json(output, allow_nan=False)
                if capture_directory is not None:
                    name = proposal['data_as_of'].replace('-', '').replace(':', '') + '-' + proposal['proposal_sha256'] + '.json'
                    try:
                        capture_bundle(capture_directory, name, (encoded + '\n').encode('utf-8'))
                    except (OSError, ValueError):
                        print("Paper input capture failed; no bundle was emitted.", file=sys.stderr)
                        return 1
                print(encoded, flush=True)
                previous = proposal
            if not watch:
                return 0
            time.sleep(1)
    except KeyboardInterrupt:
        return 130
    except BrokenPipeError:
        # Python flushes stdout again at shutdown. Redirect only this broken
        # output descriptor so termination cannot emit a second traceback.
        with open(os.devnull, 'wb') as sink:
            os.dup2(sink.fileno(), sys.stdout.fileno())
        print("Paper signal proposal output is closed; producer stopped.", file=sys.stderr)
        return 1
    except (OSError, ValueError, KeyError, TypeError, ArithmeticError, RecursionError, csv.Error):
        print("Paper signal proposal input is invalid; no proposal was produced.", file=sys.stderr)
        return 1
