"""Unit tests for skill manager — cache, dispatch, apply, sync."""

import hashlib
import base64
import pytest
from unittest.mock import AsyncMock, MagicMock

from iagent_device.skills.manager import SkillManager
from iagent_device.agentclient.client import AgentClient


@pytest.fixture
def skill_mgr(tmp_skills_dir, skill_repo, agent_repo, outbox):
    docker_mgr = MagicMock()
    client = MagicMock(spec=AgentClient)
    client.install_skill = AsyncMock()
    client.enable_skill = AsyncMock()
    client.disable_skill = AsyncMock()
    client.delete_skill = AsyncMock()
    docker_mgr.get_client = MagicMock(return_value=client)
    return SkillManager(tmp_skills_dir, skill_repo, agent_repo, docker_mgr, outbox)


class TestSkillManager:
    @pytest.mark.asyncio
    async def test_dispatch_begin_creates_buffer(self, skill_mgr):
        await skill_mgr.handle_dispatch_begin({
            "skill_id": "sk1",
            "skill_version_id": "v1",
            "sha256": "abc",
            "total_chunks": 2,
        })
        assert "sk1" in skill_mgr._dispatch_bufs
        assert skill_mgr._dispatch_bufs["sk1"]["total_chunks"] == 2

    @pytest.mark.asyncio
    async def test_dispatch_chunks_and_end_sha256_ok(self, skill_mgr, tmp_skills_dir, outbox_repo):
        data = b"skill package data"
        sha = hashlib.sha256(data).hexdigest()

        await skill_mgr.handle_dispatch_begin({
            "skill_id": "sk2",
            "skill_version_id": "v1",
            "sha256": sha,
            "total_chunks": 1,
        })
        await skill_mgr.handle_chunk({
            "skill_id": "sk2",
            "data": base64.b64encode(data).decode(),
        })
        await skill_mgr.handle_dispatch_end({"skill_id": "sk2"})

        assert (tmp_skills_dir / "sk2" / "artifact.tar.gz").exists()
        unacked = outbox_repo.list_unacked()
        ack = [u for u in unacked if u["type"] == "SKILL_DISPATCH_ACK"]
        assert len(ack) == 1
        assert "CACHED" in ack[0]["payload"]

    @pytest.mark.asyncio
    async def test_dispatch_end_sha256_mismatch(self, skill_mgr, outbox_repo):
        data = b"skill package data"

        await skill_mgr.handle_dispatch_begin({
            "skill_id": "sk3",
            "skill_version_id": "v1",
            "sha256": "deadbeef" * 8,
            "total_chunks": 1,
        })
        await skill_mgr.handle_chunk({
            "skill_id": "sk3",
            "data": base64.b64encode(data).decode(),
        })
        await skill_mgr.handle_dispatch_end({"skill_id": "sk3"})

        unacked = outbox_repo.list_unacked()
        ack = [u for u in unacked if u["type"] == "SKILL_DISPATCH_ACK"]
        assert any("ERROR" in p["payload"] for p in ack)

    @pytest.mark.asyncio
    async def test_skill_action_device_install(self, skill_mgr, agent_repo, skill_repo):
        agent_repo.upsert("a1", "agent-1", "img", 8090, status="idle")
        agent_repo.upsert("a2", "agent-2", "img", 8091, status="idle")

        await skill_mgr.handle_skill_action({
            "scope": "device",
            "action": "install",
            "skill_id": "sk1",
            "version": "1.0",
        })

        skills = skill_repo.list_device_skills()
        assert len(skills) == 1
        assert skills[0]["skill_id"] == "sk1"

    @pytest.mark.asyncio
    async def test_skill_action_device_disable(self, skill_mgr, skill_repo):
        skill_repo.upsert_device_skill("sk2", "sk2", "Test", "1.0", "", "", "", "installed")

        await skill_mgr.handle_skill_action({
            "scope": "device",
            "action": "disable",
            "skill_id": "sk2",
        })

        skills = skill_repo.list_device_skills()
        assert skills[0]["status"] == "disabled"

    @pytest.mark.asyncio
    async def test_skill_action_per_agent(self, skill_mgr, agent_repo, skill_repo):
        agent_repo.upsert("a3", "agent-3", "img", 8092, status="idle")
        skill_repo.upsert_device_skill("sk3", "sk3", "Test3", "1.0", "", "", "", "installed")

        await skill_mgr.handle_skill_action({
            "scope": "agent",
            "action": "enable",
            "skill_id": "sk3",
            "agent_id": "a3",
        })

        agent_skills = skill_repo.list_agent_skills("a3")
        assert len(agent_skills) == 1
        assert agent_skills[0]["status"] == "enabled"

    @pytest.mark.asyncio
    async def test_skill_state_emission(self, skill_mgr, agent_repo, skill_repo, outbox_repo):
        agent_repo.upsert("a4", "agent-4", "img", 8093, status="idle")
        skill_repo.upsert_device_skill("sk4", "sk4", "Test4", "1.0", "", "", "", "installed")
        skill_repo.upsert_agent_skill("a4", "sk4", "enabled")

        await skill_mgr._emit_skill_state()

        unacked = outbox_repo.list_unacked()
        state = [u for u in unacked if u["type"] == "SKILL_STATE"]
        assert len(state) >= 1

    @pytest.mark.asyncio
    async def test_prune_skill_cache(self, skill_mgr, tmp_skills_dir):
        skill_dir = tmp_skills_dir / "sk5"
        skill_dir.mkdir(parents=True)
        (skill_dir / "artifact.tar.gz").write_text("data")

        skill_mgr._prune_skill_cache("sk5")
        assert not skill_dir.exists()

    @pytest.mark.asyncio
    async def test_handle_skill_sync(self, skill_mgr, agent_repo, skill_repo):
        agent_repo.upsert("a5", "agent-5", "img", 8094, status="idle")
        skill_repo.upsert_device_skill("sk6", "sk6", "Test6", "1.0", "", "", "", "installed")

        await skill_mgr.handle_skill_sync({
            "device_skills": [
                {"skill_id": "sk6", "version": "1.0"},
                {"skill_id": "sk7", "version": "2.0"},
            ],
            "agent_skills": [
                {"agent_id": "a5", "skill_id": "sk6", "status": "enabled"},
            ],
        })

    @pytest.mark.asyncio
    async def test_install_skills_on_new_agent(self, skill_mgr, agent_repo, skill_repo):
        skill_repo.upsert_device_skill("sk8", "sk8", "Test8", "1.0", "", "", "", "installed")
        agent_repo.upsert("new-agent", "new", "img", 8095, status="idle")

        await skill_mgr.install_skills_on_new_agent("new-agent")

        agent_skills = skill_repo.list_agent_skills("new-agent")
        assert len(agent_skills) == 1
        assert agent_skills[0]["skill_id"] == "sk8"
