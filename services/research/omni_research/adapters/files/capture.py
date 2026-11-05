"""Durable publication of exact paper input bundles."""

from __future__ import annotations

import os
import secrets
import stat
from pathlib import Path


def capture_bundle(directory: Path, name: str, raw: bytes) -> None:
    # Pin the directory so path replacement cannot redirect publication/cleanup.
    directory_fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    temporary = None
    try:
        info = os.fstat(directory_fd)
        if info.st_uid != os.getuid() or info.st_mode & 0o077:
            raise ValueError("capture directory must be private and owned")
        candidate = '.paper-capture-' + secrets.token_hex(16) + '.tmp'
        fd = os.open(candidate, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=directory_fd)
        temporary = candidate
        with os.fdopen(fd, 'wb') as target:
            target.write(raw)
            target.flush()
            os.fsync(target.fileno())
        try:
            os.link(temporary, name, src_dir_fd=directory_fd, dst_dir_fd=directory_fd, follow_symlinks=False)
        except FileExistsError:
            fd = os.open(name, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW, dir_fd=directory_fd)
            with os.fdopen(fd, 'rb') as existing:
                info = os.fstat(existing.fileno())
                if not stat.S_ISREG(info.st_mode) or info.st_size > 4 * 1_048_576:
                    raise ValueError("capture destination is not a bounded regular file")
                if existing.read(4 * 1_048_576 + 1) != raw:
                    raise ValueError("capture destination differs")
        os.unlink(temporary, dir_fd=directory_fd)
        temporary = None
        os.fsync(directory_fd)
    finally:
        try:
            if temporary is not None:
                os.unlink(temporary, dir_fd=directory_fd)
        finally:
            os.close(directory_fd)
