"""Hatch build hook: compile the hapiq Go binary into hapiq/bin/ at wheel build time."""

import os
import subprocess
import sys
from pathlib import Path

from hatchling.builders.hooks.plugin.interface import BuildHookInterface


class CustomBuildHook(BuildHookInterface):
    def initialize(self, version: str, build_data: dict) -> None:
        repo_root = Path(__file__).parent.parent
        bin_dir = Path(__file__).parent / "hapiq" / "bin"
        bin_dir.mkdir(parents=True, exist_ok=True)

        binary_name = "hapiq.exe" if sys.platform == "win32" else "hapiq"
        output = bin_dir / binary_name

        env = os.environ.copy()
        goos = env.get("GOOS", "")
        goarch = env.get("GOARCH", "")

        cmd = ["go", "build", "-o", str(output), "."]
        result = subprocess.run(cmd, cwd=str(repo_root), env=env)
        if result.returncode != 0:
            raise RuntimeError(
                f"go build failed (GOOS={goos or 'default'}, GOARCH={goarch or 'default'}). "
                "Make sure Go is installed: https://go.dev/dl/"
            )

        if sys.platform != "win32":
            output.chmod(0o755)

        build_data["artifacts"].append(f"hapiq/bin/{binary_name}")
        build_data["force_include"][str(output)] = f"hapiq/bin/{binary_name}"
