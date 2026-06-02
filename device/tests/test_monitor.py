"""Unit tests for monitor resource sampling."""

import pytest
from unittest.mock import AsyncMock, MagicMock

from iagent_device.monitor.monitor import Monitor


class TestMonitor:
    @pytest.mark.asyncio
    async def test_sample_emits_agent_status(self, agent_repo, outbox_repo):
        agent_repo.upsert("a1", "agent-1", "img", 8090, status="idle")

        docker_mgr = MagicMock()
        docker_mgr.get_container_stats = AsyncMock(
            return_value={"cpu_pct": 12.5, "mem_mb": 512, "disk_mb": 100}
        )

        monitor = Monitor(agent_repo, None, docker_mgr=docker_mgr)

        async def capture_send(frame_type, payload):
            pass

        from iagent_device.tunnel.outbox import Outbox
        monitor.outbox = Outbox(outbox_repo, capture_send)

        agent = agent_repo.get_by_id("a1")
        await monitor._sample(agent)

        unacked = outbox_repo.list_unacked()
        statuses = [u for u in unacked if u["type"] == "AGENT_STATUS"]
        assert len(statuses) >= 1
        payload = statuses[0]["payload"]
        assert '"agent_id"' in payload
        assert '"usage"' in payload

    def test_build_hello_extras(self, agent_repo):
        agent_repo.upsert("a1", "agent-1", "img", 8090, status="idle")
        agent_repo.upsert("a2", "agent-2", "img", 8091, status="busy")

        monitor = Monitor(agent_repo, None)
        extras = monitor.build_hello_extras(agent_repo, vnc_enabled=True)

        assert "platform" in extras
        assert "agent_version" in extras
        assert "resources" in extras
        assert "agents" in extras
        assert "capabilities" in extras
        assert extras["capabilities"]["vnc_enabled"] is True
        assert len(extras["agents"]) == 2
        assert extras["agent_count"] == 2

    @pytest.mark.asyncio
    async def test_sample_no_docker(self, agent_repo, outbox_repo):
        agent_repo.upsert("a3", "agent-3", "img", 8092, status="idle")

        monitor = Monitor(agent_repo, None)

        async def capture_send(frame_type, payload):
            pass

        from iagent_device.tunnel.outbox import Outbox
        monitor.outbox = Outbox(outbox_repo, capture_send)

        agent = agent_repo.get_by_id("a3")
        await monitor._sample(agent)

        unacked = outbox_repo.list_unacked()
        statuses = [u for u in unacked if u["type"] == "AGENT_STATUS"]
        assert len(statuses) >= 1
        payload = statuses[0]["payload"]
        assert 'cpu_pct' in payload
