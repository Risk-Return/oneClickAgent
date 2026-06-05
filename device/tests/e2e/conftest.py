"""E2E test fixtures: mock gateway, device lifecycle, Docker containers."""

import asyncio
import json
import logging
import os
import shutil
import signal
import sqlite3
import subprocess
import sys
import tempfile
import time
import uuid
from pathlib import Path

import pytest

from iagent_device.config import Config
from iagent_device.docker.manager import DockerManager
from iagent_device.monitor.monitor import Monitor
from iagent_device.store.connection import connect as db_connect, migrate
from iagent_device.store.repositories import (
    AgentRepo, OutboxRepo,
)
from iagent_device.tunnel.client import TunnelClient
from iagent_device.tunnel.outbox import Outbox

from .mock_gateway import MockGateway

logger = logging.getLogger(__name__)

PROJECT_ROOT = Path(__file__).resolve().parent.parent.parent.parent


def _docker_available() -> bool:
    try:
        result = subprocess.run(
            ["sg", "docker", "-c", "docker info --format '{{.ServerVersion}}'"],
            capture_output=True, text=True, timeout=5,
        )
        return result.returncode == 0
    except Exception:
        return False


DOCKER_AVAILABLE = _docker_available()


@pytest.fixture
async def mock_gateway():
    gw = MockGateway()
    await gw.start()
    yield gw
    await gw.stop()


@pytest.fixture
def temp_data_dir():
    d = tempfile.mkdtemp(prefix="iagent-e2e-")
    yield Path(d)
    shutil.rmtree(d, ignore_errors=True)


@pytest.fixture
def device_config(temp_data_dir, mock_gateway):
    cfg = Config()
    # The device converts http->ws, so we set the WS base URL directly
    cfg.gateway_url = mock_gateway.base_url + "ws"
    cfg.device_data_dir = temp_data_dir
    cfg.device_data_dir.mkdir(parents=True, exist_ok=True)
    (cfg.device_data_dir / "workspaces").mkdir(parents=True, exist_ok=True)
    (cfg.device_data_dir / "skills").mkdir(parents=True, exist_ok=True)
    cfg.pool_size = 0
    cfg.prepull_image = False
    cfg.agent_image = "iagent/agent:dev"
    return cfg


@pytest.fixture
def device_db(device_config):
    db_path = device_config.db_path
    conn = db_connect(db_path)
    migrate(conn)
    yield conn
    conn.close()


@pytest.fixture
def device_enrolled(device_db, device_config, mock_gateway):
    """Simulate an enrolled device by inserting directly into SQLite."""
    device_id = str(uuid.uuid4())
    token = f"{device_id}-{uuid.uuid4()}"

    # Register token with mock gateway
    mock_gateway.register_device(device_id, token)

    device_db.execute(
        "INSERT OR REPLACE INTO device_info (device_id, token, gateway_url, enrolled_at) VALUES (?, ?, ?, ?)",
        (device_id, token, "ws://" + mock_gateway.base_url.replace("ws://", ""),
         time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())),
    )
    device_db.commit()

    return {
        "device_id": device_id,
        "token": token,
        "gateway_url": mock_gateway.base_url,
        "data_dir": device_config.device_data_dir,
    }


@pytest.fixture
async def device_connected(mock_gateway, device_enrolled, device_db, device_config):
    """Start the device tunnel client and connect to mock gateway."""
    device_id = device_enrolled["device_id"]
    token = device_enrolled["token"]
    data_dir = device_config.device_data_dir
    gw_url = "ws://" + mock_gateway.base_url.replace("ws://", "")

    agent_repo = AgentRepo(device_db)
    outbox_repo = OutboxRepo(device_db)

    docker_mgr = DockerManager(
        agent_repo=agent_repo,
        image=device_config.agent_image,
        data_dir=str(data_dir),
        max_restarts=3,
        port_start=42100,
        port_end=42109,
    )

    outbox = Outbox(outbox_repo, lambda ft, p: None)

    monitor = Monitor(agent_repo=agent_repo, outbox=outbox, docker_mgr=docker_mgr)
    hello_extras = monitor.build_hello_extras(agent_repo, vnc_enabled=False)

    tunnel = TunnelClient(
        gateway_url=gw_url,
        device_id=device_id,
        device_token=token,
        heartbeat_s=15,
        handlers={},
        outbox=outbox,
        hello_extras=hello_extras,
    )

    tunnel_task = asyncio.create_task(tunnel.run())
    outbox.send_fn = tunnel._send

    deadline = asyncio.get_event_loop().time() + 10
    while asyncio.get_event_loop().time() < deadline:
        if mock_gateway.has_connection(device_id):
            break
        await asyncio.sleep(0.1)
    else:
        tunnel_task.cancel()
        pytest.fail("Device did not connect to mock gateway")

    yield {
        "device_id": device_id,
        "token": token,
        "tunnel": tunnel,
        "tunnel_task": tunnel_task,
        "docker_mgr": docker_mgr,
        "monitor": monitor,
        "data_dir": data_dir,
        "agent_repo": agent_repo,
        "outbox": outbox,
        "gateway": mock_gateway,
    }

    tunnel_task.cancel()
    try:
        await tunnel_task
    except asyncio.CancelledError:
        pass


@pytest.fixture
def docker_required():
    if not DOCKER_AVAILABLE:
        pytest.skip("Docker not available")
    return True


@pytest.fixture
def agent_image():
    result = subprocess.run(
        ["sg", "docker", "-c", "docker images -q iagent/agent:dev"],
        capture_output=True, text=True, timeout=5,
    )
    if not result.stdout.strip():
        pytest.skip("iagent/agent:dev image not built")
    return "iagent/agent:dev"
