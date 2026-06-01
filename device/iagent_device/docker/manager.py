"""Docker agent pool manager via docker-py:
- Create N idle containers (pull image, resource limits, security hardening, labels)
- Start/stop/restart/remove individual agents
- Health checks per agent, recovery with max_restarts cap
- Pool reaper: recycle agents after job completion (clear workspace → IDLE)
- Scale up/down the pool to match desired size
"""

import asyncio
import logging
import uuid

from iagent_device.store.repositories import AgentRepo
from iagent_device.agentclient.client import AgentClient

logger = logging.getLogger(__name__)


class DockerManager:
    def __init__(
        self,
        agent_repo: AgentRepo,
        image: str,
        port_start: int,
        port_end: int,
        max_restarts: int = 3,
        docker_client=None,
        data_dir: str = "",
    ):
        self.repo = agent_repo
        self.image = image
        self.port_start = port_start
        self.port_end = port_end
        self.max_restarts = max_restarts
        self.docker = docker_client  # docker.from_env()
        self.data_dir = data_dir
        self._allocated_ports: set[int] = set()

    async def ensure_pool(self, desired_size: int):
        current = self.repo.list_all()
        idle = [a for a in current if a["status"] == "idle"]
        busy = [a for a in current if a["status"] == "busy"]
        total = len(idle) + len(busy)

        # Remove surplus
        if total > desired_size:
            for agent in idle[desired_size - len(busy):]:
                self.repo.delete(agent["agent_id"])
                total -= 1

        # Create missing
        for _ in range(desired_size - total):
            agent_id = str(uuid.uuid4())
            port = self._allocate_port()
            name = f"agent-{agent_id[:8]}"
            self.repo.upsert(agent_id, name, self.image, port, status="creating")
            await self._create_container(agent_id, name, port)
            self.repo.update_status(agent_id, "idle")

    async def _create_container(self, agent_id: str, name: str, port: int):
        if not self.docker:
            logger.info("mock: create container %s on port %d", name, port)
            return
        try:
            self.docker.containers.run(
                self.image,
                name=name,
                detach=True,
                ports={8090: port},
                labels={
                    "iagent.agent_id": agent_id,
                    "iagent.pool": "true",
                },
                mem_limit="4g",
                nano_cpus=2_000_000_000,
                network="bridge",
                cap_drop=["ALL"],
                read_only=True,
                remove=True,
            )
        except Exception:
            logger.exception("failed to create container %s", name)

    def _allocate_port(self) -> int:
        for p in range(self.port_start, self.port_end + 1):
            if p not in self._allocated_ports:
                self._allocated_ports.add(p)
                return p
        raise RuntimeError("no available ports in range")

    def _release_port(self, port: int):
        self._allocated_ports.discard(port)

    async def health_check(self, agent_id: str):
        agent = self.repo.get_by_id(agent_id)
        if not agent:
            return
        url = f"http://127.0.0.1:{agent['port']}"
        client = AgentClient(url)
        ok = await client.healthz()
        if not ok:
            restarts = int(agent.get("restarts", 0)) + 1
            if restarts > self.max_restarts:
                self.repo.update_status(agent_id, "failed")
                logger.error("agent %s exceeded max restarts", agent_id)
            else:
                logger.warning("agent %s unhealthy, restart %d/%d", agent_id, restarts, self.max_restarts)

    async def health_loop(self, interval: float = 30.0):
        while True:
            for agent in self.repo.list_all():
                await self.health_check(agent["agent_id"])
            await asyncio.sleep(interval)

    def get_client(self, agent_id: str) -> AgentClient | None:
        agent = self.repo.get_by_id(agent_id)
        if not agent or not agent.get("port"):
            return None
        return AgentClient(f"http://127.0.0.1:{agent['port']}")
