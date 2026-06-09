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
MAX_CHUNKS_IN_FLIGHT = 8
MAX_RETRIES = 3
CHUNK_ACK_TIMEOUT = 10.0


class FilePuller:
    def __init__(self, workspace_dir: Path, outbox: Outbox):
        self.workspace_dir = workspace_dir
        self.outbox = outbox
        self._events: dict[str, dict[int, asyncio.Event]] = {}
        self._ack_status: dict[str, dict[int, str]] = {}

    async def pull_outputs(self, job_id: str) -> list[str]:
        ws = self.workspace_dir / "workspaces" / job_id / "output"
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
        for attempt in range(MAX_RETRIES):
            try:
                await self._send_file_once(job_id, file_id, name, path)
                return
            except Exception:
                if attempt < MAX_RETRIES - 1:
                    logger.warning("pull retry %d/%d for file %s", attempt + 1, MAX_RETRIES, name)
                    await asyncio.sleep(1)
                else:
                    logger.error("pull failed after %d retries for file %s", MAX_RETRIES, name)
        self._cleanup_file_events(file_id)

    async def _send_file_once(self, job_id: str, file_id: str, name: str, path: Path):
        size = path.stat().st_size
        sha = self._sha256(path)
        total_chunks = max(1, (size + CHUNK_SIZE - 1) // CHUNK_SIZE)

        self._events[file_id] = {}
        self._ack_status[file_id] = {}

        await self.outbox.enqueue_and_send(FrameType.FILE_PULL_BEGIN, {
            "file_id": file_id,
            "job_id": job_id,
            "name": name,
            "size": size,
            "sha256": sha,
            "chunks": total_chunks,
        })

        with open(path, "rb") as f:
            inflight: list[int] = []
            for idx in range(total_chunks):
                # Backpressure: wait if inflight >= max
                while len(inflight) >= MAX_CHUNKS_IN_FLIGHT:
                    oldest = inflight.pop(0)
                    evt = self._events[file_id].get(oldest)
                    if evt is None:
                        self._events[file_id][oldest] = asyncio.Event()
                        evt = self._events[file_id][oldest]
                    try:
                        await asyncio.wait_for(evt.wait(), timeout=CHUNK_ACK_TIMEOUT)
                    except asyncio.TimeoutError:
                        raise RuntimeError(f"chunk {oldest} ack timeout")
                    status = self._ack_status[file_id].get(oldest, "")
                    if status == "ERROR":
                        raise RuntimeError(f"chunk {oldest} failed")

                data = f.read(CHUNK_SIZE)
                evt = asyncio.Event()
                self._events[file_id][idx] = evt
                inflight.append(idx)

                await self.outbox.enqueue_and_send(FrameType.FILE_PULL_CHUNK, {
                    "file_id": file_id,
                    "index": idx,
                    "data_b64": base64.b64encode(data).decode(),
                })

            # Wait for remaining chunks
            for idx in inflight:
                evt = self._events[file_id].get(idx)
                if evt is None:
                    self._events[file_id][idx] = asyncio.Event()
                    evt = self._events[file_id][idx]
                try:
                    await asyncio.wait_for(evt.wait(), timeout=CHUNK_ACK_TIMEOUT)
                except asyncio.TimeoutError:
                    raise RuntimeError(f"chunk {idx} ack timeout")
                status = self._ack_status[file_id].get(idx, "")
                if status == "ERROR":
                    raise RuntimeError(f"chunk {idx} failed")

        await self.outbox.enqueue_and_send(FrameType.FILE_PULL_END, {
            "file_id": file_id,
        })

        self._cleanup_file_events(file_id)

    def _cleanup_file_events(self, file_id: str):
        self._events.pop(file_id, None)
        self._ack_status.pop(file_id, None)

    def handle_pull_ack(self, payload: dict) -> None:
        file_id = payload.get("file_id", "")
        status = payload.get("status", "")
        chunk_index = payload.get("chunk_index", -1)

        if file_id not in self._events:
            return

        if chunk_index >= 0:
            self._ack_status.setdefault(file_id, {})[chunk_index] = status
        elif file_id in self._events:
            for idx, evt in self._events[file_id].items():
                self._ack_status.setdefault(file_id, {})[idx] = status
                evt.set()

    @staticmethod
    def _sha256(path: Path) -> str:
        h = hashlib.sha256()
        with open(path, "rb") as f:
            for chunk in iter(lambda: f.read(65536), b""):
                h.update(chunk)
        return h.hexdigest()
