"""Unit tests for Docker manager with mock client."""

import pytest
from unittest.mock import MagicMock

from iagent_device.store.repositories import AgentRepo
from iagent_device.docker.manager import DockerManager


@pytest.fixture
def mock_docker():
    dc = MagicMock()
    dc.containers = MagicMock()
    dc.images = MagicMock()
    dc.images.pull = MagicMock(return_value=["layer1", "layer2"])
    mock_container = MagicMock()
    mock_container.id = "mock-cid"
    dc.containers.run = MagicMock(return_value=mock_container)
    dc.containers.get = MagicMock(return_value=mock_container)
    return dc


@pytest.fixture
def docker_mgr(agent_repo, mock_docker):
    return DockerManager(
        agent_repo=agent_repo,
        image="iagent/agent:latest",
        port_start=42000,
        port_end=42009,
        max_restarts=3,
        docker_client=mock_docker,
        data_dir="/tmp/test-workspaces",
    )


class TestDockerManager:
    @pytest.mark.asyncio
    async def test_prepull_image(self, docker_mgr, mock_docker):
        await docker_mgr.prepull_image()
        mock_docker.images.pull.assert_called_once_with("iagent/agent:latest")

    @pytest.mark.asyncio
    async def test_prepull_no_docker(self, agent_repo):
        mgr = DockerManager(agent_repo, "img", 42000, 42009, docker_client=None)
        await mgr.prepull_image()

    @pytest.mark.asyncio
    async def test_ensure_pool_creates_agents(self, docker_mgr, agent_repo):
        await docker_mgr.ensure_pool(2)
        agents = agent_repo.list_all()
        assert len(agents) == 2
        assert all(a["status"] == "idle" for a in agents)

    @pytest.mark.asyncio
    async def test_ensure_pool_removes_surplus(self, docker_mgr, agent_repo):
        await docker_mgr.ensure_pool(3)
        await docker_mgr.ensure_pool(1)
        agents = agent_repo.list_all()
        assert len(agents) == 1

    @pytest.mark.asyncio
    async def test_ensure_pool_no_docker(self, agent_repo):
        mgr = DockerManager(agent_repo, "img", 42000, 42009, docker_client=None)
        await mgr.ensure_pool(1)
        assert len(agent_repo.list_all()) == 1

    @pytest.mark.asyncio
    async def test_health_check_healthy(self, docker_mgr, agent_repo):
        agent_repo.upsert("a1", "agent-1", "img", 8090, status="idle")
        agent_repo.update_status("a1", "idle")
        await docker_mgr.health_check("a1")

    @pytest.mark.asyncio
    async def test_health_check_increments_restarts(self, docker_mgr, agent_repo):
        agent_repo.upsert("a2", "agent-2", "img", 8091, status="idle")
        await docker_mgr.health_check("a2")
        agent = agent_repo.get_by_id("a2")
        assert agent["restarts"] == 1

    @pytest.mark.asyncio
    async def test_health_check_exceeds_max(self, docker_mgr, agent_repo):
        agent_repo.upsert("a3", "agent-3", "img", 8092, status="idle")
        for _ in range(4):
            await docker_mgr.health_check("a3")
        agent = agent_repo.get_by_id("a3")
        assert agent["status"] == "failed"

    @pytest.mark.asyncio
    async def test_get_client(self, docker_mgr, agent_repo):
        agent_repo.upsert("a4", "agent-4", "img", 8093, status="idle")
        client = docker_mgr.get_client("a4")
        assert client is not None

    @pytest.mark.asyncio
    async def test_get_client_missing(self, docker_mgr):
        client = docker_mgr.get_client("nonexistent")
        assert client is None

    @pytest.mark.asyncio
    async def test_allocate_and_release_port(self, docker_mgr):
        port = docker_mgr._allocate_port()
        assert 42000 <= port <= 42009
        docker_mgr._release_port(port)

    @pytest.mark.asyncio
    async def test_port_exhaustion(self, docker_mgr):
        for _ in range(10):
            docker_mgr._allocate_port()
        with pytest.raises(RuntimeError, match="no available ports"):
            docker_mgr._allocate_port()

    @pytest.mark.asyncio
    async def test_list_pool_containers_no_docker(self, agent_repo):
        mgr = DockerManager(agent_repo, "img", 42000, 42009, docker_client=None)
        assert mgr.list_pool_containers() == []

    @pytest.mark.asyncio
    async def test_get_container_stats(self, docker_mgr, agent_repo):
        agent_repo.upsert("a5", "agent-5", "img", 8094, status="idle",
                          container_id="fake_cid")
        stats = await docker_mgr.get_container_stats("a5")
        assert isinstance(stats, dict)
        assert "cpu_pct" in stats
        assert "mem_mb" in stats

    @pytest.mark.asyncio
    async def test_reaper_cleanup(self, docker_mgr):
        await docker_mgr.reaper_cleanup("a1", "j1")


def _patch_agent_repo_upsert(agent_repo, agent_id, name, image, port, status="idle", container_id=""):
    agent_repo.upsert(agent_id, name, image, port, container_id, status)


# Patch AgentRepo.upsert for tests that need container_id
setattr(AgentRepo, 'upsert_with_cid', lambda self, agent_id, name, image, port, container_id="", status="creating":
    _patch_agent_repo_upsert(self, agent_id, name, image, port, status, container_id))
