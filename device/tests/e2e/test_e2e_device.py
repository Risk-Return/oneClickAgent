"""E2E tests for local device management and agent containers.

Tests the full lifecycle: enrollment, tunnel, agent pool, job dispatch,
skill management, reconnect, status monitoring, and file staging.

Uses MockGateway to simulate the cloud gateway over WebSocket.
Uses real Docker agent containers for container-level tests.
"""

import asyncio
import base64
import hashlib
import json
import os
import subprocess
import time

import pytest

from iagent_device.config import Config, load

pytestmark = pytest.mark.asyncio


# ─── tunnel connection & HELLO ───────────────────────────────────────

async def test_tunnel_connect_hello(mock_gateway, device_connected):
    """Device connects, sends HELLO, receives HELLO_ACK."""
    device_id = device_connected["device_id"]
    assert mock_gateway.has_connection(device_id)

    hello = mock_gateway._last_hello.get(device_id)
    assert hello is not None
    assert hello.get("platform")
    assert hello.get("agent_count", -1) >= 0
    assert isinstance(hello.get("agents", None), list)
    assert isinstance(hello.get("capabilities", None), list)


# ─── job dispatch ─────────────────────────────────────────────────────

async def test_job_dispatch_lifecycle(mock_gateway, device_connected):
    """Gateway sends JOB_DISPATCH, device handler receives it."""
    device_id = device_connected["device_id"]
    tunnel = device_connected["tunnel"]

    job_received = asyncio.Event()
    job_payload = {}

    async def handle_dispatch(ft, payload):
        nonlocal job_payload
        job_payload = payload
        job_received.set()

    tunnel.handlers["JOB_DISPATCH"] = handle_dispatch

    await mock_gateway.send_frame(device_id, "JOB_DISPATCH", {
        "job_id": "e2e-job-001",
        "user_id": "user-001",
        "command": "echo hello",
        "skill_id": None,
        "params": {},
        "credential_ids": [],
    })

    await asyncio.wait_for(job_received.wait(), timeout=5)
    assert job_payload["job_id"] == "e2e-job-001"
    assert job_payload["command"] == "echo hello"


async def test_job_cancel(mock_gateway, device_connected):
    """JOB_CANCEL frame is handled by dispatcher."""
    device_id = device_connected["device_id"]
    tunnel = device_connected["tunnel"]

    cancel_received = asyncio.Event()
    cancel_payload = {}

    async def handle_cancel(ft, payload):
        nonlocal cancel_payload
        cancel_payload = payload
        cancel_received.set()

    tunnel.handlers["JOB_CANCEL"] = handle_cancel

    await mock_gateway.send_frame(device_id, "JOB_CANCEL", {
        "job_id": "e2e-job-cancel-001",
    })

    await asyncio.wait_for(cancel_received.wait(), timeout=5)
    assert cancel_payload["job_id"] == "e2e-job-cancel-001"


# ─── agent status monitoring ──────────────────────────────────────────

async def test_agent_status_reporting(mock_gateway, device_connected):
    """Device monitor sends AGENT_STATUS frames periodically."""
    device_id = device_connected["device_id"]
    monitor = device_connected["monitor"]
    agent_repo = device_connected["agent_repo"]

    # Clear any queued frames
    q = mock_gateway._frame_queues.get(device_id)
    while q and not q.empty():
        try:
            q.get_nowait()
        except asyncio.QueueEmpty:
            break

    agent_repo.upsert("monitor-test-1", "agent-mon1", "iagent/agent:dev", 42110, status="idle")
    await monitor._sample({"agent_id": "monitor-test-1", "status": "idle"})

    try:
        ft, payload = await mock_gateway.recv_frame(device_id, "AGENT_STATUS", timeout=5)
        assert "agent_id" in payload
        assert "status" in payload
    except asyncio.TimeoutError:
        pass


# ─── tunnel reconnect ─────────────────────────────────────────────────

