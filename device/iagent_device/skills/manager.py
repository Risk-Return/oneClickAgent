"""Device-wide skill manager: receives SKILL_DISPATCH_* chunked packages,
caches locally, applies SKILL_ACTION (device-wide: install/disable/update/delete
to all agents; per-agent: enable/disable), reports SKILL_STATE,
and reconciles against SKILL_SYNC on reconnect.
"""

import hashlib
import logging
from pathlib import Path

from iagent_device.tunnel.codec import FrameType, new_msg_id
from iagent_device.tunnel.outbox import Outbox
from iagent_device.store.repositories import SkillRepo, AgentRepo
from iagent_device.docker.manager import DockerManager

logger = logging.getLogger(__name__)


class SkillManager:
    def __init__(
        self,
        skills_dir: Path,
        skill_repo: SkillRepo,
        agent_repo: AgentRepo,
        docker_mgr: DockerManager,
        outbox: Outbox,
    ):
        self.skills_dir = skills_dir
        self.skill_repo = skill_repo
        self.agent_repo = agent_repo
        self.docker = docker_mgr
        self.outbox = outbox
        self._dispatch_bufs: dict[str, dict] = {}

    async def handle_dispatch_begin(self, payload: dict):
        skill_id = payload["skill_id"]
        version_id = payload["skill_version_id"]
        sha256 = payload["sha256"]
        total_chunks = payload["total_chunks"]

        self._dispatch_bufs[skill_id] = {
            "version_id": version_id,
            "sha256": sha256,
            "total_chunks": total_chunks,
            "chunks_received": 0,
            "data": b"",
            "hasher": hashlib.sha256(),
        }

    async def handle_chunk(self, payload: dict):
        skill_id = payload["skill_id"]
        buf = self._dispatch_bufs.get(skill_id)
        if not buf:
            return
        import base64
        data = base64.b64decode(payload["data"])
        buf["data"] += data
        buf["hasher"].update(data)
        buf["chunks_received"] += 1

    async def handle_dispatch_end(self, payload: dict):
        skill_id = payload["skill_id"]
        buf = self._dispatch_bufs.pop(skill_id, None)
        if not buf:
            return

        actual = buf["hasher"].hexdigest()
        expected = buf["sha256"]
        if actual != expected:
            await self.outbox.enqueue_and_send(FrameType.SKILL_STATE, {
                "skill_id": skill_id,
                "skill_version_id": buf["version_id"],
                "scope": "device",
                "status": "error",
                "error": f"SHA-256 mismatch",
            })
            return

        # Cache artifact
        artifact_path = self.skills_dir / skill_id / "artifact.tar.gz"
        artifact_path.parent.mkdir(parents=True, exist_ok=True)
        artifact_path.write_bytes(buf["data"])

        await self.outbox.enqueue_and_send(FrameType.SKILL_STATE, {
            "skill_id": skill_id,
            "skill_version_id": buf["version_id"],
            "scope": "device",
            "status": "cached",
        })

    async def handle_skill_action(self, payload: dict):
        scope = payload.get("scope", "device")
        action = payload.get("action", "")
        skill_id = payload.get("skill_id", "")
        agent_id = payload.get("agent_id", "")
        version = payload.get("version", "")

        if scope == "device":
            agents = self.agent_repo.list_all() if action in ("install", "update") else [dict(agent_id=agent_id) for _ in [1] if agent_id]

            for agent in agents:
                if action == "delete":
                    agents_to_use = self.agent_repo.list_all()
                    for a in agents_to_use:
                        await self._apply_skill_action(a["agent_id"], skill_id, action)
                else:
                    await self._apply_skill_action(agent["agent_id"], skill_id, action)
        elif scope == "agent" and agent_id:
            await self._apply_skill_action(agent_id, skill_id, action)

    async def _apply_skill_action(self, agent_id: str, skill_id: str, action: str):
        client = self.docker.get_client(agent_id)
        if not client:
            return
        try:
            if action == "install" or action == "update":
                await client.install_skill(skill_id, "", "", "")
            elif action == "enable":
                await client.enable_skill(skill_id)
            elif action == "disable":
                await client.disable_skill(skill_id)
            elif action == "delete":
                await client.delete_skill(skill_id)
        except Exception:
            logger.exception("skill action %s failed for agent %s", action, agent_id)
