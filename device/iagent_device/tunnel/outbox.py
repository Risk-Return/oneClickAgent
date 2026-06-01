"""Durable outbox for at-least-once delivery.
Progress/results are written to SQLite outbox before sending;
removed only after cloud ACK. Survives restarts and tunnel drops.
"""

import json
import asyncio
import logging

from iagent_device.store.repositories import OutboxRepo
from iagent_device.tunnel.codec import FrameType, new_msg_id

logger = logging.getLogger(__name__)


class Outbox:
    def __init__(self, repo: OutboxRepo, send_fn):
        self.repo = repo
        self.send_fn = send_fn  # async function to send a frame

    async def enqueue_and_send(self, frame_type: FrameType, payload: dict):
        msg_id = new_msg_id()
        self.repo.enqueue(msg_id, str(frame_type), payload)
        await self.send_fn(msg_id, frame_type, payload)

    async def flush(self):
        """Flush all unacknowledged outbox entries."""
        for entry in self.repo.list_unacked():
            try:
                await self.send_fn(
                    entry["msg_id"],
                    FrameType(entry["type"]),
                    json.loads(entry["payload"]),
                )
            except Exception:
                logger.exception("outbox flush failed for msg_id=%s", entry["msg_id"])

    def ack(self, msg_id: str):
        self.repo.mark_acked(msg_id)
