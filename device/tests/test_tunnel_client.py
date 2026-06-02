"""Integration tests for TunnelClient with mock WebSocket server."""

import asyncio
import json
import pytest
from unittest.mock import patch

from iagent_device.tunnel.codec import FrameType, FRAME_VERSION
from iagent_device.tunnel.client import TunnelClient
from iagent_device.tunnel.outbox import Outbox


def _make_frame_str(frame_type, payload=None, msg_id="m1", ack_id=None):
    frame = {"v": FRAME_VERSION, "type": str(frame_type), "msg_id": msg_id, "ts": 1000}
    if ack_id:
        frame["ack_id"] = ack_id
    if payload:
        frame["payload"] = payload
    return json.dumps(frame)


class MockAsyncIterator:
    def __init__(self, messages):
        self._messages = messages
        self._idx = 0

    def __aiter__(self):
        return self

    async def __anext__(self):
        if self._idx >= len(self._messages):
            raise StopAsyncIteration
        msg = self._messages[self._idx]
        self._idx += 1
        return msg


class MockWS:
    def __init__(self, server_messages=None):
        self.sent = []
        self._server_messages = server_messages or []

    def __aiter__(self):
        return MockAsyncIterator(self._server_messages)

    async def send(self, msg):
        self.sent.append(msg)

    async def close(self):
        pass

    async def __aenter__(self):
        return self

    async def __aexit__(self, *args):
        pass


def _noop_send(frame_type, payload):
    pass


@pytest.mark.asyncio
class TestTunnelClient:
    async def test_connect_sends_hello_and_sync(self, outbox_repo):
        mock_ws = MockWS()
        hello_extras = {
            "platform": "Linux",
            "agent_version": "0.1.0",
            "resources": {"cpu": 8, "mem_mb": 16384, "disk_mb": 100000},
            "agents": [{"agent_id": "a1", "status": "idle", "port": 42000, "tags": ""}],
            "capabilities": {"vnc_enabled": True},
        }

        outbox = Outbox(outbox_repo, _noop_send)
        tunnel = TunnelClient(
            gateway_url="https://gateway.example.com",
            device_id="dev-1",
            device_token="token-1",
            handlers={},
            outbox=outbox,
            hello_extras=hello_extras,
        )

        with patch.object(tunnel, '_connect', new=lambda: tunnel._connect_with_mock_ws(mock_ws)):
            with patch.object(tunnel, '_read_loop', new=lambda ws: asyncio.sleep(0)):
                with patch.object(tunnel, '_heartbeat_loop', new=lambda: asyncio.sleep(0)):
                    try:
                        async def _connect_with_mock_ws(ws):
                            tunnel._ws = ws
                            await tunnel._send_hello_and_sync()

                        tunnel._connect_with_mock_ws = _connect_with_mock_ws
                        await tunnel._connect()
                    except Exception:
                        pass

        hello_frame = None
        sync_frame = None
        for msg in mock_ws.sent:
            frame = json.loads(msg)
            if frame["type"] == "HELLO":
                hello_frame = frame
            elif frame["type"] == "STATE_SYNC":
                sync_frame = frame

        assert hello_frame is not None
        assert hello_frame.get("payload", {}).get("platform") == "Linux"
        assert hello_frame.get("payload", {}).get("device_id") == "dev-1"
        agents = hello_frame.get("payload", {}).get("agents", [])
        assert len(agents) == 1
        assert agents[0]["agent_id"] == "a1"

        assert sync_frame is not None

    async def test_backoff_delay_increases(self, outbox_repo):
        Outbox(outbox_repo, _noop_send)
        tunnel = TunnelClient(
            gateway_url="https://gateway.example.com",
            device_id="dev-1",
            device_token="token-1",
        )

        d1 = tunnel._backoff_delay(1)
        d5 = tunnel._backoff_delay(5)
        assert d1 > 0
        assert d5 >= d1

    async def test_backoff_max_cap(self, outbox_repo):
        Outbox(outbox_repo, _noop_send)
        tunnel = TunnelClient(
            gateway_url="https://gateway.example.com",
            device_id="dev-1",
            device_token="token-1",
        )

        from iagent_device.tunnel.client import RECONNECT_MAX_S
        d100 = tunnel._backoff_delay(100)
        assert d100 <= RECONNECT_MAX_S * (1.2)

    async def test_handler_dispatch(self, outbox_repo):
        handled = {}

        async def handler(ft, payload):
            handled["type"] = ft
            handled["payload"] = payload

        tunnel = TunnelClient(
            gateway_url="https://gateway.example.com",
            device_id="dev-1",
            device_token="token-1",
            handlers={str(FrameType.JOB_DISPATCH): handler},
        )
        tunnel._ws = MockWS()

        frame = {"v": 1, "type": "JOB_DISPATCH", "msg_id": "m1",
                 "payload": {"job_id": "j1", "agent_id": "a1"}}
        await tunnel._handle_frame(frame)

        assert handled.get("type") == "JOB_DISPATCH"
        assert handled["payload"]["job_id"] == "j1"

    async def test_send_with_ack(self, outbox_repo):
        tunnel = TunnelClient(
            gateway_url="https://gateway.example.com",
            device_id="dev-1",
            device_token="token-1",
        )
        tunnel._ws = MockWS()

        future = tunnel.send_with_ack(FrameType.JOB_ACCEPTED, {"job_id": "j1"})
        assert future is not None
        assert len(tunnel._pending_acks) == 1

        msg_id = list(tunnel._pending_acks.keys())[0]
        ack_frame = _make_frame_str(FrameType.ACK, ack_id=msg_id)
        await tunnel._handle_frame(json.loads(ack_frame))

        assert future.done()
        assert msg_id not in tunnel._pending_acks

    async def test_reconnect_attempts_capped(self, outbox_repo):
        Outbox(outbox_repo, _noop_send)
        tunnel = TunnelClient(
            gateway_url="https://gateway.example.com",
            device_id="dev-1",
            device_token="token-1",
            max_reconnect_attempts=2,
        )
        tunnel._running = True
        tunnel._reconnect_attempt = 0

        call_count = 0

        async def fake_connect():
            nonlocal call_count
            call_count += 1
            raise ConnectionError("simulated")

        tunnel._connect = fake_connect

        try:
            await tunnel.run()
        except Exception:
            pass

        assert 3 <= call_count <= 4

    async def test_hello_ack_handler(self, outbox_repo):
        tunnel = TunnelClient(
            gateway_url="https://gateway.example.com",
            device_id="dev-1",
            device_token="token-1",
        )
        tunnel._ws = MockWS()

        frame = {"v": 1, "type": "HELLO_ACK", "msg_id": "m1",
                 "payload": {"status": "ok"}}
        await tunnel._handle_frame(frame)

    async def test_error_handler(self, outbox_repo):
        tunnel = TunnelClient(
            gateway_url="https://gateway.example.com",
            device_id="dev-1",
            device_token="token-1",
        )
        tunnel._ws = MockWS()

        frame = {"v": 1, "type": "ERROR", "msg_id": "m1",
                 "payload": {"code": "AUTH_FAILED", "message": "bad token"}}
        await tunnel._handle_frame(frame)
