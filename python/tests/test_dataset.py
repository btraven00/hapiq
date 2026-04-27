import json
from pathlib import Path
from unittest.mock import patch

import pytest

from hapiq import Dataset, HapiqError
from hapiq.types import Metadata


METADATA_RESPONSE = {
    "success": True,
    "metadata": {
        "id": "GSE133344",
        "title": "Single cell RNA-seq",
        "source": "geo",
        "file_count": 2,
        "total_size": 2048,
    },
    "files": [],
}

DOWNLOAD_RESPONSE = {
    "success": True,
    "metadata": {"id": "GSE133344", "title": "Single cell RNA-seq", "source": "geo"},
    "files": [
        {"path": "/tmp/out/file1.h5ad", "size": 1024},
        {"path": "/tmp/out/file2.csv.gz", "size": 1024},
    ],
    "witness_file": "/tmp/out/hapiq.json",
    "bytes_downloaded": 2048,
}


def _mock_run(args, **kwargs):
    return METADATA_RESPONSE if "--dry-run" in args else DOWNLOAD_RESPONSE


def test_dataset_repr():
    ds = Dataset("geo", "GSE133344")
    assert repr(ds) == "Dataset(source='geo', id='GSE133344')"


def test_metadata_cached():
    ds = Dataset("geo", "GSE133344")
    with patch("hapiq.dataset.run", side_effect=_mock_run) as mock_run:
        m1 = ds.metadata
        m2 = ds.metadata
        assert mock_run.call_count == 1  # cached after first call
    assert isinstance(m1, Metadata)
    assert m1.id == "GSE133344"
    assert m1.file_count == 2


def test_fetch_returns_paths():
    ds = Dataset("geo", "GSE133344")
    with patch("hapiq.dataset.run", side_effect=_mock_run):
        paths = ds.fetch("/tmp/out")
    assert len(paths) == 2
    assert all(isinstance(p, Path) for p in paths)
    assert paths[0] == Path("/tmp/out/file1.h5ad")


def test_fetch_passes_filters():
    ds = Dataset("geo", "GSE133344", include_ext=[".h5ad"], organism="Homo sapiens", parallel=4)
    with patch("hapiq.dataset.run", side_effect=_mock_run) as mock_run:
        ds.fetch("/tmp/out")
    args = mock_run.call_args[0][0]
    assert "--include-ext" in args
    assert ".h5ad" in args
    assert "--organism" in args
    assert "Homo sapiens" in args
    assert "--parallel" in args
    assert "4" in args


def test_fetch_skip_existing_default():
    ds = Dataset("geo", "GSE133344")
    with patch("hapiq.dataset.run", side_effect=_mock_run) as mock_run:
        ds.fetch("/tmp/out")
    args = mock_run.call_args[0][0]
    assert "--skip-existing" in args


def test_hapiq_error_propagates():
    ds = Dataset("geo", "INVALID")
    with patch("hapiq.dataset.run", side_effect=HapiqError("dataset not found")):
        with pytest.raises(HapiqError, match="dataset not found"):
            ds.fetch("/tmp/out")
