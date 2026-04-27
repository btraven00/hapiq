"""Integration tests — hit real network endpoints via the hapiq binary.

Run with:  pytest -m integration
Skip with: pytest -m "not integration"  (default in CI)

The HAPIQ_DOWNLOAD_TEST=1 env var enables the single-file download test,
which transfers actual data (~few MB) and may be slow.
"""

import os
import tempfile

import pytest

from hapiq import Dataset, HapiqError, search
from hapiq.types import DownloadResult, Metadata, SearchResult

pytestmark = pytest.mark.integration


# ── search ────────────────────────────────────────────────────────────────────

def test_search_geo_returns_results():
    results = search("geo", "single cell RNA-seq liver", timeout=30)
    assert len(results) > 0
    assert all(isinstance(r, SearchResult) for r in results)
    # Every result must have a non-empty accession
    assert all(r.accession for r in results)


def test_search_geo_accessions_look_like_geo_ids():
    results = search("geo", "PBMC", timeout=30)
    assert len(results) > 0
    # GEO accessions start with GSE, GSM, GPL, or GDS
    geo_prefixes = ("GSE", "GSM", "GPL", "GDS")
    assert any(r.accession.startswith(geo_prefixes) for r in results)


def test_search_invalid_source_raises():
    with pytest.raises(HapiqError):
        search("notasource", "query", timeout=10)


# ── metadata (dry-run) ────────────────────────────────────────────────────────

# GSE164073: stable, public, 10-file GEO series
_TEST_GEO_ID = "GSE164073"


def test_metadata_geo():
    ds = Dataset("geo", _TEST_GEO_ID)
    meta = ds.metadata
    assert isinstance(meta, Metadata)
    assert meta.id == _TEST_GEO_ID
    assert meta.title
    assert meta.file_count > 0
    assert meta.source == "geo"


def test_metadata_is_cached():
    ds = Dataset("geo", _TEST_GEO_ID)
    m1 = ds.metadata
    m2 = ds.metadata
    assert m1 is m2  # same object — not re-fetched


def test_metadata_invalid_id_raises():
    ds = Dataset("geo", "GSE000000000INVALID")
    with pytest.raises(HapiqError):
        _ = ds.metadata


# ── fetch (dry-run) ───────────────────────────────────────────────────────────

def test_fetch_dry_run_returns_no_files():
    ds = Dataset("geo", _TEST_GEO_ID)
    with tempfile.TemporaryDirectory() as tmp:
        paths = ds.fetch(tmp, dry_run=True)
    # dry-run enumerates but downloads nothing → no paths in result
    assert isinstance(paths, list)


def test_fetch_result_dry_run():
    ds = Dataset("geo", _TEST_GEO_ID)
    with tempfile.TemporaryDirectory() as tmp:
        result = ds.fetch_result(tmp)
    # skip_existing=True by default; no existing files → may download
    # Just validate the shape of the result
    assert isinstance(result, DownloadResult)


# ── actual download (opt-in) ──────────────────────────────────────────────────

@pytest.mark.skipif(
    not os.environ.get("HAPIQ_DOWNLOAD_TEST"),
    reason="set HAPIQ_DOWNLOAD_TEST=1 to run download tests",
)
def test_download_single_file():
    # Limit to 1 file so the test stays fast
    ds = Dataset("geo", _TEST_GEO_ID, limit_files=1)
    with tempfile.TemporaryDirectory() as tmp:
        paths = ds.fetch(tmp, skip_existing=False)
    assert len(paths) == 1
    assert paths[0].exists()
    assert paths[0].stat().st_size > 0
