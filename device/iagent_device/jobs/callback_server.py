"""Lightweight HTTP callback server that receives agent progress events
and relays them to the gateway via outbox.

The agent POSTs to /jobs/{job_id}/events with progress data.
The device receives these and sends JOB_PROGRESS frames to the gateway.
"""

import asyncio
import json
import logging
from urllib.parse import urlparse

logger = logging.getLogger(__name__)

MAX_BODY = 65536


class CallbackServer:
    def __init__(
        self,
        host: str = "127.0.0.1",
        port: int = 0,
        outbox=None,
        advertise_host: str | None = None,
    ):
        self.host = host
        self.port = port
        self._advertise_host = advertise_host
        self._outbox = outbox
        self._server: asyncio.AbstractServer | None = None

    @property
    def callback_url(self) -> str:
        h = self._advertise_host or self.host
        return f"http://{h}:{self.port}"

    async def start(self):
        self._server = await asyncio.start_server(
            self._handle, self.host, self.port,
        )
        addr = self._server.sockets[0].getsockname()
        self.host = addr[0]
        self.port = addr[1]
        logger.info("callback server listening on %s", self.callback_url)

    async def stop(self):
        if self._server:
            self._server.close()
            await self._server.wait_closed()

    async def _handle(self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter):
        try:
            raw = await asyncio.wait_for(reader.readuntil(b"\r\n\r\n"), timeout=10)
        except (asyncio.TimeoutError, asyncio.IncompleteReadError):
            writer.close()
            return

        request = raw.decode(errors="replace")
        lines = request.split("\r\n")
        if not lines:
            writer.close()
            return

        first = lines[0].split(" ")
        method = first[0] if first else ""
        path = first[1] if len(first) > 1 else "/"

        body_start = request.find("\r\n\r\n")
        body = ""
        if body_start >= 0:
            body = request[body_start + 4:]
            content_length = 0
            for line in lines[1:]:
                if line.lower().startswith("content-length:"):
                    try:
                        content_length = int(line.split(":", 1)[1].strip())
                    except ValueError:
                        pass
                    break
            remaining = max(0, content_length - len(body))
            if remaining > 0:
                try:
                    more = await asyncio.wait_for(
                        reader.read(min(remaining, MAX_BODY)), timeout=5,
                    )
                    body += more.decode(errors="replace")
                except (asyncio.TimeoutError, asyncio.IncompleteReadError):
                    pass

        if method == "POST" and path.startswith("/jobs/") and path.endswith("/events"):
            parts = path.split("/")
            job_id = parts[2] if len(parts) > 2 else ""
            try:
                event = json.loads(body)
            except json.JSONDecodeError:
                event = {}
            await self._handle_event(job_id, event)

        self._send_response(writer, 204)

    async def _handle_event(self, job_id: str, event: dict):
        if not self._outbox:
            return
        percent = event.get("percent", event.get("progress", 0))
        message = event.get("message", event.get("status", ""))
        seq = event.get("event_seq", 0)
        terminal = event.get("terminal", False)
        status = event.get("status", "running")

        if terminal:
            result_data = event.get("result", {})
            error_data = event.get("error", {})
            await self._outbox.enqueue_and_send("JOB_RESULT", {
                "job_id": job_id,
                "status": status,
                "result": result_data,
                "error_msg": error_data.get("message", "") if status != "succeeded" else "",
            })
        else:
            await self._outbox.enqueue_and_send("JOB_PROGRESS", {
                "job_id": job_id,
                "status": "running",
                "percent": percent,
                "message": message,
                "event_seq": seq,
            })

    @staticmethod
    def _send_response(writer: asyncio.StreamWriter, status: int):
        msg = f"HTTP/1.1 {status} OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
        writer.write(msg.encode())
        writer.close()
