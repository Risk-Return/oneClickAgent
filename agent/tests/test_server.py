import asyncio
import json
import os
import tempfile
from pathlib import Path

import pytest
import pytest_asyncio
from httpx import ASGITransport, AsyncClient

from iagent_agent.server import create_app


@pytest.fixture
def temp_work_dir():
    with tempfile.TemporaryDirectory() as tmp:
        yield tmp


class LifespanAsyncClient:
    def __init__(self, app, base_url="http://test"):
        self._app = app
        self._transport = ASGITransport(app=app)
        self._client: AsyncClient | None = None
        self._base_url = base_url
        self._startup_event = asyncio.Event()
        self._shutdown_event = asyncio.Event()
        self._lifespan_task: asyncio.Task | None = None

    async def __aenter__(self):
        self._client = AsyncClient(transport=self._transport, base_url=self._base_url)
        async def _run_lifespan():
            from starlette.types import Message
            state = {"startup_complete": False}

            async def receive() -> Message:
                if not state["startup_complete"]:
                    state["startup_complete"] = True
                    return {"type": "lifespan.startup"}
                return {"type": "lifespan.shutdown"}

            async def send(message: Message):
                if message["type"] == "lifespan.startup.complete":
                    self._startup_event.set()
                elif message["type"] == "lifespan.shutdown.complete":
                    self._shutdown_event.set()

            await self._app({"type": "lifespan", "asgi": {"version": "3.0"}}, receive, send)

        self._lifespan_task = asyncio.create_task(_run_lifespan())
        await self._startup_event.wait()
        return self._client

    async def __aexit__(self, *args):
        if self._lifespan_task:
            self._shutdown_event.set()
            self._lifespan_task.cancel()
            try:
                await self._lifespan_task
            except asyncio.CancelledError:
                pass
            self._lifespan_task = None
        if self._client:
            await self._client.aclose()
            self._client = None


@pytest_asyncio.fixture
async def client(temp_work_dir):
    os.environ["IAGENT_WORK_DIR"] = temp_work_dir
    os.environ["IAGENT_BRAIN"] = "stub"
    os.environ["IAGENT_VNC_ENABLED"] = "true"
    os.environ["IAGENT_STUB_DELAY"] = "0.01"

    app = create_app()
    async with LifespanAsyncClient(app) as ac:
        yield ac


@pytest.mark.asyncio
async def test_healthz(client):
    resp = await client.get("/healthz")
    assert resp.status_code == 200
    data = resp.json()
    assert data["status"] == "ok"
    assert data["busy"] is False


@pytest.mark.asyncio
async def test_status(client):
    resp = await client.get("/status")
    assert resp.status_code == 200
    data = resp.json()
    assert data["current_job"] is None
    assert "cpu_pct" in data["usage"]
    assert "mem_mb" in data["usage"]
    assert "disk_mb" in data["usage"]
    assert isinstance(data["skills"], list)


@pytest.mark.asyncio
async def test_submit_job_and_poll(client):
    resp = await client.post("/jobs", json={
        "job_id": "job-1",
        "command": "test command",
    })
    assert resp.status_code == 202
    data = resp.json()
    assert data["job_id"] == "job-1"

    for _ in range(10):
        await asyncio.sleep(0.05)
        resp = await client.get("/jobs/job-1")
        if resp.status_code == 200:
            job = resp.json()
            assert job["job_id"] == "job-1"
            if job["status"] == "SUCCEEDED":
                assert job["percent"] == 100
                assert "result" in job
                break
            elif job["status"] == "RUNNING":
                continue
    else:
        resp = await client.get("/jobs/job-1")
        if resp.status_code == 404:
            return
        raise AssertionError(f"job stuck in state: {resp.json()}")


@pytest.mark.asyncio
async def test_busy_409(client):
    await client.post("/jobs", json={
        "job_id": "job-busy",
        "command": "first job",
    })
    resp = await client.post("/jobs", json={
        "job_id": "job-busy-2",
        "command": "second job",
    })
    assert resp.status_code == 409
    assert resp.json()["detail"]["code"] == "BUSY"


@pytest.mark.asyncio
async def test_job_not_found(client):
    resp = await client.get("/jobs/nonexistent")
    assert resp.status_code == 404


@pytest.mark.asyncio
async def test_cancel_job(client):
    old_delay = os.environ["IAGENT_STUB_DELAY"]
    os.environ["IAGENT_STUB_DELAY"] = "2.0"
    app2 = create_app()
    async with LifespanAsyncClient(app2) as c2:
        await c2.post("/jobs", json={
            "job_id": "job-cancel",
            "command": "slow job",
        })
        resp = await c2.post("/jobs/job-cancel/cancel")
        assert resp.status_code == 202
        await asyncio.sleep(0.1)

    os.environ["IAGENT_STUB_DELAY"] = old_delay


