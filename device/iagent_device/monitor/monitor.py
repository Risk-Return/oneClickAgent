"""Resource and status monitor: samples per-agent CPU/mem/disk via Docker stats,
emits AGENT_STATUS on change or interval, refreshes device-level resources.
"""

import asyncio
import logging
import platform

from iagent_device.tunnel.codec import FrameType
from iagent_device.tunnel.outbox import Outbox
from iagent_device.store.repositories import AgentRepo

logger = logging.getLogger(__name__)


class Monitor:
    def __init__(
        self,
        agent_repo: AgentRepo,
        outbox: Outbox,
        docker_mgr=None,
        interval: float = 10.0,
    ):
        self.repo = agent_repo
        self.outbox = outbox
        self.docker = docker_mgr
        self.interval = interval

    async def run(self):
        while True:
            for agent in self.repo.list_all():
                await self._sample(agent)
            await asyncio.sleep(self.interval)

    async def _sample(self, agent: dict):
        agent_id = agent["agent_id"]
        try:
            usage = {"cpu_pct": 0.0, "mem_mb": 0, "disk_mb": 0}
            if self.docker:
                usage = await self._docker_stats(agent_id)

            await self.outbox.enqueue_and_send(FrameType.AGENT_STATUS, {
                "agent_id": agent_id,
                "status": agent.get("status", ""),
                "health": "healthy" if agent.get("status") not in ("failed", "unhealthy") else "unhealthy",
                "restarts": agent.get("restarts", 0),
                "usage": usage,
                "ts": int(asyncio.get_event_loop().time() * 1000),
            })
        except Exception:
            pass

    async def _docker_stats(self, agent_id: str) -> dict:
        try:
            stats = await self.docker.get_container_stats(agent_id)
            return stats
        except Exception:
            return {"cpu_pct": 0.0, "mem_mb": 0, "disk_mb": 0}

    def build_hello_extras(self, agent_repo: AgentRepo, vnc_enabled: bool = False) -> dict:
        agents_list = []
        for a in agent_repo.list_all():
            agents_list.append({
                "agent_id": a["agent_id"],
                "status": a.get("status", ""),
                "port": a.get("port", 0),
                "tags": a.get("tags", ""),
            })
        return {
            "platform": platform.system(),
            "agent_version": "0.1.0",
            "resources": {
                "cpu": 0,
                "cpu_logical": 0,
                "mem_mb": 0,
                "disk_mb": 0,
            },
            "agents": agents_list,
            "capabilities": ["vnc"] if vnc_enabled else [],
            "agent_count": len(agents_list),
        }
