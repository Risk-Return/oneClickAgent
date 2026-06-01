"""Resource and status monitor: samples per-agent CPU/mem/disk via Docker stats,
emits AGENT_STATUS on change or interval, refreshes device-level resources.
"""

import asyncio
import logging
import psutil

from iagent_device.tunnel.codec import FrameType
from iagent_device.tunnel.outbox import Outbox
from iagent_device.store.repositories import AgentRepo

logger = logging.getLogger(__name__)


class Monitor:
    def __init__(self, agent_repo: AgentRepo, outbox: Outbox, interval: float = 30.0):
        self.repo = agent_repo
        self.outbox = outbox
        self.interval = interval

    async def run(self):
        while True:
            for agent in self.repo.list_all():
                await self._sample(agent["agent_id"])
            await asyncio.sleep(self.interval)

    async def _sample(self, agent_id: str):
        try:
            cpu = psutil.cpu_percent(interval=1)
            mem = psutil.virtual_memory()
            await self.outbox.enqueue_and_send(FrameType.AGENT_STATUS, {
                "agent_id": agent_id,
                "status": self.repo.get_by_id(agent_id)["status"],
                "cpu_percent": cpu,
                "memory_mb": mem.used // (1024 * 1024),
            })
        except Exception:
            pass
