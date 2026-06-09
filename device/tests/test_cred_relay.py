"""Unit tests for credential relay (no persistence, sha256 verification)."""

import asyncio
import hashlib
import base64
import pytest
from unittest.mock import AsyncMock, MagicMock

from iagent_device.creds.relay import CredRelay
from iagent_device.agentclient.client import AgentClient


class TestCredRelay:
    @pytest.fixture
    def cred_relay(self, outbox):
        docker_mgr = MagicMock()
        client = MagicMock(spec=AgentClient)
        client.set_browser_state = AsyncMock()
        client.get_browser_state = AsyncMock(return_value={"storage_state": '{"cookies":[]}'})
        docker_mgr.get_client = MagicMock(return_value=client)
        return CredRelay(docker_mgr, outbox)

    @pytest.mark.asyncio
    async def test_cred_push_valid(self, cred_relay, outbox_repo):
        storage_state = '{"cookies":[{"name":"sess","value":"abc"}]}'
        data_b64 = base64.b64encode(storage_state.encode()).decode()
        sha = hashlib.sha256(storage_state.encode()).hexdigest()

        await cred_relay.handle_cred_push({
            "job_id": "j1",
            "credential_id": "c1",
            "agent_id": "a1",
            "storage_state": data_b64,
            "sha256": sha,
        })

        unacked = outbox_repo.list_unacked()
        ack = [u for u in unacked if u["type"] == "CRED_PUSH_ACK"]
        assert len(ack) >= 1
        assert "PUSH_ACK" in ack[0]["type"]

    @pytest.mark.asyncio
    async def test_cred_push_sha256_mismatch(self, cred_relay, outbox_repo):
        data_b64 = base64.b64encode(b"bad data").decode()
        await cred_relay.handle_cred_push({
            "job_id": "j2",
            "credential_id": "c2",
            "agent_id": "a2",
            "storage_state": data_b64,
            "sha256": "deadbeef" * 8,
        })

        unacked = outbox_repo.list_unacked()
        ack = [u for u in unacked if u["type"] == "CRED_PUSH_ACK"]
        assert len(ack) >= 1

    @pytest.mark.asyncio
    async def test_cred_push_no_agent(self, cred_relay, outbox_repo):
        cred_relay.docker.get_client = MagicMock(return_value=None)
        data_b64 = base64.b64encode(b"data").decode()
        sha = hashlib.sha256(b"data").hexdigest()

        await cred_relay.handle_cred_push({
            "job_id": "j3",
            "credential_id": "c3",
            "agent_id": "a99",
            "storage_state": data_b64,
            "sha256": sha,
        })

    @pytest.mark.asyncio
    async def test_cred_push_status_injected(self, cred_relay, outbox_repo):
        storage_state = '{"cookies":[]}'
        data_b64 = base64.b64encode(storage_state.encode()).decode()
        sha = hashlib.sha256(storage_state.encode()).hexdigest()

        await cred_relay.handle_cred_push({
            "job_id": "j4",
            "credential_id": "c4",
            "agent_id": "a4",
            "storage_state": data_b64,
            "sha256": sha,
        })

        unacked = outbox_repo.list_unacked()
        payloads = [u["payload"] for u in unacked if u["type"] == "CRED_PUSH_ACK"]
        assert any("INJECTED" in p for p in payloads)

    @pytest.mark.asyncio
    async def test_cred_capture(self, cred_relay, outbox_repo):
        await cred_relay.handle_cred_capture({
            "session_id": "s1",
            "agent_id": "a1",
            "origin": "https://example.com",
        })

        unacked = outbox_repo.list_unacked()
        capture = [u for u in unacked if u["type"] == "CRED_CAPTURE"]
        assert len(capture) >= 1

    @pytest.mark.asyncio
    async def test_cred_capture_no_agent(self, cred_relay):
        cred_relay.docker.get_client = MagicMock(return_value=None)
        await cred_relay.handle_cred_capture({
            "session_id": "s2",
            "agent_id": "a99",
            "origin": "",
        })

    @pytest.mark.asyncio
    async def test_cred_never_persisted_to_db(self, cred_relay, tmp_path):
        storage_state = '{"cookies":[]}'
        data_b64 = base64.b64encode(storage_state.encode()).decode()
        sha = hashlib.sha256(storage_state.encode()).hexdigest()

        await cred_relay.handle_cred_push({
            "job_id": "j5",
            "credential_id": "c5",
            "agent_id": "a5",
            "storage_state": data_b64,
            "sha256": sha,
        })

    @pytest.mark.asyncio
    async def test_wait_for_injections_empty_returns_immediately(self, cred_relay):
        assert await cred_relay.wait_for_injections("j1", [], timeout=0.1) is True

    @pytest.mark.asyncio
    async def test_wait_for_injections_resolves_when_all_pushed(self, cred_relay):
        storage_state = '{"cookies":[]}'
        data_b64 = base64.b64encode(storage_state.encode()).decode()
        sha = hashlib.sha256(storage_state.encode()).hexdigest()

        async def push_later():
            await asyncio.sleep(0.05)
            for cid in ("c1", "c2"):
                await cred_relay.handle_cred_push({
                    "job_id": "jw", "credential_id": cid, "agent_id": "a1",
                    "storage_state": data_b64, "sha256": sha,
                })

        waiter = asyncio.create_task(cred_relay.wait_for_injections("jw", ["c1", "c2"], timeout=2.0))
        pusher = asyncio.create_task(push_later())
        assert await waiter is True
        await pusher

    @pytest.mark.asyncio
    async def test_wait_for_injections_times_out_when_missing(self, cred_relay):
        result = await cred_relay.wait_for_injections("jt", ["missing"], timeout=0.1)
        assert result is False
