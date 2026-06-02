"""Unit tests for job dispatcher state machine."""

import pytest
from unittest.mock import AsyncMock, MagicMock

from iagent_device.jobs.dispatcher import JobDispatcher
from iagent_device.agentclient.client import AgentClient


def make_client():
    client = MagicMock(spec=AgentClient)
    client.create_job = AsyncMock(return_value={"status": "accepted"})
    client.get_job = AsyncMock(return_value={"status": "succeeded", "percent": 100, "result": {"output": "done"}})
    client.cancel_job = AsyncMock(return_value={"status": "cancelled"})
    return client


@pytest.fixture
def docker_mgr(agent_repo):
    mgr = MagicMock()
    mgr.get_client = MagicMock(return_value=make_client())
    mgr.reaper_cleanup = AsyncMock()
    return mgr


@pytest.fixture
def dispatcher(job_repo, agent_repo, docker_mgr, outbox):
    stager = MagicMock()
    stager.cleanup = AsyncMock()
    return JobDispatcher(
        job_repo=job_repo,
        agent_repo=agent_repo,
        docker_mgr=docker_mgr,
        outbox=outbox,
        stager=stager,
    )


class TestJobDispatch:
    @pytest.mark.asyncio
    async def test_job_dispatch_creates_and_accepts(self, dispatcher, job_repo, agent_repo, docker_mgr):
        agent_repo.upsert("a1", "agent-1", "img", 8090, status="idle")

        payload = {"job_id": "j1", "agent_id": "a1", "user_id": "u1", "command": "test"}
        await dispatcher.handle_job_dispatch(payload)

        job = job_repo.get_by_id("j1")
        assert job is not None
        assert job["command"] == "test"

        agent = agent_repo.get_by_id("a1")
        assert agent["status"] == "idle"

    @pytest.mark.asyncio
    async def test_job_dispatch_no_agent_client(self, dispatcher, agent_repo, docker_mgr):
        docker_mgr.get_client = MagicMock(return_value=None)

        payload = {"job_id": "j2", "agent_id": "a99"}
        await dispatcher.handle_job_dispatch(payload)

    @pytest.mark.asyncio
    async def test_job_progress_relay(self, dispatcher, agent_repo, docker_mgr, job_repo):
        agent_repo.upsert("a2", "agent-2", "img", 8091, status="idle")

        payload = {"job_id": "j3", "agent_id": "a2", "user_id": "u2", "command": "test"}
        await dispatcher.handle_job_dispatch(payload)

        job = job_repo.get_by_id("j3")
        assert job["status"] in ("running", "succeeded")

    @pytest.mark.asyncio
    async def test_job_cancel(self, dispatcher, job_repo, agent_repo, docker_mgr):
        agent_repo.upsert("a3", "agent-3", "img", 8092, status="busy")
        agent_repo.allocate("a3", "u3", "j4")
        job_repo.create("j4", "a3", "u3", "test")

        payload = {"job_id": "j4", "agent_id": "a3"}
        await dispatcher.handle_job_cancel(payload)

        job = job_repo.get_by_id("j4")
        assert job["status"] == "cancelled"
        agent = agent_repo.get_by_id("a3")
        assert agent["status"] == "idle"

    @pytest.mark.asyncio
    async def test_job_dispatch_with_credential_ids(self, dispatcher, job_repo, agent_repo):
        agent_repo.upsert("a4", "agent-4", "img", 8093, status="idle")

        payload = {
            "job_id": "j5", "agent_id": "a4", "user_id": "u4",
            "command": "test", "credential_ids": ["cred-1", "cred-2"],
        }
        await dispatcher.handle_job_dispatch(payload)

        job = job_repo.get_by_id("j5")
        assert "cred-1" in job.get("credential_ids", "")
