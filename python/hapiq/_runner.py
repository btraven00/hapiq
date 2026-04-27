import json
import os
import subprocess

from ._binary import find_binary


class HapiqError(Exception):
    pass


def run(args: list[str], timeout: int = 600, env: dict | None = None) -> dict | list:
    binary = find_binary()
    result = subprocess.run(
        [binary] + args,
        capture_output=True,
        text=True,
        timeout=timeout,
        env={**os.environ, **(env or {})},
    )
    if result.returncode != 0:
        raise HapiqError(result.stderr.strip() or f"hapiq exited with code {result.returncode}")
    return json.loads(result.stdout)
