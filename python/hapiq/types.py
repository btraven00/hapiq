from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


@dataclass
class Metadata:
    id: str = ""
    title: str = ""
    description: str = ""
    source: str = ""
    doi: str = ""
    version: str = ""
    license: str = ""
    authors: list[str] = field(default_factory=list)
    tags: list[str] = field(default_factory=list)
    keywords: list[str] = field(default_factory=list)
    organisms: list[str] = field(default_factory=list)
    file_count: int = 0
    total_size: int = 0
    last_modified: str = ""
    created: str = ""
    custom: dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, d: dict) -> "Metadata":
        return cls(
            id=d.get("id", ""),
            title=d.get("title", ""),
            description=d.get("description", ""),
            source=d.get("source", ""),
            doi=d.get("doi", ""),
            version=d.get("version", ""),
            license=d.get("license", ""),
            authors=d.get("authors") or [],
            tags=d.get("tags") or [],
            keywords=d.get("keywords") or [],
            organisms=d.get("organisms") or [],
            file_count=d.get("file_count", 0),
            total_size=d.get("total_size", 0),
            last_modified=d.get("last_modified", ""),
            created=d.get("created", ""),
            custom=d.get("custom") or {},
        )


@dataclass
class FileInfo:
    path: str = ""
    original_name: str = ""
    checksum: str = ""
    checksum_type: str = ""
    source_url: str = ""
    content_type: str = ""
    size: int = 0
    download_time: str = ""

    @classmethod
    def from_dict(cls, d: dict) -> "FileInfo":
        return cls(
            path=d.get("path", ""),
            original_name=d.get("original_name", ""),
            checksum=d.get("checksum", ""),
            checksum_type=d.get("checksum_type", ""),
            source_url=d.get("source_url", ""),
            content_type=d.get("content_type", ""),
            size=d.get("size", 0),
            download_time=d.get("download_time", ""),
        )


@dataclass
class DownloadResult:
    success: bool = False
    files: list[FileInfo] = field(default_factory=list)
    metadata: Metadata | None = None
    witness_file: str = ""
    bytes_total: int = 0
    bytes_downloaded: int = 0
    duration: int = 0
    errors: list[str] = field(default_factory=list)
    warnings: list[str] = field(default_factory=list)

    @classmethod
    def from_dict(cls, d: dict) -> "DownloadResult":
        meta = d.get("metadata")
        return cls(
            success=d.get("success", False),
            files=[FileInfo.from_dict(f) for f in (d.get("files") or [])],
            metadata=Metadata.from_dict(meta) if meta else None,
            witness_file=d.get("witness_file", ""),
            bytes_total=d.get("bytes_total", 0),
            bytes_downloaded=d.get("bytes_downloaded", 0),
            duration=d.get("duration", 0),
            errors=d.get("errors") or [],
            warnings=d.get("warnings") or [],
        )


@dataclass
class SearchResult:
    accession: str = ""
    title: str = ""
    organism: str = ""
    entry_type: str = ""
    dataset_type: str = ""
    date: str = ""
    sample_count: int = 0
    file_size: int = 0

    @classmethod
    def from_dict(cls, d: dict) -> "SearchResult":
        return cls(
            accession=d.get("accession", ""),
            title=d.get("title", ""),
            organism=d.get("organism", ""),
            entry_type=d.get("entry_type", ""),
            dataset_type=d.get("dataset_type", ""),
            date=d.get("date", ""),
            sample_count=d.get("sample_count", 0),
            file_size=d.get("file_size", 0),
        )
