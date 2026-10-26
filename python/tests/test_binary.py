import pytest

from hapiq import __version__, _binary


def _fake_binary(tmp_path, stdout):
    exe = tmp_path / "hapiq"
    exe.write_text(f"#!/bin/sh\necho '{stdout}'\n")
    exe.chmod(0o755)
    return exe


@pytest.fixture(autouse=True)
def _clear_cache():
    _binary.find_binary.cache_clear()
    yield
    _binary.find_binary.cache_clear()


def test_bundled_binary_matching_version(tmp_path, monkeypatch):
    exe = _fake_binary(tmp_path, f"hapiq {__version__}")
    monkeypatch.setattr(_binary, "BUNDLED", exe)
    assert _binary.find_binary() == str(exe)


def test_bundled_binary_stale_version(tmp_path, monkeypatch):
    monkeypatch.setattr(_binary, "BUNDLED", _fake_binary(tmp_path, "hapiq 0.0.1"))
    with pytest.raises(RuntimeError, match="bundled hapiq binary is 0.0.1"):
        _binary.find_binary()
