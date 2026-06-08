"""Reverse WSS tunnel client: dial-out to gateway, authenticate with device_token,
send HELLO/STATE_SYNC, maintain heartbeat loop, reconnect with exponential backoff + jitter,
retransmit unacked frames with backoff.
"""

import asyncio
import json
import logging
import random
import time

from websockets.asyncio.client import connect as ws_connect

from iagent_device.tunnel.codec import (
    FrameType, encode_frame, decode_frame, decode_payload, FRAME_MAX_SIZE, new_msg_id,
)
from iagent_device.tunnel.outbox import Outbox

logger = logging.getLogger(__name__)

RECONNECT_BASE_S = 1.0
RECONNECT_MAX_S = 30.0
RECONNECT_JITTER = 0.2

ACK_RETRANSMIT_BASE_S = 1.0
ACK_RETRANSMIT_MAX_RETRIES = 3


class _PendingSend:
    __slots__ = ("msg_id", "frame_json", "sent_at", "retries", "future")
    msg_id: str
    frame_json: str
    sent_at: float
    retries: int
    future: asyncio.Future


class TunnelClient:
    def __init__(
        self,
        gateway_url: str,
        device_id: str,
        device_token: str,
        heartbeat_s: int = 15,
        handlers: dict | None = None,
        outbox: Outbox | None = None,
        max_reconnect_attempts: int = 0,
        hello_extras: dict | None = None,
        hello_builder: object = None,
    ):
        self.gateway_url = gateway_url.replace("https://", "wss://").replace("http://", "ws://")
        if not self.gateway_url.endswith("/tunnel"):
            self.gateway_url = self.gateway_url.rstrip("/") + "/tunnel"
        self.device_id = device_id
        self.device_token = device_token
        self.heartbeat_s = heartbeat_s
        self.handlers = handlers or {}
        self.outbox = outbox
        self.max_reconnect_attempts = max_reconnect_attempts
        self.hello_extras = hello_extras or {}
        self.hello_builder = hello_builder
        self._ws = None
        self._running = False
        self._pending_acks: dict[str, _PendingSend] = {}
        self._reconnect_attempt = 0
        self._processed_msg_ids: set[str] = set()
        self._processed_msg_ids_max = 10000
        self._retransmit_task: asyncio.Task | None = None

    async def run(self):
        self._running = True
        self._reconnect_attempt = 0
        while self._running:
            try:
                await self._connect()
                self._reconnect_attempt = 0
            except asyncio.CancelledError:
                break
            except Exception:
                self._reconnect_attempt += 1
                if self.max_reconnect_attempts > 0 and self._reconnect_attempt > self.max_reconnect_attempts:
                    logger.error("exceeded max reconnect attempts (%d), giving up", self.max_reconnect_attempts)
                    break
                logger.exception("tunnel disconnected, reconnecting...")
                delay = self._backoff_delay(self._reconnect_attempt)
                logger.info("reconnecting in %.1fs (attempt %d)", delay, self._reconnect_attempt)
                await asyncio.sleep(delay)

    def _backoff_delay(self, attempt: int) -> float:
        base = min(RECONNECT_MAX_S, RECONNECT_BASE_S * (2 ** attempt))
        jitter = base * random.uniform(-RECONNECT_JITTER, RECONNECT_JITTER)
        return max(1.0, base + jitter)

    async def _connect(self):
        async with ws_connect(
            self.gateway_url,
            additional_headers={"Authorization": f"Bearer {self.device_token}"},
            subprotocols=["iagent.tunnel.v1"],
            max_size=FRAME_MAX_SIZE,
            ping_interval=None,
        ) as ws:
            self._ws = ws
            self._pending_acks.clear()
            await self._send_hello_and_sync()
            if self.outbox:
                await self.outbox.flush()
                logger.info("outbox flushed after reconnect")
            self._retransmit_task = asyncio.create_task(self._retransmit_loop())
            await asyncio.gather(
                self._read_loop(ws),
                self._heartbeat_loop(),
            )

    async def _send_hello_and_sync(self):
        agents = self.hello_extras.get("agents", [])
        hello_payload = {
            "device_id": self.device_id,
            "platform": self.hello_extras.get("platform", ""),
            "agent_version": self.hello_extras.get("agent_version", ""),
            "agent_count": len(agents),
            "resources": self.hello_extras.get("resources", {}),
            "agents": agents,
            "capabilities": self.hello_extras.get("capabilities", {}),
        }
        if self.hello_builder:
            try:
                extra = await self.hello_builder() if asyncio.iscoroutinefunction(self.hello_builder) else self.hello_builder()
                if isinstance(extra, dict):
                    hello_payload.update(extra)
            except Exception:
                logger.exception("hello_builder failed")

        jobs = self.hello_extras.get("jobs", [])
        await self._send(FrameType.HELLO, hello_payload)
        await self._send(FrameType.STATE_SYNC, {"jobs": jobs, "agents": agents})

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

        if frame_type != FrameType.ACK:
            await self._send_ack(msg_id)

        # Idempotency: skip already-processed msg_ids
        if frame_type not in (FrameType.ACK, FrameType.PING, FrameType.PONG):
            if msg_id in self._processed_msg_ids:
                logger.debug("duplicate frame, already processed: %s", msg_id)
                return
            if len(self._processed_msg_ids) >= self._processed_msg_ids_max:
                self._processed_msg_ids.clear()
            self._processed_msg_ids.add(msg_id)

        if frame_type == FrameType.ACK:
            ack_id = frame.get("ack_id", "")
            entry = self._pending_acks.pop(ack_id, None)
            if entry is not None:
                if not entry.future.done():
                    entry.future.set_result(True)
                if self.outbox:
                    self.outbox.ack(ack_id)
            return

        if frame_type == FrameType.PING:
            await self._send(FrameType.PONG, {})
            return

        if frame_type == FrameType.PONG:
            return

        if frame_type == FrameType.HELLO_ACK:
            payload = decode_payload(frame)
            config = payload.get("config", {})
            if config.get("heartbeat_s"):
                self.heartbeat_s = config["heartbeat_s"]
            server_time = payload.get("server_time", 0)
            logger.info("received HELLO_ACK from gateway (server_time=%s, session_id=%s, heartbeat_s=%s)",
                        server_time, payload.get("session_id", ""), self.heartbeat_s)
            return

        if frame_type == FrameType.ERROR:
            error_msg = frame.get("payload", {}).get("message", "gateway error")
            logger.error("gateway error frame: %s", error_msg)
            return

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

    async def _send(self, frame_type: FrameType, payload: dict, ack_id: str | None = None, msg_id: str | None = None):
        if not self._ws:
            return
        msg = encode_frame(frame_type, payload, ack_id, msg_id)
        await self._ws.send(msg)

    def send_with_ack(self, frame_type: FrameType, payload: dict) -> asyncio.Future | None:
        if not self._ws:
            return None
        msg_id_val = new_msg_id()
        frame = {
            "v": 1,
            "type": str(frame_type),
            "msg_id": msg_id_val,
            "ts": int(time.time() * 1000),
        }
        if payload:
            frame["payload"] = payload
        frame_json = json.dumps(frame)
        future: asyncio.Future = asyncio.get_event_loop().create_future()
        self._pending_acks[msg_id_val] = _PendingSend()
        self._pending_acks[msg_id_val].msg_id = msg_id_val
        self._pending_acks[msg_id_val].frame_json = frame_json
        self._pending_acks[msg_id_val].sent_at = time.monotonic()
        self._pending_acks[msg_id_val].retries = 0
        self._pending_acks[msg_id_val].future = future
        asyncio.create_task(self._ws.send(frame_json))
        return future

    async def _send_ack(self, msg_id: str):
        await self._send(FrameType.ACK, {}, ack_id=msg_id)

    async def _retransmit_loop(self):
        """Retransmit unacked frames with exponential backoff (1s/2s/4s, max 3 retries)."""
        while self._ws and self._running:
            await asyncio.sleep(1.0)
            now = time.monotonic()
            to_retry: list[_PendingSend] = []
            to_fail: list[str] = []

            for msg_id, entry in list(self._pending_acks.items()):
                if entry.retries >= ACK_RETRANSMIT_MAX_RETRIES:
                    to_fail.append(msg_id)
                    continue
                backoff = ACK_RETRANSMIT_BASE_S * (1 << entry.retries)
                if now - entry.sent_at >= backoff:
                    entry.retries += 1
                    entry.sent_at = now
                    to_retry.append(entry)

            for entry in to_retry:
                logger.debug("retransmitting unacked frame %s (retry %d)", entry.msg_id, entry.retries)
                try:
                    await self._ws.send(entry.frame_json)
                except Exception:
                    logger.exception("retransmit send failed for %s", entry.msg_id)

            for msg_id in to_fail:
                entry = self._pending_acks.pop(msg_id, None)
                if entry is not None and not entry.future.done():
                    logger.error("frame exceeded max retransmit retries: %s", msg_id)
                    entry.future.set_exception(TimeoutError(f"max retransmit retries exceeded for {msg_id}"))

    async def close(self):
        self._running = False
        if self._ws:
            await self._ws.close()
