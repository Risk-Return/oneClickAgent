"""Docker agent pool manager via docker-py:
- Create N idle containers (pull image, resource limits, security hardening, labels)
- Start/stop/restart/remove individual agents
- Health checks per agent, recovery with max_restarts cap
- Pool reaper: recycle agents after job completion (clear workspace -> IDLE)
- Scale up/down the pool to match desired size
"""

import asyncio
import logging
import shutil
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
        agent_env: dict | None = None,
    ):
        self.repo = agent_repo
        self.image = image
        self.port_start = port_start
        self.port_end = port_end
        self.max_restarts = max_restarts
        self.docker = docker_client
        self.data_dir = data_dir
        self.agent_env = agent_env or {}
        self._allocated_ports: set[int] = set()

    async def prepull_image(self):
        if not self.docker:
            logger.info("mock: pull image %s", self.image)
            return
        try:
            logger.info("pulling image %s ...", self.image)
            self.docker.images.pull(self.image)
            logger.info("image %s pulled", self.image)
        except Exception:
            logger.exception("failed to pull image %s", self.image)

    async def ensure_pool(self, desired_size: int):
        current = self.repo.list_all()
        idle = [a for a in current if a["status"] == "idle"]
        busy = [a for a in current if a["status"] == "busy"]
        total = len(idle) + len(busy)

        if total > desired_size:
            surplus = idle[max(0, desired_size - len(busy)):]
            for agent in surplus:
                self._remove_container(agent)
                self.repo.delete(agent["agent_id"])
                self._release_port(agent["port"])
                total -= 1

        for _ in range(desired_size - total):
            agent_id = str(uuid.uuid4())
            port = self._allocate_port()
            name = f"agent-{agent_id[:12]}"
            self.repo.upsert(agent_id, name, self.image, port, status="creating")
            container_id = await self._create_container(agent_id, name, port)
            self.repo.update_status(agent_id, "idle", container_id=container_id or "")

    async def create_agent_with_id(self, agent_id: str):
        existing = self.repo.get_by_id(agent_id)
        if existing:
            logger.info("agent %s already exists, skipping", agent_id)
            return
        port = self._allocate_port()
        name = f"agent-{agent_id[:16]}"
        self.repo.upsert(agent_id, name, self.image, port, status="creating")
        container_id = await self._create_container(agent_id, name, port)
        self.repo.update_status(agent_id, "idle", container_id=container_id or "")

    async def _create_container(self, agent_id: str, name: str, port: int) -> str | None:
        if not self.docker:
            logger.info("mock: create container %s on port %d", name, port)
            return None
        try:
            workspace_mount = f"{self.data_dir}/work:/work:rw"
            container = self.docker.containers.run(
                self.image,
                name=name,
                detach=True,
                ports={"8090/tcp": port},
                labels={
                    "iagent.agent_id": agent_id,
                    "iagent.pool": "true",
                },
                mem_limit="4g",
                nano_cpus=2_000_000_000,
                network="bridge",
                cap_drop=["ALL"],
                read_only=True,
                remove=False,
                volumes=[workspace_mount],
                tmpfs={"/tmp": "exec", "/run": "exec,rw"},
                pids_limit=256,
                environment=self.agent_env,
            )
            return container.id
        except Exception:
            logger.exception("failed to create container %s", name)
            return None

    def _remove_container(self, agent: dict):
        if not self.docker or not agent.get("container_id"):
            return
        try:
            c = self.docker.containers.get(agent["container_id"])
            c.stop(timeout=5)
            c.remove(force=True)
        except Exception:
            pass

    def _allocate_port(self) -> int:
        for p in range(self.port_start, self.port_end + 1):
            if p not in self._allocated_ports:
                self._allocated_ports.add(p)
                return p
        raise RuntimeError("no available ports in range")

    def _release_port(self, port: int):
        self._allocated_ports.discard(port)

    def get_container_ip(self, agent_id: str) -> str:
        agent = self.repo.get_by_id(agent_id)
        if not agent or not self.docker:
            return "127.0.0.1"
        try:
            c = self.docker.containers.get(agent["container_id"])
            nets = c.attrs.get("NetworkSettings", {}).get("Networks", {})
            for net in nets.values():
                ip = net.get("IPAddress", "")
                if ip:
                    return ip
        except Exception:
            pass
        return "127.0.0.1"

    async def health_check(self, agent_id: str):
        agent = self.repo.get_by_id(agent_id)
        if not agent:
            return
        if agent.get("status") == "creating":
            return
        url = f"http://127.0.0.1:{agent['port']}"
        client = AgentClient(url)
        ok = await client.healthz()
        if not ok:
            restarts = int(agent.get("restarts", 0)) + 1
            self.repo.increment_restarts(agent_id)
            if restarts > self.max_restarts:
                self.repo.update_status(agent_id, "failed")
                logger.error("agent %s exceeded max restarts", agent_id)
            else:
                logger.warning("agent %s unhealthy, restart %d/%d", agent_id, restarts, self.max_restarts)
                self._restart_container(agent_id)
        elif agent.get("status") == "unhealthy":
            self.repo.update_status(agent_id, "idle")
            logger.info("agent %s recovered to idle", agent_id)

    def _restart_container(self, agent_id: str):
        agent = self.repo.get_by_id(agent_id)
        if not agent or not self.docker:
            return
        try:
            cid = agent.get("container_id", "")
            if cid:
                c = self.docker.containers.get(cid)
                c.restart(timeout=10)
                logger.info("restarted container for agent %s", agent_id)
        except Exception:
            logger.exception("failed to restart container for agent %s", agent_id)

    async def health_loop(self, interval: float = 30.0):
        while True:
            for agent in self.repo.list_all():
                await self.health_check(agent["agent_id"])
            await asyncio.sleep(interval)

    async def get_container_stats(self, agent_id: str) -> dict:
        agent = self.repo.get_by_id(agent_id)
        if not agent or not self.docker:
            return {"cpu_pct": 0.0, "mem_mb": 0, "disk_mb": 0}
        try:
            cid = agent.get("container_id", "")
            if not cid:
                return {"cpu_pct": 0.0, "mem_mb": 0, "disk_mb": 0}
            c = self.docker.containers.get(cid)
            stats = c.stats(stream=False)
            cpu_delta = stats["cpu_stats"]["cpu_usage"]["total_usage"] - stats["precpu_stats"]["cpu_usage"]["total_usage"]
            system_delta = stats["cpu_stats"]["system_cpu_usage"] - stats["precpu_stats"]["system_cpu_usage"]
            num_cpus = stats["cpu_stats"].get("online_cpus", 1)
            cpu_pct = 0.0
            if system_delta > 0 and cpu_delta > 0:
                cpu_pct = (cpu_delta / system_delta) * num_cpus * 100.0
            mem_usage = stats["memory_stats"].get("usage", 0)
            mem_mb = round(mem_usage / (1024 * 1024))
            return {
                "cpu_pct": round(cpu_pct, 2),
                "mem_mb": mem_mb,
                "disk_mb": 0,
            }
        except Exception:
            return {"cpu_pct": 0.0, "mem_mb": 0, "disk_mb": 0}

    async def reaper_cleanup(self, agent_id: str, job_id: str):
        ws_dir = f"{self.data_dir}/work/workspaces/{job_id}"
        try:
            shutil.rmtree(ws_dir, ignore_errors=True)
        except Exception:
            pass

    def get_client(self, agent_id: str) -> AgentClient | None:
        agent = self.repo.get_by_id(agent_id)
        if not agent or not agent.get("port"):
            return None
        return AgentClient(f"http://127.0.0.1:{agent['port']}")

    def list_pool_containers(self) -> list[dict]:
        if not self.docker:
            return []
        try:
            containers = self.docker.containers.list(
                all=True,
                filters={"label": "iagent.pool=true"},
            )
            result = []
            for c in containers:
                labels = c.labels or {}
                result.append({
                    "container_id": c.id,
                    "name": c.name,
                    "status": c.status,
                    "agent_id": labels.get("iagent.agent_id", ""),
                    "labels": labels,
                })
            return result
        except Exception:
            logger.exception("failed to list pool containers")
            return []