@pytest.mark.asyncio
async def test_skills_crud(client):
    resp = await client.post("/skills", json={
        "skill_id": "pdf-extract",
        "name": "PDF Extract",
        "version": "1.0.0",
        "manifest": {"entrypoint": "extract.py"},
    })
    assert resp.status_code == 204

    resp = await client.get("/skills")
    skills = resp.json()
    assert len(skills) == 1
    assert skills[0]["skill_id"] == "pdf-extract"
    assert skills[0]["status"] == "enabled"

    resp = await client.post("/skills/pdf-extract/disable")
    assert resp.status_code == 204

    resp = await client.get("/skills")
    assert resp.json()[0]["status"] == "disabled"

    resp = await client.post("/skills/pdf-extract/enable")
    assert resp.status_code == 204

    resp = await client.get("/skills")
    assert resp.json()[0]["status"] == "enabled"

    resp = await client.delete("/skills/pdf-extract")
    assert resp.status_code == 204

    resp = await client.get("/skills")
    assert len(resp.json()) == 0


@pytest.mark.asyncio
async def test_skill_not_enabled_for_job(client):
    resp = await client.post("/skills", json={
        "skill_id": "test-skill",
        "name": "Test Skill",
        "version": "1.0.0",
        "manifest": {},
    })
    assert resp.status_code == 204

    await client.post("/skills/test-skill/disable")

    resp = await client.post("/jobs", json={
        "job_id": "job-skill",
        "command": "test",
        "skill_id": "test-skill",
    })
    assert resp.status_code == 422
    assert "SKILL_NOT_ENABLED" in resp.json()["detail"]["code"]


@pytest.mark.asyncio
async def test_skill_update_idempotent(client):
    await client.post("/skills", json={
        "skill_id": "skill-update",
        "name": "Original",
        "version": "1.0.0",
        "manifest": {"key": "old"},
    })
    resp = await client.post("/skills", json={
        "skill_id": "skill-update",
        "name": "Updated",
        "version": "2.0.0",
        "manifest": {"key": "new"},
    })
    assert resp.status_code == 204

    resp = await client.get("/skills")
    skill = resp.json()[0]
    assert skill["version"] == "2.0.0"
    assert skill["manifest"] == {"key": "new"}


@pytest.mark.asyncio
async def test_vnc_info(client):
    resp = await client.get("/vnc")
    assert resp.status_code == 200
    data = resp.json()
    assert data["enabled"] is True
    assert data["rfb_host"] == "127.0.0.1"
    assert isinstance(data["rfb_port"], int)


@pytest.mark.asyncio
async def test_vnc_start_no_active_job(client):
    resp = await client.post("/vnc/start")
    assert resp.status_code == 409


@pytest.mark.asyncio
async def test_browser_state_inject_export(client):
    state = {"cookies": [{"name": "session", "value": "abc123"}], "origins": []}
    resp = await client.post("/browser/state", json={"storage_state": state})
    assert resp.status_code == 204

    resp = await client.get("/browser/state")
    assert resp.status_code == 200
    data = resp.json()
    assert data["storage_state"] == state


@pytest.mark.asyncio
async def test_workspace_wipe_after_success(client):
    work_dir = Path(os.environ["IAGENT_WORK_DIR"])
    profile = work_dir / "profile" / "storage_state.json"
    profile.parent.mkdir(parents=True, exist_ok=True)
    profile.write_text(json.dumps({"secret": "test"}))

    resp = await client.post("/jobs", json={
        "job_id": "job-wipe",
        "command": "wipe test",
    })
    assert resp.status_code == 202

    await asyncio.sleep(0.5)

    for sub in ("inputs", "scratch", "output", "profile"):
        d = work_dir / sub
        contents = list(d.iterdir()) if d.exists() else []
        assert len(contents) == 0, f"{sub} not wiped: {contents}"


@pytest.mark.asyncio
async def test_workspace_wipe_after_cancel(client):
    with tempfile.TemporaryDirectory() as tmp2:
        old_dir = os.environ["IAGENT_WORK_DIR"]
        old_delay = os.environ["IAGENT_STUB_DELAY"]
        os.environ["IAGENT_WORK_DIR"] = tmp2
        os.environ["IAGENT_STUB_DELAY"] = "5.0"

        app2 = create_app()
        async with LifespanAsyncClient(app2) as c2:
            work_dir = Path(tmp2)
            profile_file = work_dir / "profile" / "data.txt"
            profile_file.parent.mkdir(parents=True, exist_ok=True)
            profile_file.write_text("sensitive")

            await c2.post("/jobs", json={
                "job_id": "job-wipe-cancel",
                "command": "slow job",
            })

            await c2.post("/jobs/job-wipe-cancel/cancel")
            await asyncio.sleep(0.5)

            for sub in ("inputs", "scratch", "output", "profile"):
                d = work_dir / sub
                contents = list(d.iterdir()) if d.exists() else []
                assert len(contents) == 0, f"{sub} not wiped after cancel: {contents}"

        os.environ["IAGENT_WORK_DIR"] = old_dir
        os.environ["IAGENT_STUB_DELAY"] = old_delay


@pytest.mark.asyncio
async def test_healthz_reflects_busy(client):
    resp = await client.post("/jobs", json={
        "job_id": "job-busy-hz",
        "command": "busy test",
    })
    assert resp.status_code == 202

    resp = await client.get("/healthz")
    assert resp.json()["busy"] is True

    await asyncio.sleep(0.5)

    resp = await client.get("/healthz")
    assert resp.json()["busy"] is False
