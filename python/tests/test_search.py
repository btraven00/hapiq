from unittest.mock import patch

from hapiq import search
from hapiq.types import SearchResult

SEARCH_RESPONSE = [
    {"accession": "GSE133344", "title": "Lung cancer scRNA", "organism": "Homo sapiens", "sample_count": 5},
    {"accession": "GSE200001", "title": "PBMC atlas", "organism": "Mus musculus", "sample_count": 10},
]


def test_search_returns_results():
    with patch("hapiq.search.run", return_value=SEARCH_RESPONSE):
        results = search("geo", "lung cancer")
    assert len(results) == 2
    assert all(isinstance(r, SearchResult) for r in results)
    assert results[0].accession == "GSE133344"


def test_search_passes_args():
    with patch("hapiq.search.run", return_value=SEARCH_RESPONSE) as mock_run:
        search("geo", "lung cancer", organism="Homo sapiens", limit=5)
    args = mock_run.call_args[0][0]
    assert "search" in args
    assert "geo" in args
    assert "lung cancer" in args
    assert "--organism" in args
    assert "Homo sapiens" in args
    assert "--limit" in args
    assert "5" in args


def test_search_empty():
    with patch("hapiq.search.run", return_value=[]):
        results = search("geo", "zzz_no_results")
    assert results == []


def test_search_non_list_response():
    with patch("hapiq.search.run", return_value={"error": "unexpected"}):
        results = search("geo", "bad")
    assert results == []
