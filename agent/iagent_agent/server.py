"""IAgent agent container — FastAPI server exposing the fixed HTTP API.

Routes (per spec 04-agent-container §3):
  GET  /healthz           liveness/readiness
  GET  /status            current job, resource usage, skills
  POST /jobs              submit a job
  GET  /jobs/{id}         job status
  POST /jobs/{id}/cancel  cancel a job
  GET  /skills            list installed skills
  POST /skills            install/update a skill
  POST /skills/{id}/disable
  POST /skills/{id}/enable
  DELETE /skills/{id}
  GET  /vnc               VNC info
  POST /vnc/start         start VNC stack
  POST /vnc/stop          stop VNC stack
  POST /browser/state     inject login storage-state
  GET  /browser/state     export current storage-state
"""

from __future__ import annotations

import logging
import os
from contextlib import asynccontextmanager
from typing import Optional

import psutil
from fastapi import FastAPI, HTTPException, Query, Request, status as http_status

from iagent_agent.adapter.brain_stub import StubBrain
from iagent_agent.adapter.protocol import AgentBrain
from iagent_agent.browser.manager import BrowserManager, CloakBrowserManager, VNCStack
from iagent_agent.runtime.executor import JobExecutor
from iagent_agent.skills.loader import SkillManager
from iagent_agent.workspace import Workspace

logger = logging.getLogger(__name__)

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(name)s] %(levelname)s: %(message)s")


def _make_brain() -> AgentBrain:
    brain_key = os.getenv("IAGENT_BRAIN", "opencode")
    delay = float(os.getenv("IAGENT_STUB_DELAY", "0.1"))
    if brain_key == "opencode":
        import shutil
        if shutil.which("opencode") or os.getenv("IAGENT_BRAIN_PATH"):
            from iagent_agent.adapter.brain_opencode import OpenCodeBrain
            logger.info("using opencode brain adapter")
            return OpenCodeBrain()
    if brain_key == "stub":
        return StubBrain(delay=delay)
    logger.warning("brain adapter '%s' not available; falling back to stub", brain_key)
    return StubBrain(delay=delay)


def _get_state(request: Request):
    return request.app.state


