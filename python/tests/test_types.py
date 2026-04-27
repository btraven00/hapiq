from hapiq.types import DownloadResult, FileInfo, Metadata, SearchResult


def test_metadata_from_dict():
    d = {
        "id": "GSE123",
        "title": "Test Dataset",
        "source": "geo",
        "file_count": 3,
        "total_size": 1024,
        "authors": ["Alice", "Bob"],
    }
    m = Metadata.from_dict(d)
    assert m.id == "GSE123"
    assert m.title == "Test Dataset"
    assert m.file_count == 3
    assert m.authors == ["Alice", "Bob"]


def test_metadata_from_dict_missing_fields():
    m = Metadata.from_dict({})
    assert m.id == ""
    assert m.file_count == 0
    assert m.authors == []


def test_fileinfo_from_dict():
    d = {"path": "/tmp/out/file.h5ad", "size": 512, "checksum": "abc123", "source_url": "https://example.com/f"}
    f = FileInfo.from_dict(d)
    assert f.path == "/tmp/out/file.h5ad"
    assert f.size == 512


def test_download_result_from_dict():
    d = {
        "success": True,
        "files": [{"path": "/tmp/f.h5ad", "size": 100}],
        "metadata": {"id": "GSE1", "title": "T", "source": "geo"},
        "witness_file": "/tmp/hapiq.json",
        "bytes_downloaded": 100,
    }
    r = DownloadResult.from_dict(d)
    assert r.success is True
    assert len(r.files) == 1
    assert r.metadata is not None
    assert r.metadata.id == "GSE1"


def test_search_result_from_dict():
    d = {"accession": "GSE99", "title": "Lung", "organism": "Homo sapiens", "sample_count": 10}
    s = SearchResult.from_dict(d)
    assert s.accession == "GSE99"
    assert s.sample_count == 10
