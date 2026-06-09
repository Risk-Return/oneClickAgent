"""Credential handling (login cookies): pass-through relay.
Never persists cookies to disk. In-memory only.
"""

import asyncio
import hashlib
import logging

from iagent_device.tunnel.codec import FrameType
from iagent_device.tunnel.outbox import Outbox
from iagent_device.docker.manager import DockerManager

logger = logging.getLogger(__name__)


class CredRelay:
    def __init__(self, docker_mgr: DockerManager, outbox: Outbox):
        self.docker = docker_mgr
        self.outbox = outbox
        self._injected: dict[str, set[str]] = {}
        self._injection_event = asyncio.Event()

    def _record_injection(self, job_id: str, credential_id: str) -> None:
        if not job_id or not credential_id:
            return
        self._injected.setdefault(job_id, set()).add(credential_id)
        self._injection_event.set()

    async def wait_for_injections(self, job_id: str, credential_ids: list[str], timeout: float) -> bool:
        """Block until every credential_id has been injected for job_id, or timeout.
        Returns True if all injected, False on timeout (job proceeds best-effort).
        """
        expected = {c for c in credential_ids if c}
        if not expected:
            return True
        deadline = asyncio.get_event_loop().time() + timeout
        while True:
            if expected.issubset(self._injected.get(job_id, set())):
                return True
            remaining = deadline - asyncio.get_event_loop().time()
            if remaining <= 0:
                missing = expected - self._injected.get(job_id, set())
                logger.warning("timed out waiting for credential injection job=%s missing=%s", job_id, missing)
                return False
            self._injection_event.clear()
            try:
                await asyncio.wait_for(self._injection_event.wait(), timeout=remaining)
            except asyncio.TimeoutError:
                pass

    async def handle_cred_push(self, payload: dict):
        job_id = payload.get("job_id", "")
        credential_id = payload.get("credential_id", "")
        agent_id = payload.get("agent_id", "")
        storage_state = payload.get("storage_state", "")
        sha256 = payload.get("sha256", "")

        import base64
        plaintext = base64.b64decode(storage_state)
        actual = hashlib.sha256(plaintext).hexdigest()
        if actual != sha256:
            await self.outbox.enqueue_and_send(FrameType.CRED_PUSH_ACK, {
                "job_id": job_id,
                "credential_id": credential_id,
                "status": "ERROR",
                "error": "SHA-256 mismatch",
            })
            return

        client = self.docker.get_client(agent_id)
        if not client:
            await self.outbox.enqueue_and_send(FrameType.CRED_PUSH_ACK, {
                "job_id": job_id,
                "credential_id": credential_id,
                "status": "ERROR",
                "error": "agent not reachable",
            })
            return

        try:
            state = plaintext.decode("utf-8")
            await client.set_browser_state(state)
            self._record_injection(job_id, credential_id)
            await self.outbox.enqueue_and_send(FrameType.CRED_PUSH_ACK, {
                "job_id": job_id,
                "credential_id": credential_id,
                "status": "INJECTED",
            })
        except Exception as e:
            await self.outbox.enqueue_and_send(FrameType.CRED_PUSH_ACK, {
                "job_id": job_id,
                "credential_id": credential_id,
                "status": "ERROR",
                "error": str(e),
            })

    async def handle_cred_capture(self, payload: dict):
        session_id = payload.get("session_id", "")
        agent_id = payload.get("agent_id", "")
        origin = payload.get("origin", "")
        label = payload.get("label", "")
        job_id = payload.get("job_id", "")

        client = self.docker.get_client(agent_id)
        if not client:
            await self.outbox.enqueue_and_send(FrameType.CRED_CAPTURE_ACK, {
                "session_id": session_id,
                "status": "error",
                "error": "agent not reachable",
            })
            return

        try:
            result = await client.get_browser_state(origin)
            storage_state = result.get("storage_state", "")
            if isinstance(storage_state, dict):
                import json as _json
                storage_state = _json.dumps(storage_state)
            if not storage_state:
                storage_state = ""
            sha256 = hashlib.sha256(storage_state.encode()).hexdigest()
            import base64
            encoded = base64.b64encode(storage_state.encode()).decode()

            await self.outbox.enqueue_and_send(FrameType.CRED_CAPTURE, {
                "session_id": session_id,
                "job_id": job_id,
                "agent_id": agent_id,
                "label": label,
                "origin": origin,
                "data": encoded,
                "sha256": sha256,
            })
        except Exception as e:
            await self.outbox.enqueue_and_send(FrameType.CRED_CAPTURE_ACK, {
                "session_id": session_id,
                "status": "error",
                "error": str(e),
            })