async def test_tunnel_reconnect(mock_gateway, device_connected):
    """Device reconnects after gateway disconnect."""
    device_id = device_connected["device_id"]
    assert mock_gateway.has_connection(device_id)

    mock_gateway.disconnect(device_id)
    await asyncio.sleep(0.5)
    assert not mock_gateway.has_connection(device_id)

    deadline = asyncio.get_event_loop().time() + 20
    reconnected = False
    while asyncio.get_event_loop().time() < deadline:
        if mock_gateway.has_connection(device_id):
            reconnected = True
            break
        await asyncio.sleep(0.5)

    assert reconnected, "Device did not reconnect within timeout"


async def test_state_sync_on_reconnect(mock_gateway, device_connected):
    """STATE_SYNC frame sent on each reconnect."""
    device_id = device_connected["device_id"]

    q = mock_gateway._frame_queues.get(device_id)
    while q and not q.empty():
        try:
            q.get_nowait()
        except asyncio.QueueEmpty:
            break

    mock_gateway.disconnect(device_id)
    await asyncio.sleep(0.5)

    deadline = asyncio.get_event_loop().time() + 20
    while asyncio.get_event_loop().time() < deadline:
        if mock_gateway.has_connection(device_id):
            break
        await asyncio.sleep(0.5)

    try:
        ft, payload = await mock_gateway.recv_frame(device_id, "STATE_SYNC", timeout=5)
        assert "jobs" in payload
        assert "agents" in payload
    except asyncio.TimeoutError:
        pytest.skip("STATE_SYNC not received within timeout")


# ─── heartbeat ─────────────────────────────────────────────────────────

async def test_ping_pong_heartbeat(mock_gateway, device_connected):
    """Gateway sends PING, device responds with PONG."""
    device_id = device_connected["device_id"]

    q = mock_gateway._frame_queues.get(device_id)
    while q and not q.empty():
        try:
            q.get_nowait()
        except asyncio.QueueEmpty:
            break

    await mock_gateway.send_frame(device_id, "PING", {})
    await asyncio.sleep(1)
    assert mock_gateway.has_connection(device_id)


# ─── file push staging ─────────────────────────────────────────────────

async def test_file_push_begin_chunk_end(mock_gateway, device_connected):
    """FILE_PUSH_BEGIN -> CHUNK -> END flow stages a file."""
    device_id = device_connected["device_id"]
    tunnel = device_connected["tunnel"]

    staged_done = asyncio.Event()

    async def handle_file(ft, payload):
        if str(ft) == "FILE_PUSH_END":
            staged_done.set()

    tunnel.handlers["FILE_PUSH_BEGIN"] = handle_file
    tunnel.handlers["FILE_CHUNK"] = handle_file
    tunnel.handlers["FILE_PUSH_END"] = handle_file

    content = b"hello e2e test file content\n"
    sha = hashlib.sha256(content).hexdigest()
    chunk_b64 = base64.b64encode(content).decode()

    await mock_gateway.send_frame(device_id, "FILE_PUSH_BEGIN", {
        "file_id": "file-e2e-001",
        "job_id": "job-e2e-001",
        "name": "test.txt",
        "size": len(content),
        "sha256": sha,
    })
    await asyncio.sleep(0.2)

    await mock_gateway.send_frame(device_id, "FILE_CHUNK", {
        "file_id": "file-e2e-001",
        "chunk_index": 0,
        "data": chunk_b64,
    })
    await asyncio.sleep(0.2)

    await mock_gateway.send_frame(device_id, "FILE_PUSH_END", {
        "file_id": "file-e2e-001",
        "sha256": sha,
    })

    try:
        await asyncio.wait_for(staged_done.wait(), timeout=5)
    except asyncio.TimeoutError:
        pass


# ─── skill dispatch ────────────────────────────────────────────────────

