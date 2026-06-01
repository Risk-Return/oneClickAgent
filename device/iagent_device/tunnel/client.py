"""Reverse WSS tunnel client: dial-out to gateway, authenticate with device_token,
send HELLO/STATE_SYNC, maintain heartbeat loop, reconnect with exponential backoff + jitter.
"""

import asyncio
import json
import logging
import random

import websockets
from websockets.asyncio.client import connect as ws_connect

from iagent_device.tunnel.codec import (
    FrameType, encode_frame, decode_frame, decode_payload, FRAME_MAX_SIZE,
)
from iagent_device.tunnel.outbox import Outbox

logger = logging.getLogger(__name__)


class TunnelClient:
    def __init__(
        self,
        gateway_url: str,
        device_id: str,
        device_token: str,
        heartbeat_s: int = 15,
        handlers: dict | None = None,
        outbox: Outbox | None = None,
    ):
        self.gateway_url = gateway_url.replace("https://", "wss://").replace("http://", "ws://")
        if not self.gateway_url.endswith("/tunnel"):
            self.gateway_url = self.gateway_url.rstrip("/") + "/tunnel"
        self.device_id = device_id
        self.device_token = device_token
        self.heartbeat_s = heartbeat_s
        self.handlers = handlers or {}
        self.outbox = outbox
        self._ws = None
        self._running = False
        self._pending_acks: dict[str, asyncio.Future] = {}

    async def run(self):
        self._running = True
        while self._running:
            try:
                await self._connect()
            except asyncio.CancelledError:
                break
            except Exception:
                logger.exception("tunnel disconnected, reconnecting...")
                delay = random.uniform(1, 15)
                logger.info("reconnecting in %.1fs", delay)
                await asyncio.sleep(delay)

    async def _connect(self):
        async with ws_connect(
            self.gateway_url,
            additional_headers={"Authorization": f"Bearer {self.device_token}"},
            subprotocols=["oneClickAgent.tunnel.v1"],
            max_size=FRAME_MAX_SIZE,
        ) as ws:
            self._ws = ws
            await self._send_hello_and_sync()
            await asyncio.gather(
                self._read_loop(ws),
                self._heartbeat_loop(),
            )

    async def _send_hello_and_sync(self):
        await self._send(FrameType.HELLO, {
            "device_id": self.device_id,
            "agent_count": 0,
            "agents": [],
        })
        await self._send(FrameType.STATE_SYNC, {"jobs": [], "agents": []})

    async def _read_loop(self, ws):
        async for msg in ws:
            try:
                frame = decode_frame(msg)
                await self._handle_frame(frame)
            except Exception:
                logger.exception("frame handling error")

    async def _handle_frame(self, frame: dict):
        frame_type = frame["type"]
        msg_id = frame.get("msg_id", "")

        # Always ACK non-ACK frames
        if frame_type != FrameType.ACK:
            await self._send_ack(msg_id)

        # Handle ack from gateway
        if frame_type == FrameType.ACK:
            ack_id = frame.get("ack_id", "")
            if ack_id in self._pending_acks:
                self._pending_acks[ack_id].set_result(True)
                if self.outbox:
                    self.outbox.ack(ack_id)
            return

        if frame_type == FrameType.PING:
            await self._send(FrameType.PONG, {})
            return

        if frame_type == FrameType.PONG:
            return

        # Dispatch to registered handlers
        handler = self.handlers.get(frame_type) or self.handlers.get("*")
        if handler:
            payload = decode_payload(frame)
            await handler(frame_type, payload)

    async def _heartbeat_loop(self):
        while self._ws and self._running:
            try:
                await self._send(FrameType.PING, {})
            except Exception:
                break
            await asyncio.sleep(self.heartbeat_s)

    async def _send(self, frame_type: FrameType, payload: dict, ack_id: str | None = None):
        if not self._ws:
            return
        msg = encode_frame(frame_type, payload, ack_id)
        await self._ws.send(msg)

    def send_with_ack(self, frame_type: FrameType, payload: dict) -> asyncio.Future | None:
        if not self._ws:
            return None
        msg_id = encode_frame(frame_type, payload)
        # Parse the msg_id from the encoded frame
        frame_dict = json.loads(msg_id)
        msg_id_val = frame_dict["msg_id"]
        future = asyncio.get_event_loop().create_future()
        self._pending_acks[msg_id_val] = future
        asyncio.create_task(self._ws.send(msg_id))
        return future

    async def _send_ack(self, msg_id: str):
        await self._send(FrameType.ACK, {}, ack_id=msg_id)

    async def close(self):
        self._running = False
        if self._ws:
            await self._ws.close()
