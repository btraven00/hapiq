from __future__ import annotations

import tempfile
from pathlib import Path
from typing import Any

from ._runner import run
from .types import DownloadResult, Metadata


class Dataset:
    """HDL (Has Data Locally) — lazy dataset handle. Downloads on first fetch()."""

    def __init__(
        self,
        source: str,
        id: str,
        *,
        include_ext: list[str] | None = None,
        exclude_ext: list[str] | None = None,
        max_file_size: str | None = None,
        filename_pattern: str | None = None,
        organism: str | None = None,
        subset: list[str] | None = None,
        exclude_raw: bool = False,
        exclude_supplementary: bool = False,
        parallel: int = 8,
        timeout: int = 300,
        limit_files: int = 0,
    ) -> None:
        self.source = source
        self.id = id
        self.include_ext = include_ext
        self.exclude_ext = exclude_ext
        self.max_file_size = max_file_size
        self.filename_pattern = filename_pattern
        self.organism = organism
        self.subset = subset
        self.exclude_raw = exclude_raw
        self.exclude_supplementary = exclude_supplementary
        self.parallel = parallel
        self.timeout = timeout
        self.limit_files = limit_files
        self._metadata: Metadata | None = None

    @property
    def metadata(self) -> Metadata:
        if self._metadata is None:
            with tempfile.TemporaryDirectory() as tmp:
                args = self._build_args(tmp, dry_run=True)
                data = run(args, timeout=self.timeout)
                meta = data.get("metadata") if isinstance(data, dict) else None
                self._metadata = Metadata.from_dict(meta) if meta else Metadata()
        return self._metadata

    def fetch(
        self,
        out_dir: str | Path,
        *,
        resume: bool = False,
        skip_existing: bool = True,
        dry_run: bool = False,
    ) -> list[Path]:
        args = self._build_args(str(out_dir), dry_run=dry_run)
        if resume:
            args.append("--resume")
        if skip_existing:
            args.append("--skip-existing")
        data = run(args, timeout=self.timeout)
        files = data.get("files") or [] if isinstance(data, dict) else []
        return [Path(f["path"]) for f in files if f.get("path")]

    def fetch_result(
        self,
        out_dir: str | Path,
        *,
        resume: bool = False,
        skip_existing: bool = True,
    ) -> DownloadResult:
        args = self._build_args(str(out_dir), dry_run=False)
        if resume:
            args.append("--resume")
        if skip_existing:
            args.append("--skip-existing")
        data = run(args, timeout=self.timeout)
        return DownloadResult.from_dict(data if isinstance(data, dict) else {})

    def _build_args(self, out_dir: str, dry_run: bool) -> list[str]:
        args = ["download", self.source, self.id, "--out", out_dir, "--output", "json", "--yes", "--quiet"]
        if dry_run:
            args.append("--dry-run")
        if self.include_ext:
            args += ["--include-ext", ",".join(self.include_ext)]
        if self.exclude_ext:
            args += ["--exclude-ext", ",".join(self.exclude_ext)]
        if self.max_file_size:
            args += ["--max-file-size", self.max_file_size]
        if self.filename_pattern:
            args += ["--filename-pattern", self.filename_pattern]
        if self.organism:
            args += ["--organism", self.organism]
        if self.subset:
            args += ["--subset", ",".join(self.subset)]
        if self.exclude_raw:
            args.append("--exclude-raw")
        if self.exclude_supplementary:
            args.append("--exclude-supplementary")
        if self.parallel != 8:
            args += ["--parallel", str(self.parallel)]
        if self.limit_files:
            args += ["--limit-files", str(self.limit_files)]
        return args

    def __repr__(self) -> str:
        return f"Dataset(source={self.source!r}, id={self.id!r})"
