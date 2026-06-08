"""Durable outbox for at-least-once delivery.
Progress/results are written to SQLite outbox before sending;
removed only after cloud ACK. Survives restarts and tunnel drops.
"""

import asyncio
import json
import logging

from iagent_device.store.repositories import OutboxRepo
from iagent_device.tunnel.codec import FrameType, new_msg_id

logger = logging.getLogger(__name__)


class Outbox:
    def __init__(self, repo: OutboxRepo, send_fn):
        self.repo = repo
        self.send_fn = send_fn  # async or sync function to send a frame

    async def enqueue_and_send(self, frame_type: FrameType, payload: dict):
        msg_id = new_msg_id()
        self.repo.enqueue(msg_id, str(frame_type), payload)
        result = self.send_fn(frame_type, payload, msg_id=msg_id)
        if asyncio.iscoroutine(result):
            await result

    async def flush(self):
        """Flush all unacknowledged outbox entries."""
        for entry in self.repo.list_unacked():
            try:
                result = self.send_fn(
                    FrameType(entry["type"]),
                    json.loads(entry["payload"]),
                    msg_id=entry["msg_id"],
                )
                if asyncio.iscoroutine(result):
                    await result
            except Exception:
                logger.exception("outbox flush failed for msg_id=%s", entry["msg_id"])

    def ack(self, msg_id: str):
        self.repo.mark_acked(msg_id)