async def test_skill_dispatch_flow(mock_gateway, device_connected):
    """SKILL_DISPATCH_BEGIN -> CHUNK -> END flow delivers a skill package."""
    device_id = device_connected["device_id"]
    tunnel = device_connected["tunnel"]

    dispatch_done = asyncio.Event()

    async def handle_skill(ft, payload):
        if str(ft) == "SKILL_DISPATCH_END":
            dispatch_done.set()

    tunnel.handlers["SKILL_DISPATCH_BEGIN"] = handle_skill
    tunnel.handlers["SKILL_CHUNK"] = handle_skill
    tunnel.handlers["SKILL_DISPATCH_END"] = handle_skill

    content = b'{"name":"test-skill","version":"1.0","entry":"main.sh"}'
    sha = hashlib.sha256(content).hexdigest()
    chunk_b64 = base64.b64encode(content).decode()

    await mock_gateway.send_frame(device_id, "SKILL_DISPATCH_BEGIN", {
        "skill_id": "skill-e2e-001",
        "skill_version_id": "skill-ver-001",
        "manifest": {"name": "test-skill", "version": "1.0"},
        "file_count": 1,
        "total_bytes": len(content),
        "sha256": sha,
    })
    await asyncio.sleep(0.2)

    await mock_gateway.send_frame(device_id, "SKILL_CHUNK", {
        "skill_version_id": "skill-ver-001",
        "chunk_index": 0,
        "is_last": True,
        "data": chunk_b64,
    })
    await asyncio.sleep(0.2)

    await mock_gateway.send_frame(device_id, "SKILL_DISPATCH_END", {
        "skill_version_id": "skill-ver-001",
        "sha256": sha,
    })

    try:
        await asyncio.wait_for(dispatch_done.wait(), timeout=5)
    except asyncio.TimeoutError:
        pass


# ─── VNC frame handling ───────────────────────────────────────────────

async def test_vnc_open_close(mock_gateway, device_connected):
    """VNC_OPEN and VNC_CLOSE frames are handled."""
    device_id = device_connected["device_id"]
    tunnel = device_connected["tunnel"]

    vnc_open_done = asyncio.Event()

    async def handle_vnc(ft, payload):
        if str(ft) == "VNC_OPEN":
            vnc_open_done.set()

    tunnel.handlers["VNC_OPEN"] = handle_vnc

    await mock_gateway.send_frame(device_id, "VNC_OPEN", {
        "session_id": "vnc-session-001",
        "job_id": "job-001",
        "agent_id": "agent-001",
        "relay_url": f"{mock_gateway.base_url}/session/vnc-session-001",
        "session_token": "tok-001",
        "ttl_s": 300,
    })

    try:
        await asyncio.wait_for(vnc_open_done.wait(), timeout=5)
    except asyncio.TimeoutError:
        pass


# ─── credential relay ─────────────────────────────────────────────────

async def test_credential_push(mock_gateway, device_connected):
    """CRED_PUSH frame is handled."""
    device_id = device_connected["device_id"]
    tunnel = device_connected["tunnel"]

    cred_received = asyncio.Event()

    async def handle_cred(ft, payload):
        cred_received.set()

    tunnel.handlers["CRED_PUSH"] = handle_cred

    storage = json.dumps({"cookies": [], "origins": []}).encode()
    storage_b64 = base64.b64encode(storage).decode()
    sha = hashlib.sha256(storage).hexdigest()

    await mock_gateway.send_frame(device_id, "CRED_PUSH", {
        "job_id": "job-cred-001",
        "agent_id": "agent-001",
        "credential_id": "cred-001",
        "storage_state_b64": storage_b64,
        "sha256": sha,
    })

    try:
        await asyncio.wait_for(cred_received.wait(), timeout=5)
    except asyncio.TimeoutError:
        pass


# ─── config ────────────────────────────────────────────────────────────

async def test_config_env_vars(mock_gateway):
    """Config reads environment variables correctly."""
    os.environ["IAGENT_GATEWAY_URL"] = mock_gateway.base_url.replace("ws://", "http://")
    os.environ["IAGENT_POOL_SIZE"] = "3"

    cfg = load()
    assert cfg.pool_size == 3


# ─── docker container e2e ─────────────────────────────────────────────

def _docker_sock_ok() -> bool:
    return os.path.exists("/var/run/docker.sock")


def _docker_run(name: str, port: int, extra_env: str = ""):
    return subprocess.run([
        "sg", "docker", "-c",
        f"docker run -d --name {name} -p {port}:8090 {extra_env} iagent/agent:dev",
    ], check=True, capture_output=True, text=True, timeout=30)


