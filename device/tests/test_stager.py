"""Unit tests for file stager — chunk assembly and SHA-256 verification."""

import hashlib
import base64
import pytest

from iagent_device.files.stager import FileStager


class TestFileStager:
    @pytest.fixture
    def stager(self, tmp_workspace, file_repo, outbox):
        return FileStager(tmp_workspace, file_repo, outbox)

    @pytest.mark.asyncio
    async def test_begin_creates_workspace_and_buf(self, stager, tmp_workspace):
        payload = {
            "file_id": "f1", "job_id": "j1", "file_name": "test.txt",
            "sha256": "", "total_chunks": 2, "size_bytes": 10,
        }
        await stager.handle_begin(payload)

        ws = tmp_workspace / "j1" / "inputs"
        assert ws.exists()
        assert stager._bufs["f1"] is not None

    @pytest.mark.asyncio
    async def test_chunk_appends_data(self, stager, tmp_workspace):
        await stager.handle_begin({
            "file_id": "f2", "job_id": "j2", "file_name": "data.bin",
            "sha256": "abc", "total_chunks": 2, "size_bytes": 10,
        })

        chunk1 = base64.b64encode(b"hello").decode()
        chunk2 = base64.b64encode(b"world").decode()

        await stager.handle_chunk({"file_id": "f2", "data": chunk1})
        await stager.handle_chunk({"file_id": "f2", "data": chunk2})

        buf = stager._bufs["f2"]
        assert buf["chunks"] == 2
        assert buf["hasher"].hexdigest() == hashlib.sha256(b"helloworld").hexdigest()

    @pytest.mark.asyncio
    async def test_end_sha256_match(self, stager):
        data = b"test content"
        sha = hashlib.sha256(data).hexdigest()

        await stager.handle_begin({
            "file_id": "f3", "job_id": "j3", "file_name": "content.txt",
            "sha256": sha, "total_chunks": 1, "size_bytes": len(data),
        })
        await stager.handle_chunk({"file_id": "f3", "data": base64.b64encode(data).decode()})

        await stager.handle_end({"file_id": "f3"})

        assert "f3" not in stager._bufs

    @pytest.mark.asyncio
    async def test_end_sha256_mismatch(self, stager, outbox_repo):
        data = b"test content"
        wrong_sha = "deadbeef" * 8

        await stager.handle_begin({
            "file_id": "f4", "job_id": "j4", "file_name": "wrong.txt",
            "sha256": wrong_sha, "total_chunks": 1, "size_bytes": len(data),
        })
        await stager.handle_chunk({"file_id": "f4", "data": base64.b64encode(data).decode()})
        await stager.handle_end({"file_id": "f4"})

        unacked = outbox_repo.list_unacked()
        ack_frames = [u for u in unacked if u["type"] == "FILE_ACK"]
        assert any("error" in u["payload"].lower() for u in ack_frames)

    @pytest.mark.asyncio
    async def test_cleanup_removes_workspace(self, stager, tmp_workspace):
        ws = tmp_workspace / "j5" / "inputs"
        ws.mkdir(parents=True, exist_ok=True)
        (ws / "f.txt").write_text("data")

        await stager.cleanup("j5")
        assert not (tmp_workspace / "j5").exists()

    @pytest.mark.asyncio
    async def test_chunk_ignored_without_begin(self, stager):
        await stager.handle_chunk({"file_id": "noexist", "data": "dGVzdA=="})
