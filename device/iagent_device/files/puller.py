import asyncio
import base64
import hashlib
import logging
import uuid
from pathlib import Path

from iagent_device.tunnel.codec import FrameType
from iagent_device.tunnel.outbox import Outbox

logger = logging.getLogger(__name__)

CHUNK_SIZE = 256 * 1024


class FilePuller:
    def __init__(self, workspace_dir: Path, outbox: Outbox):
        self.workspace_dir = workspace_dir
        self.outbox = outbox
        self._pending: dict[str, asyncio.Event] = {}

    async def pull_outputs(self, job_id: str) -> list[str]:
        ws = self.workspace_dir / job_id
        if not ws.exists():
            return []

        relayed: list[str] = []
        for path in sorted(ws.rglob("*")):
            if not path.is_file():
                continue
            rel = path.relative_to(ws)
            if rel.parts and rel.parts[0] == "inputs":
                continue

            file_id = str(uuid.uuid4())
            await self._send_file(job_id, file_id, str(rel), path)
            relayed.append(str(rel))

        return relayed

    async def _send_file(self, job_id: str, file_id: str, name: str, path: Path):
        size = path.stat().st_size
        sha = self._sha256(path)
        total_chunks = max(1, (size + CHUNK_SIZE - 1) // CHUNK_SIZE)

        await self.outbox.enqueue_and_send(FrameType.FILE_PULL_BEGIN, {
            "file_id": file_id,
            "job_id": job_id,
            "name": name,
            "size": size,
            "sha256": sha,
            "chunks": total_chunks,
        })

        with open(path, "rb") as f:
            for idx in range(total_chunks):
                data = f.read(CHUNK_SIZE)
            await self.outbox.enqueue_and_send(FrameType.FILE_PULL_CHUNK, {
                    "file_id": file_id,
                    "index": idx,
                    "data_b64": base64.b64encode(data).decode(),
                })

        await self.outbox.enqueue_and_send(FrameType.FILE_PULL_END, {
            "file_id": file_id,
        })

    def handle_pull_ack(self, payload: dict) -> None:
        file_id = payload.get("file_id", "")
        event = self._pending.pop(file_id, None)
        if event:
            event.set()

    @staticmethod
    def _sha256(path: Path) -> str:
        h = hashlib.sha256()
        with open(path, "rb") as f:
            for chunk in iter(lambda: f.read(65536), b""):
                h.update(chunk)
        return h.hexdigest()