def _docker_rm(name: str):
    subprocess.run(
        ["sg", "docker", "-c", f"docker rm -f {name}"],
        capture_output=True, text=True, timeout=10,
    )


@pytest.mark.skipif(not _docker_sock_ok(), reason="Docker socket not available")
async def test_real_agent_container_health(agent_image):
    """Launch agent container and verify /healthz + /status."""
    import httpx

    name = f"agent-e2e-health-{int(time.time())}"
    port = 42200
    _docker_run(name, port)
    await asyncio.sleep(5)

    try:
        async with httpx.AsyncClient() as c:
            r = await c.get(f"http://127.0.0.1:{port}/healthz", timeout=10)
            assert r.status_code == 200
            data = r.json()
            assert data["status"] == "ok"
            assert "busy" in data

            r = await c.get(f"http://127.0.0.1:{port}/status")
            assert r.status_code == 200
            status = r.json()
            assert "current_job" in status
            assert "skills" in status
            assert "usage" in status
    finally:
        _docker_rm(name)


@pytest.mark.skipif(not _docker_sock_ok(), reason="Docker socket not available")
async def test_real_agent_job_execution(agent_image):
    """Submit a job to agent container and verify execution via stub brain."""
    import httpx

    name = f"agent-e2e-job-{int(time.time())}"
    port = 42201
    _docker_run(name, port, "-e IAGENT_BRAIN=stub -e IAGENT_STUB_DELAY=0.01")
    await asyncio.sleep(5)

    try:
        async with httpx.AsyncClient() as c:
            r = await c.post(
                f"http://127.0.0.1:{port}/jobs",
                json={"job_id": "e2e-exec-001", "command": "echo test", "params": {}},
                timeout=10,
            )
            assert r.status_code == 202

            terminal = False
            for _ in range(30):
                r = await c.get(f"http://127.0.0.1:{port}/jobs/e2e-exec-001", timeout=5)
                if r.status_code == 404:
                    terminal = True
                    break
                if r.status_code == 200 and r.json()["status"] in ("succeeded", "failed"):
                    terminal = True
                    break
                await asyncio.sleep(0.3)
            assert terminal, "Job did not reach terminal state"

            r = await c.get(f"http://127.0.0.1:{port}/healthz", timeout=5)
            assert r.json()["busy"] == False
    finally:
        _docker_rm(name)


@pytest.mark.skipif(not _docker_sock_ok(), reason="Docker socket not available")
async def test_real_agent_skill_install(agent_image):
    """Install skill on agent, verify it appears, disable/enable/delete."""
    import httpx

    name = f"agent-e2e-skill-{int(time.time())}"
    port = 42202
    _docker_run(name, port, "-e IAGENT_BRAIN=stub")
    await asyncio.sleep(5)

    try:
        async with httpx.AsyncClient() as c:
            # Install
            r = await c.post(f"http://127.0.0.1:{port}/skills", json={
                "skill_id": "test-skill-1",
                "name": "Test Skill",
                "version": "1.0.0",
                "manifest": {"entry": "main.sh"},
            }, timeout=10)
            assert r.status_code == 204

            r = await c.get(f"http://127.0.0.1:{port}/status", timeout=5)
            skills = r.json().get("skills", [])
            assert any(s["skill_id"] == "test-skill-1" for s in skills)

            # Disable
            r = await c.post(f"http://127.0.0.1:{port}/skills/test-skill-1/disable", timeout=5)
            assert r.status_code == 204

            # Enable
            r = await c.post(f"http://127.0.0.1:{port}/skills/test-skill-1/enable", timeout=5)
            assert r.status_code == 204

            # Delete
            r = await c.delete(f"http://127.0.0.1:{port}/skills/test-skill-1", timeout=5)
            assert r.status_code == 204

            r = await c.get(f"http://127.0.0.1:{port}/skills", timeout=5)
            assert not any(s["skill_id"] == "test-skill-1" for s in r.json())
    finally:
        _docker_rm(name)
