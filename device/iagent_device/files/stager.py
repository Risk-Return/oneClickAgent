"""File stager: receives FILE_PUSH_*/CHUNK/END, verifies sha256,
writes to per-job workspace, mounts inputs read-only into agent container,
and cleans up workspace on job terminal state.
"""

import hashlib
import logging
from pathlib import Path

from iagent_device.tunnel.codec import FrameType
from iagent_device.tunnel.outbox import Outbox
from iagent_device.store.repositories import FileRepo

logger = logging.getLogger(__name__)


class FileStager:
    def __init__(self, workspace_dir: Path, file_repo: FileRepo, outbox: Outbox):
        self.workspace_dir = workspace_dir
        self.repo = file_repo
        self.outbox = outbox
        self._bufs: dict[str, dict] = {}  # file_id → {file, sha256, total_chunks, chunks_received}

    async def handle_begin(self, payload: dict):
        file_id = payload["file_id"]
        job_id = payload.get("job_id", "")
        name = payload["file_name"]
        sha256 = payload.get("sha256", "")
        total_chunks = payload.get("total_chunks", 0)
        size = payload.get("size_bytes", 0)

        ws_dir = self.workspace_dir / job_id / "inputs"
        ws_dir.mkdir(parents=True, exist_ok=True)

        f = open(ws_dir / name, "wb")
        self._bufs[file_id] = {
            "file": f,
            "sha256": sha256,
            "path": str(ws_dir / name),
            "job_id": job_id,
            "name": name,
            "size": size,
            "total_chunks": total_chunks,
            "chunks": 0,
            "hasher": hashlib.sha256(),
        }
        self.repo.create(file_id, job_id, name, size, sha256, str(ws_dir / name))

    async def handle_chunk(self, payload: dict):
        file_id = payload["file_id"]
        buf = self._bufs.get(file_id)
        if not buf:
            return
        import base64
        data = base64.b64decode(payload["data"])
        buf["file"].write(data)
        buf["hasher"].update(data)
        buf["chunks"] += 1

    async def handle_end(self, payload: dict):
        file_id = payload["file_id"]
        buf = self._bufs.pop(file_id, None)
        if not buf:
            return
        buf["file"].close()
        actual = buf["hasher"].hexdigest()
        expected = buf["sha256"]
        if actual != expected:
            await self.outbox.enqueue_and_send(FrameType.FILE_ACK, {
                "file_id": file_id,
                "status": "error",
                "error": f"SHA-256 mismatch: expected {expected}, got {actual}",
            })
        else:
            await self.outbox.enqueue_and_send(FrameType.FILE_ACK, {
                "file_id": file_id,
                "status": "staged_device",
            })

    async def cleanup(self, job_id: str):
        ws_dir = self.workspace_dir / job_id
        if ws_dir.exists():
            import shutil
            shutil.rmtree(ws_dir)
        for f in self.repo.list_by_job(job_id):
            self.repo.purge(f["file_id"])
