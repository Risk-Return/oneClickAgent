"""Job dispatcher: receives JOB_DISPATCH from tunnel for a pre-allocated agent,
dispatches to that agent container, relays progress/results back through outbox,
handles cancellation. Signals pool reaper on terminal to release agent back to IDLE.
"""

import asyncio
import logging

from iagent_device.tunnel.codec import FrameType
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
        stager=None,
        puller=None,
        cred_relay=None,
        callback_url: str = "",
    ):
        self.job_repo = job_repo
        self.agent_repo = agent_repo
        self.docker = docker_mgr
        self.outbox = outbox
        self.stager = stager
        self.puller = puller
        self.cred_relay = cred_relay
        self.callback_url = callback_url

    async def _wait_for_files(self, job_id: str, timeout: float = 30.0):
        if not self.stager:
            return
        start = asyncio.get_event_loop().time()
        while asyncio.get_event_loop().time() - start < timeout:
            files = self.stager.repo.list_by_job(job_id)
            if not files:
                return
            pending = self.stager.repo.count_pending(job_id)
            if pending == 0:
                return
            await asyncio.sleep(0.5)

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
            if credential_ids_list and self.cred_relay:
                for cred in credential_ids_list:
                    cred_payload = {
                        "job_id": job_id,
                        "credential_id": cred,
                        "agent_id": agent_id,
                    }
                    await self.cred_relay.inject_credential(cred_payload)

            await self._wait_for_files(job_id)

            await client.create_job(job_id, command, {}, self.callback_url, skill_id, workspace_dir=f"/workspaces/{job_id}")
            self.job_repo.update_status(job_id, "running")
            await self.outbox.enqueue_and_send(FrameType.JOB_PROGRESS, {
                "job_id": job_id,
                "status": "running",
                "percent": 0,
                "message": "Job started",
            })

            await self._poll_progress(client, job_id)

        except Exception as e:
            self.job_repo.update_status(job_id, "failed")
            await self.outbox.enqueue_and_send(FrameType.JOB_RESULT, {
                "job_id": job_id,
                "status": "failed",
                "error_msg": str(e),
            })
        finally:
            if self.stager:
                try:
                    await self.stager.cleanup(job_id)
                except Exception:
                    logger.exception("cleanup failed for job %s", job_id)
                await self.outbox.enqueue_and_send(FrameType.FILE_PURGED, {
                    "job_id": job_id,
                })
            await self.docker.reaper_cleanup(agent_id, job_id)
            self.agent_repo.release(agent_id)
            await self.outbox.enqueue_and_send(FrameType.AGENT_STATUS, {
                "agent_id": agent_id,
                "status": "idle",
                "health": "healthy",
                "restarts": 0,
                "usage": {"cpu_pct": 0, "mem_mb": 0, "disk_mb": 0},
                "ts": int(asyncio.get_event_loop().time() * 1000),
            })

    async def _poll_progress(self, client, job_id: str):
        last_percent = 0
        for _ in range(300):
            try:
                status_data = await client.get_job(job_id)
                status = status_data.get("status", "running")
                percent = status_data.get("percent", last_percent)
                message = status_data.get("message", "")

                if percent != last_percent or status in ("succeeded", "failed", "cancelled"):
                    await self.outbox.enqueue_and_send(FrameType.JOB_PROGRESS, {
                        "job_id": job_id,
                        "status": status,
                        "percent": percent,
                        "message": message,
                    })
                    last_percent = percent

                if status in ("succeeded", "failed", "cancelled"):
                    result_data = status_data.get("result", {})
                    error_data = status_data.get("error", {})

                    self.job_repo.update_status(job_id, status)
                    if status == "succeeded":
                        await self.outbox.enqueue_and_send(FrameType.JOB_RESULT, {
                            "job_id": job_id,
                            "status": "succeeded",
                            "result": result_data,
                        })
                        if self.puller:
                            try:
                                await self.puller.pull_outputs(job_id)
                            except Exception:
                                logger.exception("pull_outputs failed for job %s", job_id)
                    else:
                        await self.outbox.enqueue_and_send(FrameType.JOB_RESULT, {
                            "job_id": job_id,
                            "status": status,
                            "error_msg": error_data.get("message", status),
                        })
                    return
            except Exception:
                pass
            await asyncio.sleep(2)

        self.job_repo.update_status(job_id, "failed")
        await self.outbox.enqueue_and_send(FrameType.JOB_RESULT, {
            "job_id": job_id,
            "status": "failed",
            "error_msg": "job timed out",
        })

    async def handle_job_cancel(self, payload: dict):
        job_id = payload.get("job_id", "")
        agent_id = payload.get("agent_id", "")

        if agent_id:
            client = self.docker.get_client(agent_id)
            if client:
                await client.cancel_job(job_id)

        self.job_repo.update_status(job_id, "cancelled")
        self.agent_repo.release(agent_id)
