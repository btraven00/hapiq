import os
import shutil
from pathlib import Path


def find_binary() -> str:
    bundled = Path(__file__).parent / "bin" / "hapiq"
    if bundled.exists() and os.access(bundled, os.X_OK):
        return str(bundled)
    found = shutil.which("hapiq")
    if found:
        return found
    raise RuntimeError(
        "hapiq binary not found. "
        "Install via 'pip install hapiq' (includes bundled binary) "
        "or put hapiq on your PATH."
    )
