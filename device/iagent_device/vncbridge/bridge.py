"""VNC session bridge: on VNC_OPEN, dials the gateway session WS,
connects TCP to the agent's RFB port, and bridges bytes both ways.
"""

import asyncio
import logging
from typing import cast

from websockets.asyncio.client import connect as ws_connect
from websockets.typing import Subprotocol

from iagent_device.tunnel.codec import FrameType
from iagent_device.tunnel.outbox import Outbox
from iagent_device.store.repositories import VNCSessionRepo
from iagent_device.docker.manager import DockerManager

logger = logging.getLogger(__name__)

BACKPRESSURE_BUFFER = 256 * 1024


class VNCBridge:
    def __init__(
        self,
        vnc_repo: VNCSessionRepo,
        docker_mgr: DockerManager,
        outbox: Outbox,
        dial_timeout: int = 15,
    ):
        self.repo = vnc_repo
        self.docker = docker_mgr
        self.outbox = outbox
        self.dial_timeout = dial_timeout
        self._sessions: dict[str, asyncio.Task] = {}

    async def handle_vnc_open(self, payload: dict):
        session_id = payload["session_id"]
        agent_id = payload.get("agent_id", "")
        relay_url = payload["relay_url"]
        session_token = payload["session_token"]
        ttl_s = payload.get("ttl_s", 0)

        self.repo.create(session_id, "", agent_id, relay_url, session_token)

        client = self.docker.get_client(agent_id)
        if not client:
            return

        try:
            vnc_info = await client.vnc_start()
            rfb_port = vnc_info.get("rfb_port", 5900)
            rfb_password = vnc_info.get("rfb_password", "")

            self.repo.update_status(session_id, "ready", rfb_port, rfb_password)

            await self.outbox.enqueue_and_send(FrameType.VNC_OPENED, {
                "session_id": session_id,
                "status": "ready",
                "rfb_password": rfb_password,
            })

            task = asyncio.create_task(
                self._bridge(session_id, relay_url, session_token, rfb_port, agent_id, ttl_s)
            )
            self._sessions[session_id] = task
        except Exception as e:
            logger.exception("vnc open failed for session %s", session_id)
            await self.outbox.enqueue_and_send(FrameType.VNC_OPENED, {
                "session_id": session_id,
                "status": "error",
                "error": str(e),
            })

    async def _bridge(self, session_id: str, relay_url: str, session_token: str, rfb_port: int, agent_id: str, ttl_s: int = 0):
        try:
            async with ws_connect(
                relay_url,
                additional_headers={"Authorization": f"Bearer {session_token}"},
                subprotocols=[cast(Subprotocol, "iagent.session.v1")],
                max_size=BACKPRESSURE_BUFFER * 2,
            ) as ws:
                reader, writer = await asyncio.wait_for(
                    asyncio.open_connection("127.0.0.1", rfb_port),
                    timeout=self.dial_timeout,
                )

                async def ws_to_tcp():
                    try:
                        async for msg in ws:
                            writer.write(msg if isinstance(msg, bytes) else msg.encode())
                            await writer.drain()
                    except Exception:
                        pass

                async def tcp_to_ws():
                    try:
                        while True:
                            data = await reader.read(65536)
                            if not data:
                                break
                            await ws.send(data)
                    except Exception:
                        pass

                if ttl_s > 0:
                    await asyncio.wait_for(
                        asyncio.gather(ws_to_tcp(), tcp_to_ws()),
                        timeout=ttl_s,
                    )
                else:
                    await asyncio.gather(ws_to_tcp(), tcp_to_ws())
        except asyncio.TimeoutError:
            logger.info("vnc session %s expired (ttl=%ds)", session_id, ttl_s)
        except Exception:
            logger.exception("vnc bridge error for session %s", session_id)
        finally:
            self.repo.close(session_id)
            client = self.docker.get_client(agent_id)
            if client:
                await client.vnc_stop()
            await self.outbox.enqueue_and_send(FrameType.VNC_CLOSE, {
                "session_id": session_id,
                "reason": "bridge closed",
            })

    async def handle_vnc_close(self, payload: dict):
        session_id = payload.get("session_id", "")
        task = self._sessions.pop(session_id, None)
        if task:
            task.cancel()
        self.repo.close(session_id)
