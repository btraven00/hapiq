import functools
import os
import shutil
import subprocess
from pathlib import Path

from ._version import __version__

BUNDLED = Path(__file__).parent / "bin" / "hapiq"


def _binary_version(path: str) -> str:
    try:
        out = subprocess.run([path, "version"], capture_output=True, text=True, timeout=30)
    except OSError:
        return ""
    # stdout is "hapiq <version>"; an old binary without `version` exits non-zero.
    parts = out.stdout.split()
    return parts[1] if out.returncode == 0 and len(parts) == 2 else ""


@functools.lru_cache(maxsize=None)
def find_binary() -> str:
    bundled = BUNDLED
    if bundled.exists() and os.access(bundled, os.X_OK):
        found = _binary_version(str(bundled))
        if found != __version__:
            raise RuntimeError(
                f"bundled hapiq binary is {found or 'unknown'}, but the package is {__version__}. "
                "Reinstall the package (for a checkout: pip install -e python/) "
                f"or delete {bundled} to use hapiq from PATH."
            )
        return str(bundled)
    found = shutil.which("hapiq")
    if found:
        return found
    raise RuntimeError(
        "hapiq binary not found. "
        "Install via 'pip install hapiq' (includes bundled binary) "
        "or put hapiq on your PATH."
    )
