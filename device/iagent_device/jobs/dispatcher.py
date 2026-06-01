"""Job dispatcher: receives JOB_DISPATCH from tunnel for a pre-allocated agent,
dispatches to that agent container, relays progress/results back through outbox,
handles cancellation. Signals pool reaper on terminal to release agent back to IDLE.
"""

import asyncio
import logging

from iagent_device.tunnel.codec import FrameType, new_msg_id
from iagent_device.tunnel.outbox import Outbox
from iagent_device.store.repositories import JobRepo, AgentRepo
from iagent_device.docker.manager import DockerManager

logger = logging.getLogger(__name__)


class JobDispatcher:
    def __init__(
        self,
        job_repo: JobRepo,
        agent_repo: AgentRepo,
        docker_mgr: DockerManager,
        outbox: Outbox,
    ):
        self.job_repo = job_repo
        self.agent_repo = agent_repo
        self.docker = docker_mgr
        self.outbox = outbox

    async def handle_job_dispatch(self, payload: dict):
        job_id = payload["job_id"]
        agent_id = payload["agent_id"]
        user_id = payload.get("user_id", "")
        command = payload.get("command", "")
        skill_id = payload.get("skill_id", "")
        credential_ids_list = payload.get("credential_ids", [])
        credential_ids_str = ",".join(credential_ids_list) if credential_ids_list else ""

        self.job_repo.create(job_id, agent_id, user_id, command, skill_id, credential_ids_str)
        self.agent_repo.allocate(agent_id, user_id, job_id)

        await self.outbox.enqueue_and_send(FrameType.JOB_ACCEPTED, {"job_id": job_id})

        client = self.docker.get_client(agent_id)
        if not client:
            await self.outbox.enqueue_and_send(FrameType.JOB_REJECTED, {
                "job_id": job_id,
                "reason": "agent not reachable",
            })
            return

        try:
            result = await client.create_job(job_id, command, {}, "", skill_id)
            self.job_repo.update_status(job_id, "running")
            await self.outbox.enqueue_and_send(FrameType.JOB_PROGRESS, {
                "job_id": job_id,
                "status": "running",
                "percent": 0,
                "message": "Job started",
            })
            # Simulate progress then result
            await asyncio.sleep(1)
            await self.outbox.enqueue_and_send(FrameType.JOB_PROGRESS, {
                "job_id": job_id,
                "status": "running",
                "percent": 50,
                "message": "Processing...",
            })
            await asyncio.sleep(1)
            self.job_repo.update_status(job_id, "succeeded")
            await self.outbox.enqueue_and_send(FrameType.JOB_RESULT, {
                "job_id": job_id,
                "status": "succeeded",
                "result": {"output": result},
            })
        except Exception as e:
            self.job_repo.update_status(job_id, "failed")
            await self.outbox.enqueue_and_send(FrameType.JOB_RESULT, {
                "job_id": job_id,
                "status": "failed",
                "error_msg": str(e),
            })
        finally:
            self.agent_repo.release(agent_id)

    async def handle_job_cancel(self, payload: dict):
        job_id = payload.get("job_id", "")
        agent_id = payload.get("agent_id", "")

        if agent_id:
            client = self.docker.get_client(agent_id)
            if client:
                await client.cancel_job(job_id)

        self.job_repo.update_status(job_id, "cancelled")
        self.agent_repo.release(agent_id)
