"""Async HTTP client (httpx) to agent containers.
Calls /healthz, /status, /jobs (dispatch/cancel/query), /skills,
/vnc/start, /vnc/stop, /browser/state.
"""

import logging
import httpx

logger = logging.getLogger(__name__)


class AgentClient:
    def __init__(self, base_url: str, timeout: float = 30.0):
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout

    async def healthz(self) -> bool:
        try:
            async with httpx.AsyncClient(timeout=self.timeout) as c:
                r = await c.get(f"{self.base_url}/healthz")
                return r.status_code == 200
        except Exception:
            return False

    async def status(self) -> dict:
        async with httpx.AsyncClient(timeout=self.timeout) as c:
            r = await c.get(f"{self.base_url}/status")
            return r.json()

    async def create_job(self, job_id: str, command: str, params: dict | None = None, inputs_dir: str = "", skill_id: str = "", workspace_dir: str = "", callback_url: str = "") -> dict:
        payload = {
            "job_id": job_id,
            "command": command,
            "params": params or {},
            "inputs_dir": inputs_dir,
            "workspace_dir": workspace_dir or inputs_dir,
        }
        if skill_id:
            payload["skill_id"] = skill_id
        if callback_url:
            payload["callback_url"] = callback_url
        async with httpx.AsyncClient(timeout=self.timeout) as c:
            r = await c.post(f"{self.base_url}/jobs", json=payload)
            return r.json()

    async def cancel_job(self, job_id: str) -> dict:
        async with httpx.AsyncClient(timeout=self.timeout) as c:
            r = await c.post(f"{self.base_url}/jobs/{job_id}/cancel")
            return r.json()

    async def get_job(self, job_id: str) -> dict:
        async with httpx.AsyncClient(timeout=self.timeout) as c:
            r = await c.get(f"{self.base_url}/jobs/{job_id}")
            r.raise_for_status()
            return r.json()

    async def install_skill(self, skill_id: str, version: str, manifest: str, artifact_path: str) -> dict:
        async with httpx.AsyncClient(timeout=self.timeout) as c:
            r = await c.post(f"{self.base_url}/skills", json={
                "skill_id": skill_id,
                "version": version,
                "manifest": manifest,
                "artifact_path": artifact_path,
            })
            return r.json()

    async def enable_skill(self, skill_id: str) -> dict:
        async with httpx.AsyncClient(timeout=self.timeout) as c:
            r = await c.post(f"{self.base_url}/skills/{skill_id}/enable")
            return r.json()

    async def disable_skill(self, skill_id: str) -> dict:
        async with httpx.AsyncClient(timeout=self.timeout) as c:
            r = await c.post(f"{self.base_url}/skills/{skill_id}/disable")
            return r.json()

    async def delete_skill(self, skill_id: str) -> dict:
        async with httpx.AsyncClient(timeout=self.timeout) as c:
            r = await c.delete(f"{self.base_url}/skills/{skill_id}")
            return r.json()

    async def vnc_start(self) -> dict:
        async with httpx.AsyncClient(timeout=self.timeout) as c:
            r = await c.post(f"{self.base_url}/vnc/start")
            return r.json()

    async def vnc_stop(self) -> dict:
        async with httpx.AsyncClient(timeout=self.timeout) as c:
            r = await c.post(f"{self.base_url}/vnc/stop")
            return r.json()

    async def set_browser_state(self, storage_state: str) -> dict:
        async with httpx.AsyncClient(timeout=self.timeout) as c:
            r = await c.post(f"{self.base_url}/browser/state", json={"storage_state": storage_state})
            return r.json()

    async def get_browser_state(self, origin: str = "") -> dict:
        params = {"origin": origin} if origin else {}
        async with httpx.AsyncClient(timeout=self.timeout) as c:
            r = await c.get(f"{self.base_url}/browser/state", params=params)
            return r.json()

    async def get_job_events(self, job_id: str, since: int = 0) -> dict:
        async with httpx.AsyncClient(timeout=self.timeout) as c:
            r = await c.get(f"{self.base_url}/jobs/{job_id}/events", params={"since": since})
            return r.json()
