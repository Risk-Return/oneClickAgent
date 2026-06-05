"""Device-wide skill manager: receives SKILL_DISPATCH_* chunked packages,
caches locally, applies SKILL_ACTION (device-wide: install/disable/update/delete
to all agents; per-agent: enable/disable), reports SKILL_STATE,
and reconciles against SKILL_SYNC on reconnect.
"""

import hashlib
import logging
from pathlib import Path

from iagent_device.tunnel.codec import FrameType
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
            await self.outbox.enqueue_and_send(FrameType.SKILL_DISPATCH_ACK, {
                "skill_id": skill_id,
                "skill_version_id": buf["version_id"],
                "status": "ERROR",
                "error": "SHA-256 mismatch",
            })
            return

        artifact_path = self.skills_dir / skill_id / "artifact.tar.gz"
        artifact_path.parent.mkdir(parents=True, exist_ok=True)
        artifact_path.write_bytes(buf["data"])

        self.skill_repo.upsert_device_skill(
            skill_id, skill_id, skill_id, buf["version_id"], "", str(artifact_path), buf["sha256"], "cached"
        )

        await self.outbox.enqueue_and_send(FrameType.SKILL_DISPATCH_ACK, {
            "skill_id": skill_id,
            "skill_version_id": buf["version_id"],
            "status": "CACHED",
        })

    async def handle_skill_action(self, payload: dict):
        scope = payload.get("scope", "device")
        action = payload.get("action", "")
        skill_id = payload.get("skill_id", "")
        agent_id = payload.get("agent_id", "")
        version = payload.get("version", "")

        if scope == "device":
            if action == "install":
                self.skill_repo.upsert_device_skill(
                    skill_id, skill_id, skill_id, version, "", "", "", "installing"
                )
            elif action == "update":
                self.skill_repo.update_device_skill_status(skill_id, "updating")
            elif action == "disable":
                self.skill_repo.update_device_skill_status(skill_id, "disabling")
            elif action == "delete":
                self.skill_repo.update_device_skill_status(skill_id, "deleting")

            target_agents = self.agent_repo.list_all()
            agent_results: list[dict] = []
            successes = 0
            failures = 0
            for agent in target_agents:
                try:
                    await self._apply_skill_action(agent["agent_id"], skill_id, action, version)
                    successes += 1
                    agent_results.append({"agent_id": agent["agent_id"], "status": "installed"})
                except Exception as e:
                    failures += 1
                    agent_results.append({"agent_id": agent["agent_id"], "status": "error", "error": str(e)})
                    logger.exception("skill action %s failed for agent %s", action, agent["agent_id"])

            if action == "install" or action == "update":
                final_status = "installed" if failures == 0 else "error"
                self.skill_repo.update_device_skill_status(skill_id, final_status)
                # Persist per-agent results
                for r in agent_results:
                    self.skill_repo.upsert_agent_skill(
                        r["agent_id"], skill_id,
                        "installed" if r["status"] == "installed" else "error"
                    )
            elif action == "disable":
                final_status = "disabled" if failures == 0 else "error"
                self.skill_repo.update_device_skill_status(skill_id, final_status)
            elif action == "delete":
                if failures == 0:
                    self.skill_repo.delete_device_skill(skill_id)
                    self._prune_skill_cache(skill_id)
                    final_status = "deleted"
                else:
                    final_status = "error"
            else:
                final_status = "error"

            await self._emit_skill_state()

        elif scope == "agent" and agent_id:
            try:
                await self._apply_skill_action(agent_id, skill_id, action, version)
            except Exception:
                logger.exception("per-agent skill action %s failed for agent %s", action, agent_id)
            status = "enabled" if action == "enable" else "disabled" if action == "disable" else "applied"
            self.skill_repo.upsert_agent_skill(agent_id, skill_id, status)
            await self._emit_skill_state()

    async def _apply_skill_action(self, agent_id: str, skill_id: str, action: str, version: str = ""):
        client = self.docker.get_client(agent_id)
        if not client:
            return
        if action == "install" or action == "update":
            skill = self.skill_repo.list_device_skills()
            skill_info = next((s for s in skill if s["skill_id"] == skill_id), {})
            artifact_path = skill_info.get("artifact_path", "")
            manifest = skill_info.get("manifest", "")
            await client.install_skill(skill_id, version or skill_info.get("version", ""), manifest, artifact_path)
        elif action == "enable":
            await client.enable_skill(skill_id)
        elif action == "disable":
            await client.disable_skill(skill_id)
        elif action == "delete":
            await client.delete_skill(skill_id)

    async def handle_skill_retry(self, payload: dict):
        skill_id = payload.get("skill_id", "")
        agent_ids = payload.get("agent_ids", [])
        version = payload.get("version", "")

        target_agents = self.agent_repo.list_all()
        if agent_ids:
            target_agents = [a for a in target_agents if a["agent_id"] in agent_ids]

        agent_results: list[dict] = []
        failures = 0
        for agent in target_agents:
            try:
                await self._apply_skill_action(agent["agent_id"], skill_id, "install", version)
                agent_results.append({"agent_id": agent["agent_id"], "status": "installed"})
            except Exception as e:
                failures += 1
                agent_results.append({"agent_id": agent["agent_id"], "status": "error", "error": str(e)})
                logger.exception("skill retry failed for agent %s", agent["agent_id"])

        for r in agent_results:
            self.skill_repo.upsert_agent_skill(
                r["agent_id"], skill_id,
                "installed" if r["status"] == "installed" else "error"
            )

        if failures == 0:
            self.skill_repo.update_device_skill_status(skill_id, "installed")

        await self._emit_skill_state()

    async def handle_skill_sync(self, payload: dict):
        desired_device_skills = payload.get("device_skills", [])
        desired_agent_skills = payload.get("agent_skills", [])

        for ds in desired_device_skills:
            skill_id = ds["skill_id"]
            existing = next((s for s in self.skill_repo.list_device_skills() if s["skill_id"] == skill_id), None)
            if not existing or existing.get("version") != ds.get("version"):
                await self.outbox.enqueue_and_send(FrameType.SKILL_SYNC, {
                    "skill_id": skill_id,
                    "status": "missing",
                })

        for ags in desired_agent_skills:
            agent_skills = self.skill_repo.list_agent_skills(ags["agent_id"])
            existing = next((s for s in agent_skills if s["skill_id"] == ags["skill_id"]), None)
            if not existing or existing.get("status") != ags.get("status"):
                await self.handle_skill_action({
                    "scope": "agent",
                    "action": ags.get("status", "enable"),
                    "skill_id": ags["skill_id"],
                    "agent_id": ags["agent_id"],
                })

    async def install_skills_on_new_agent(self, agent_id: str):
        for skill in self.skill_repo.list_device_skills():
            if skill.get("status") in ("installed", "cached"):
                try:
                    await self._apply_skill_action(agent_id, skill["skill_id"], "install", skill.get("version", ""))
                    self.skill_repo.upsert_agent_skill(agent_id, skill["skill_id"], "enabled")
                except Exception:
                    logger.exception("failed to install skill %s on new agent %s", skill["skill_id"], agent_id)

    async def _emit_skill_state(self):
        device_skills = []
        for s in self.skill_repo.list_device_skills():
            device_skills.append({
                "skill_id": s["skill_id"],
                "version": s.get("version", ""),
                "status": s.get("status", ""),
            })

        agent_skills = []
        for agent in self.agent_repo.list_all():
            for s in self.skill_repo.list_agent_skills(agent["agent_id"]):
                entry: dict = {
                    "agent_id": agent["agent_id"],
                    "skill_id": s["skill_id"],
                    "status": s.get("status", ""),
                }
                if s.get("error"):
                    entry["error"] = s.get("error")
                agent_skills.append(entry)

        await self.outbox.enqueue_and_send(FrameType.SKILL_STATE, {
            "device_skills": device_skills,
            "agent_skills": agent_skills,
        })

    def _prune_skill_cache(self, skill_id: str):
        skill_dir = self.skills_dir / skill_id
        if skill_dir.exists():
            import shutil
            shutil.rmtree(skill_dir)
