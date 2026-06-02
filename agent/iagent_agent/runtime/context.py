from __future__ import annotations

import asyncio
import logging
import time as _time
from dataclasses import dataclass, field
from typing import Optional

import httpx

from iagent_agent.adapter.protocol import JobResult, ProgressEmitter

logger = logging.getLogger(__name__)


class JobState:
    RUNNING = "RUNNING"
    SUCCEEDED = "SUCCEEDED"
    FAILED = "FAILED"
    CANCELLED = "CANCELLED"

    TERMINAL = {SUCCEEDED, FAILED, CANCELLED}


@dataclass
class JobRecord:
    job_id: str
    status: str = JobState.RUNNING
    percent: int = 0
    message: str = ""
    result: Optional[JobResult] = None
    error: Optional[str] = None
    event_seq: int = 0
    cancel_event: asyncio.Event = field(default_factory=asyncio.Event)
    finished_at: Optional[float] = None

    def to_dict(self) -> dict:
        d: dict = {
            "job_id": self.job_id,
            "status": self.status,
            "percent": self.percent,
            "message": self.message,
        }
        if self.result is not None:
            d["result"] = self.result.model_dump()
        if self.error is not None:
            d["error"] = self.error
        return d


class CallbackClient:
    def __init__(self, client: httpx.AsyncClient, callback_url: str, job_id: str):
        self._client = client
        self._callback_url = callback_url.rstrip("/")
        self._job_id = job_id

    async def post_event(self, record: JobRecord) -> None:
        payload: dict = {
            "event_seq": record.event_seq,
            "status": record.status,
            "percent": record.percent,
            "message": record.message,
            "ts": _time.time(),
        }
        if record.result is not None:
            payload["result"] = record.result.model_dump()
        if record.error is not None:
            payload["error"] = record.error
        if record.finished_at is not None:
            payload["finished_at"] = record.finished_at

        url = f"{self._callback_url}/jobs/{self._job_id}/events"
        try:
            resp = await self._client.post(url, json=payload, timeout=10)
            resp.raise_for_status()
        except Exception:
            logger.warning("callback POST failed for job=%s seq=%d", self._job_id, record.event_seq)


async def make_emit(record: JobRecord, callback: Optional[CallbackClient]) -> ProgressEmitter:
    async def emit(percent: int, message: str) -> None:
        record.percent = percent
        record.message = message
        if record.status == JobState.RUNNING:
            record.status = JobState.RUNNING
        if callback is not None:
            record.event_seq += 1
            await callback.post_event(record)

    return emit
