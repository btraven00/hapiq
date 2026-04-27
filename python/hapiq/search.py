from __future__ import annotations

from ._runner import run
from .types import SearchResult


def search(
    source: str,
    query: str,
    *,
    organism: str | None = None,
    entry_type: str | None = None,
    limit: int = 0,
    timeout: int = 60,
) -> list[SearchResult]:
    args = ["search", source, query, "--output", "json"]
    if organism:
        args += ["--organism", organism]
    if entry_type:
        args += ["--entry-type", entry_type]
    if limit:
        args += ["--limit", str(limit)]
    data = run(args, timeout=timeout)
    if not isinstance(data, list):
        return []
    return [SearchResult.from_dict(r) for r in data]
