"""Hatch build hook: compile the hapiq Go binary into hapiq/bin/ at wheel build time.

GOOS/GOARCH select the target (default: this machine), and the wheel is tagged
for that platform, so one Linux runner can build every wheel.
"""

import os
import subprocess
from pathlib import Path

from hatchling.builders.hooks.plugin.interface import BuildHookInterface

# Go 1.26 needs macOS 12+; CGO_ENABLED=0 binaries are static, so any glibc works.
PLATFORM_TAGS = {
    ("linux", "amd64"): "manylinux_2_17_x86_64.manylinux2014_x86_64",
    ("linux", "arm64"): "manylinux_2_17_aarch64.manylinux2014_aarch64",
    ("darwin", "amd64"): "macosx_12_0_x86_64",
    ("darwin", "arm64"): "macosx_12_0_arm64",
}


def _go_env(key: str) -> str:
    return subprocess.run(["go", "env", key], capture_output=True, text=True, check=True).stdout.strip()


class CustomBuildHook(BuildHookInterface):
    def initialize(self, version: str, build_data: dict) -> None:
        repo_root = Path(__file__).parent.parent
        output = Path(__file__).parent / "hapiq" / "bin" / "hapiq"
        output.parent.mkdir(parents=True, exist_ok=True)

        goos, goarch = _go_env("GOOS"), _go_env("GOARCH")
        tag = PLATFORM_TAGS.get((goos, goarch))
        if tag is None:
            raise RuntimeError(f"no wheel platform for GOOS={goos} GOARCH={goarch}")

        # Stamp the package version so find_binary can tell a stale binary apart.
        ldflags = f"-s -w -X github.com/btraven00/hapiq/internal/version.Version={self.metadata.version}"
        env = {**os.environ, "CGO_ENABLED": "0"}
        cmd = ["go", "build", "-trimpath", "-ldflags", ldflags, "-o", str(output), "."]
        if subprocess.run(cmd, cwd=str(repo_root), env=env).returncode != 0:
            raise RuntimeError(
                f"go build failed (GOOS={goos}, GOARCH={goarch}). "
                "Make sure Go is installed: https://go.dev/dl/"
            )
        output.chmod(0o755)

        build_data["pure_python"] = False
        build_data["tag"] = f"py3-none-{tag}"
        build_data["artifacts"].append("hapiq/bin/hapiq")
        build_data["force_include"][str(output)] = "hapiq/bin/hapiq"
