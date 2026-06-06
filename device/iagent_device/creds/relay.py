"""Credential handling (login cookies): pass-through relay.
Never persists cookies to disk. In-memory only.
"""

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

    async def inject_credential(self, payload: dict):
        job_id = payload.get("job_id", "")
        credential_id = payload.get("credential_id", "")
        agent_id = payload.get("agent_id", "")
        storage_state = payload.get("storage_state", "")
        sha256 = payload.get("sha256", "")

        if not storage_state:
            return

        import base64
        plaintext = base64.b64decode(storage_state)
        actual = hashlib.sha256(plaintext).hexdigest()
        if actual != sha256:
            logger.error("credential %s sha256 mismatch for job %s", credential_id, job_id)
            return

        client = self.docker.get_client(agent_id)
        if client:
            state = plaintext.decode("utf-8")
            await client.set_browser_state(state)

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
