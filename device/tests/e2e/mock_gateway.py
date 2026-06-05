"""Mock cloud gateway WebSocket server for device e2e tests.

Handles the iagent.tunnel.v1 protocol: HELLO_ACK, PING/PONG, ACK,
and captures/relays all other frames for test assertions.

Enrollment is handled by the conftest fixture directly injecting into
the device's SQLite database -- no HTTP server needed.
"""

import asyncio
import json
import logging
import uuid
from dataclasses import dataclass, field

import websockets
from websockets.asyncio.server import serve as ws_serve

logger = logging.getLogger(__name__)


@dataclass
class MockGateway:
    """WebSocket server that speaks the iagent tunnel protocol."""

    host: str = "127.0.0.1"
    port: int = 0
    _conns: dict[str, "websockets.ServerConnection"] = field(default_factory=dict)
    _token_to_device: dict[str, str] = field(default_factory=dict)
    _frame_queues: dict[str, asyncio.Queue] = field(default_factory=dict)
    _server: asyncio.AbstractServer | None = None
    _server_task: asyncio.Task | None = None
    _ready: asyncio.Event = field(default_factory=asyncio.Event)
    _last_hello: dict = field(default_factory=dict)

    @property
    def base_url(self) -> str:
        return f"ws://{self.host}:{self.port}"

    def register_device(self, device_id: str, token: str):
        self._token_to_device[token] = device_id

    async def _handler(self, ws: "websockets.ServerConnection"):
        auth = ""
        if hasattr(ws, "request") and ws.request and ws.request.headers:
            auth = ws.request.headers.get("Authorization", "")

        if not auth.startswith("Bearer "):
            await ws.close(4001, "auth required")
            return

        token = auth[7:] if auth else ""
        device_id = self._token_to_device.get(token)
        if not device_id:
            await ws.close(4001, "invalid token")
            return

        self._conns[device_id] = ws
        self._frame_queues[device_id] = asyncio.Queue()

        try:
            async for raw in ws:
                try:
                    frame = json.loads(raw)
                except json.JSONDecodeError:
                    continue
                await self._handle_frame(device_id, frame)
        except websockets.ConnectionClosed:
            pass
        finally:
            self._conns.pop(device_id, None)
            self._frame_queues.pop(device_id, None)

    async def _handle_frame(self, device_id: str, frame: dict):
        ft = frame.get("type", "")
        msg_id = frame.get("msg_id", "")
        payload = frame.get("payload", {})

        if ft != "ACK":
            await self._send(device_id, {"v": 1, "type": "ACK", "ack_id": msg_id, "ts": self._ts()})

        if ft == "HELLO":
            self._last_hello[device_id] = payload
            await self._send(device_id, {
                "v": 1, "type": "HELLO_ACK", "msg_id": str(uuid.uuid4()), "ts": self._ts(),
                "payload": {
                    "server_time": self._ts(),
                    "session_id": str(uuid.uuid4()),
                    "config": {"heartbeat_s": 15, "max_frame_bytes": 1048576},
                },
            })
            return

        if ft == "PING":
            await self._send(device_id, {"v": 1, "type": "PONG", "msg_id": str(uuid.uuid4()), "ts": self._ts()})
            return

        if ft in ("PONG", "ACK"):
            return

        self._frame_queues.get(device_id, asyncio.Queue()).put_nowait((ft, payload))

    async def _send(self, device_id: str, frame: dict):
        ws = self._conns.get(device_id)
        if ws:
            try:
                await ws.send(json.dumps(frame))
            except websockets.ConnectionClosed:
                pass

    async def send_frame(self, device_id: str, frame_type: str, payload: dict | None = None):
        frame = {
            "v": 1, "type": frame_type, "msg_id": str(uuid.uuid4()), "ts": self._ts(),
        }
        if payload:
            frame["payload"] = payload
        await self._send(device_id, frame)

    async def recv_frame(self, device_id: str, frame_type: str | None = None, timeout: float = 10) -> tuple:
        q = self._frame_queues.get(device_id)
        if not q:
            raise RuntimeError(f"device {device_id} not connected")
        deadline = asyncio.get_event_loop().time() + timeout
        while True:
            remaining = deadline - asyncio.get_event_loop().time()
            if remaining <= 0:
                raise asyncio.TimeoutError(f"timeout waiting for frame_type={frame_type}")
            try:
                ft, payload = await asyncio.wait_for(q.get(), timeout=remaining)
                if frame_type is None or ft == frame_type:
                    return ft, payload
            except TimeoutError:
                raise

    def disconnect(self, device_id: str):
        ws = self._conns.pop(device_id, None)
        if ws:
            asyncio.create_task(ws.close())

    def has_connection(self, device_id: str) -> bool:
        return device_id in self._conns

    async def start(self):
        self._server = await ws_serve(self._handler, self.host, self.port)
        addrs = [s.getsockname() for s in self._server.sockets]
        for addr in addrs:
            self.host = addr[0]
            self.port = addr[1]
            break
        logger.info("mock gateway listening on %s", self.base_url)
        self._ready.set()

    async def stop(self):
        for device_id in list(self._conns):
            self.disconnect(device_id)
        if self._server:
            self._server.close()
            await self._server.wait_closed()
        logger.info("mock gateway stopped")

    @staticmethod
    def _ts() -> int:
        return int(asyncio.get_event_loop().time() * 1000)