def create_app() -> FastAPI:

    @asynccontextmanager
    async def lifespan(application: FastAPI):
        work_dir = os.getenv("IAGENT_WORK_DIR", "/work")
        skills_dir = os.path.join(work_dir, "skills")
        browser_cmd = os.getenv("IAGENT_BROWSER_CMD", "camoufox")
        display = os.getenv("IAGENT_VNC_DISPLAY", ":99")
        rfb_port = int(os.getenv("IAGENT_VNC_PORT", "5901"))
        profile_dir = os.getenv("IAGENT_BROWSER_PROFILE", os.path.join(work_dir, "profile"))

        state = application.state
        state.vnc_enabled = os.getenv("IAGENT_VNC_ENABLED", "true").lower() == "true"
        state.agent_id = os.getenv("IAGENT_AGENT_ID", "")
        state.workspace = Workspace(work_dir)
        state.skills = SkillManager(skills_dir)

        if browser_cmd == "cloakbrowser":
            state.browser = CloakBrowserManager(display=display, profile_dir=profile_dir)
            state.browser_type = "cloakbrowser"
        else:
            state.browser = BrowserManager(browser_cmd=browser_cmd, display=display, profile_dir=profile_dir)
            state.browser_type = "camoufox"

        state.vnc = VNCStack(display=display, rfb_port=rfb_port)
        state.executor = JobExecutor(
            brain=_make_brain(),
            workspace=state.workspace,
            skills=state.skills,
            browser_manager=state.browser,
            vnc_stack=state.vnc,
        )
        yield

    app = FastAPI(title="IAgent Agent Container", version="0.1.0", lifespan=lifespan)

    @app.get("/healthz")
    async def healthz(request: Request):
        state = request.app.state
        return {"status": "ok", "busy": state.executor.busy}

    @app.get("/status")
    async def agent_status(request: Request):
        state = request.app.state
        current_job = None
        job = state.executor.get_job_record()
        if job is not None:
            current_job = job.to_dict()

        usage = {
            "cpu_pct": round(psutil.cpu_percent(interval=0.1), 2),
            "mem_mb": round(psutil.Process().memory_info().rss / (1024 * 1024), 2),
            "disk_mb": round(psutil.disk_usage(str(state.workspace.root)).used / (1024 * 1024), 2),
        }

        return {
            "agent_id": state.agent_id,
            "current_job": current_job,
            "usage": usage,
            "skills": state.skills.list_skills(),
        }

    @app.post("/jobs", status_code=http_status.HTTP_202_ACCEPTED)
    async def submit_job(body: dict, request: Request):
        state = request.app.state
        if state.executor.busy:
            raise HTTPException(status_code=409, detail={"code": "BUSY"})

        job_id = body["job_id"]
        command = body["command"]
        params = body.get("params", {})
        skill_id = body.get("skill_id")
        callback_url = body.get("callback_url")
        workspace_dir = body.get("workspace_dir", body.get("inputs_dir", ""))

        if skill_id:
            sk = state.skills.get_enabled_skill(skill_id)
            if sk is None:
                raise HTTPException(status_code=422, detail={"code": "SKILL_NOT_ENABLED"})

        await state.executor.submit(
            job_id=job_id,
            command=command,
            params=params,
            callback_url=callback_url,
            skill_id=skill_id,
            vnc_enabled=state.vnc_enabled,
            workspace_dir=workspace_dir,
        )
        return {"job_id": job_id}

    @app.get("/jobs/{job_id}")
    async def get_job(job_id: str, request: Request):
        state = request.app.state
        current = state.executor.current_job_id()
        if current != job_id:
            raise HTTPException(status_code=404)
        record = state.executor.get_job_record()
        if record is None:
            raise HTTPException(status_code=404)
        return record.to_dict()

    @app.post("/jobs/{job_id}/cancel", status_code=http_status.HTTP_202_ACCEPTED)
    async def cancel_job(job_id: str, request: Request, body: Optional[dict] = None):
        state = request.app.state
        current = state.executor.current_job_id()
        if current != job_id:
            raise HTTPException(status_code=404)
        await state.executor.cancel()
        return {"job_id": job_id, "status": "cancelled"}

    @app.post("/jobs/{job_id}/events", status_code=http_status.HTTP_202_ACCEPTED)
    async def post_job_event(job_id: str, body: dict, request: Request):
        state = request.app.state
        current = state.executor.current_job_id()
        if current != job_id:
            raise HTTPException(status_code=404)
        seq = state.executor.post_event(body)
        return {"event_seq": seq}

    @app.get("/jobs/{job_id}/events")
    async def get_job_events(job_id: str, since: int = 0, request: Request = None):
        state = request.app.state
        current = state.executor.current_job_id()
        if current != job_id:
            return {"events": []}
        events = state.executor.get_events_since(since)
        return {"events": events}

    @app.get("/skills")
    async def get_skills(request: Request):
        return request.app.state.skills.list_skills()

    @app.post("/skills", status_code=http_status.HTTP_204_NO_CONTENT)
    async def install_skill(body: dict, request: Request):
        skills = request.app.state.skills
        skill_id = body["skill_id"]
        name = body.get("name", skill_id)
        version = body.get("version", "0.1.0")
        manifest = body.get("manifest", {})
        artifact_path = body.get("artifact_path")

        if skill_id in {s["skill_id"] for s in skills.list_skills()}:
            skills.update(skill_id, version, manifest, artifact_path)
        else:
            skills.install(skill_id, name, version, manifest, artifact_path)

    @app.post("/skills/{skill_id}/disable", status_code=http_status.HTTP_204_NO_CONTENT)
    async def disable_skill(skill_id: str, request: Request):
        skills = request.app.state.skills
        if skill_id not in {s["skill_id"] for s in skills.list_skills()}:
            raise HTTPException(status_code=404)
        skills.disable(skill_id)

    @app.post("/skills/{skill_id}/enable", status_code=http_status.HTTP_204_NO_CONTENT)
    async def enable_skill(skill_id: str, request: Request):
        skills = request.app.state.skills
        if skill_id not in {s["skill_id"] for s in skills.list_skills()}:
            raise HTTPException(status_code=404)
        skills.enable(skill_id)

    @app.delete("/skills/{skill_id}", status_code=http_status.HTTP_204_NO_CONTENT)
    async def delete_skill(skill_id: str, request: Request):
        skills = request.app.state.skills
        if skill_id not in {s["skill_id"] for s in skills.list_skills()}:
            raise HTTPException(status_code=404)
        skills.delete(skill_id)

    @app.get("/vnc")
    async def vnc_info(request: Request):
        state = request.app.state
        return {
            "enabled": state.vnc_enabled,
            "display": os.getenv("IAGENT_VNC_DISPLAY", ":99"),
            "rfb_host": "127.0.0.1",
            "rfb_port": state.vnc.rfb_port,
        }

    @app.post("/vnc/start", status_code=http_status.HTTP_202_ACCEPTED)
    async def vnc_start(request: Request):
        state = request.app.state
        if not state.vnc_enabled:
            raise HTTPException(status_code=400, detail={"code": "VNC_DISABLED"})
        if not state.executor.busy:
            raise HTTPException(status_code=409, detail={"code": "NO_ACTIVE_JOB"})

        rfb_password = state.vnc.start()
        if getattr(state, "browser_type", "") == "cloakbrowser":
            import asyncio
            await asyncio.to_thread(state.browser.launch_headless)
        else:
            state.browser.launch_headless()
        return {"rfb_port": state.vnc.rfb_port, "rfb_password": rfb_password}

    @app.post("/vnc/stop", status_code=http_status.HTTP_204_NO_CONTENT)
    async def vnc_stop(request: Request):
        state = request.app.state
        if getattr(state, "browser_type", "") == "cloakbrowser":
            state.browser.save_storage_state()
        state.browser.kill()
        state.vnc.stop()

    @app.post("/browser/state", status_code=http_status.HTTP_204_NO_CONTENT)
    async def inject_browser_state(body: dict, request: Request):
        state = request.app.state
        storage_state = body.get("storage_state", {})
        if storage_state:
            state.browser.set_profile_dir(state.workspace.profile)
            state.browser.inject_state(storage_state)
            state.executor.mark_credentials_injected()

    @app.get("/browser/state")
    async def export_browser_state(request: Request, origin: str = Query("")):
        state = request.app.state
        state.browser.set_profile_dir(state.workspace.profile)
        browser_state = state.browser.export_state(origin=origin)
        return {"storage_state": browser_state}

    return app


app = create_app()
