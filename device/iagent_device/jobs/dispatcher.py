"""Job dispatcher: receives JOB_DISPATCH from tunnel for a pre-allocated agent,
dispatches to that agent container, relays progress/results back through outbox,
handles cancellation. Signals pool reaper on terminal to release agent back to IDLE.
"""

import asyncio
import logging
import time

from iagent_device.tunnel.codec import FrameType
from iagent_device.tunnel.outbox import Outbox
from iagent_device.store.repositories import JobRepo, AgentRepo
from iagent_device.docker.manager import DockerManager

logger = logging.getLogger(__name__)

PULL_OUTPUTS_TIMEOUT = 60.0


def _error_message(error, fallback: str) -> str:
    """Normalize the agent's error field (str or {code, message} dict) to a message string."""
    if isinstance(error, dict):
        return error.get("message") or error.get("code") or fallback
    if isinstance(error, str) and error:
        return error
    return fallback


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
        cred_inject_timeout: float = 30.0,
    ):
        self.job_repo = job_repo
        self.agent_repo = agent_repo
        self.docker = docker_mgr
        self.outbox = outbox
        self.stager = stager
        self.puller = puller
        self.cred_relay = cred_relay
        self.cred_inject_timeout = cred_inject_timeout

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
        credential_ids = payload.get("credential_ids", []) or []

        self.job_repo.create(job_id, agent_id, user_id, command, skill_id, ",".join(credential_ids))
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
            await self._wait_for_files(job_id)

            if credential_ids and self.cred_relay is not None:
                await self.cred_relay.wait_for_injections(job_id, credential_ids, self.cred_inject_timeout)

            await client.create_job(job_id, command, {}, skill_id=skill_id, workspace_dir=f"/work/workspaces/{job_id}")
            self.job_repo.update_status(job_id, "running")
            await self.outbox.enqueue_and_send(FrameType.JOB_PROGRESS, {
                "job_id": job_id,
                "status": "running",
                "percent": 0,
                "message": "Job started",
                "event_seq": 0,
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

    async def _poll_progress(self, client, job_id: str, timeout_s: int = 3600):
        """Poll agent GET /jobs/{job_id} until terminal status or timeout."""
        last_percent = 0
        event_seq = 1
        last_event_seq = -1
        deadline = time.monotonic() + timeout_s
        consecutive_failures = 0

        while time.monotonic() < deadline:
            try:
                status_data = await client.get_job(job_id)
                status = status_data.get("status", "running")
                percent = status_data.get("percent", 0)
                message = status_data.get("message", "")
                agent_event_seq = status_data.get("event_seq", 0)

                if agent_event_seq > 0:
                    event_seq = agent_event_seq

                if percent != last_percent or status != "running":
                    await self.outbox.enqueue_and_send(FrameType.JOB_PROGRESS, {
                        "job_id": job_id,
                        "status": status,
                        "percent": percent,
                        "event_seq": event_seq,
                        "message": message,
                    })
                    last_percent = percent

                # Poll agent events for login_required and browser_ready
                try:
                    events_data = await client.get_job_events(job_id, since=last_event_seq + 1)
                    for evt in events_data.get("events", []):
                        evt_seq = evt.get("event_seq", -1)
                        if evt_seq > last_event_seq:
                            last_event_seq = evt_seq
                        evt_type = evt.get("type", "")
                        if evt_type == "login_required":
                            await self.outbox.enqueue_and_send(FrameType.JOB_LOGIN_REQUIRED, {
                                "job_id": job_id,
                                "event_seq": evt.get("event_seq", 0),
                                "origin": evt.get("origin", ""),
                                "label": evt.get("label", ""),
                                "login_kind": evt.get("login_kind", "unknown"),
                            })
                        elif evt_type == "browser_ready":
                            await self.outbox.enqueue_and_send(FrameType.JOB_LOGIN_REQUIRED, {
                                "job_id": job_id,
                                "event_seq": evt.get("event_seq", 0),
                                "origin": status_data.get("message", ""),
                                "login_kind": "browser_ready",
                            })
                            try:
                                await client.vnc_start()
                                logger.info("pre-started VNC on browser_ready for job %s", job_id)
                            except Exception:
                                logger.debug("VNC pre-start skipped for job %s (may already be running)", job_id)
                        elif evt_type == "browser_error":
                            await self.outbox.enqueue_and_send(FrameType.JOB_PROGRESS, {
                                "job_id": job_id,
                                "status": "running",
                                "percent": percent,
                                "event_seq": evt.get("event_seq", event_seq),
                                "message": "Browser failed to launch",
                            })
                except Exception:
                    logger.debug("failed to poll agent events for job %s", job_id)

                if status in ("succeeded", "failed", "cancelled"):
                    result_data = status_data.get("result", {})

                    self.job_repo.update_status(job_id, status)

                    # Pull output files for all terminal statuses, with a wallclock cap
                    # so a stuck transfer cannot indefinitely delay JOB_RESULT.
                    if self.puller:
                        try:
                            await asyncio.wait_for(
                                self.puller.pull_outputs(job_id),
                                timeout=PULL_OUTPUTS_TIMEOUT,
                            )
                        except asyncio.TimeoutError:
                            logger.warning(
                                "pull_outputs exceeded %.0fs for job %s; sending JOB_RESULT anyway",
                                PULL_OUTPUTS_TIMEOUT, job_id,
                            )
                        except Exception:
                            logger.exception("pull_outputs failed for job %s", job_id)

                    if status == "succeeded":
                        await self.outbox.enqueue_and_send(FrameType.JOB_RESULT, {
                            "job_id": job_id,
                            "status": "succeeded",
                            "result": result_data,
                        })
                    else:
                        await self.outbox.enqueue_and_send(FrameType.JOB_RESULT, {
                            "job_id": job_id,
                            "status": status,
                            "error_msg": _error_message(status_data.get("error"), status),
                        })
                    return
                consecutive_failures = 0
            except Exception:
                consecutive_failures += 1
                if consecutive_failures >= 5:
                    logger.error("job %s poll failed %d times, giving up", job_id, consecutive_failures)
                    self.job_repo.update_status(job_id, "failed")
                    await self.outbox.enqueue_and_send(FrameType.JOB_RESULT, {
                        "job_id": job_id,
                        "status": "failed",
                        "error_msg": "agent unreachable or job not found",
                    })
                    return
                logger.warning("poll error for job %s (attempt %d)", job_id, consecutive_failures, exc_info=True)
            await asyncio.sleep(2)

        self.job_repo.update_status(job_id, "failed")
        logger.error("job %s timed out after %ds", job_id, timeout_s)
        await self.outbox.enqueue_and_send(FrameType.JOB_RESULT, {
            "job_id": job_id,
            "status": "failed",
            "error_msg": f"job timed out after {timeout_s}s",
        })
    async def handle_job_cancel(self, payload: dict):
        job_id = payload.get("job_id", "")
        job = self.job_repo.get_by_id(job_id)
        agent_id = job["agent_id"] if job else ""

        if agent_id:
            client = self.docker.get_client(agent_id)
            if client:
                try:
                    await client.cancel_job(job_id)
                except Exception:
                    logger.exception("agent cancel failed for job %s", job_id)

        self.job_repo.update_status(job_id, "cancelled")
        if agent_id:
            self.agent_repo.release(agent_id)
