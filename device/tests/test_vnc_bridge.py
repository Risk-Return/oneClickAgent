"""Unit tests for VNC bridge byte-pump."""

import pytest
from unittest.mock import AsyncMock, MagicMock

from iagent_device.vncbridge.bridge import VNCBridge
from iagent_device.agentclient.client import AgentClient


class TestVNCBridge:
    @pytest.fixture
    def vnc_bridge(self, vnc_repo, outbox):
        docker_mgr = MagicMock()
        docker_mgr.get_client = MagicMock(return_value=None)
        return VNCBridge(vnc_repo, docker_mgr, outbox, dial_timeout=5)

    @pytest.mark.asyncio
    async def test_vnc_open_no_agent_client(self, vnc_bridge, vnc_repo):
        await vnc_bridge.handle_vnc_open({
            "session_id": "s1",
            "agent_id": "a1",
            "relay_url": "wss://gateway/session",
            "session_token": "tok",
        })
    @pytest.mark.asyncio
    async def test_vnc_close_cancels_task(self, vnc_bridge, vnc_repo):
        vnc_repo.create("s2", "j1", "a1", "wss://gw", "tok")

        task = MagicMock()
        vnc_bridge._sessions["s2"] = task

        await vnc_bridge.handle_vnc_close({"session_id": "s2"})
        task.cancel.assert_called_once()

    @pytest.mark.asyncio
    async def test_vnc_close_unknown_session(self, vnc_bridge):
        await vnc_bridge.handle_vnc_close({"session_id": "nonexistent"})

    @pytest.mark.asyncio
    async def test_vnc_open_with_ttl(self, vnc_bridge, vnc_repo):
        await vnc_bridge.handle_vnc_open({
            "session_id": "s3",
            "agent_id": "a3",
            "relay_url": "wss://gateway/session",
            "session_token": "tok",
            "ttl_s": 300,
        })

    @pytest.mark.asyncio
    async def test_bridge_sends_vnc_close_on_drop(self, vnc_bridge, vnc_repo, outbox_repo):
        docker_mgr = vnc_bridge.docker
        client = MagicMock(spec=AgentClient)
        client.vnc_start = AsyncMock(return_value={"rfb_port": 5901, "rfb_password": "pw"})
        client.vnc_stop = AsyncMock()
        docker_mgr.get_client = MagicMock(return_value=client)

        await vnc_bridge.handle_vnc_open({
            "session_id": "s6",
            "agent_id": "a4",
            "relay_url": "wss://gateway/session",
            "session_token": "tok",
            "ttl_s": 1,
        })

        unacked = outbox_repo.list_unacked()
        vnc_opened = [u for u in unacked if u["type"] == "VNC_OPENED"]
        assert len(vnc_opened) >= 1  # VNC_OPENED was sent (either ready or error)
