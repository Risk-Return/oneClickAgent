"""CLI entrypoint: iagent-device {enroll, run, status, agents, logs}.
- enroll: one-time setup with gateway enrolment code
- run: main daemon — reconcile Docker, open tunnel, serve frames
- status / agents / logs: local diagnostics
"""

import asyncio
import logging
import sys
import json

import httpx

from iagent_device.config import load as load_config
from iagent_device.store.connection import connect, migrate
from iagent_device.store.repositories import (
    DeviceRepo, AgentRepo, JobRepo, OutboxRepo, FileRepo, SkillRepo, VNCSessionRepo,
)
from iagent_device.tunnel.client import TunnelClient
from iagent_device.tunnel.outbox import Outbox
from iagent_device.tunnel.codec import FrameType
from iagent_device.docker.manager import DockerManager
from iagent_device.docker.reconcile import reconcile
from iagent_device.jobs.dispatcher import JobDispatcher
from iagent_device.files.stager import FileStager
from iagent_device.skills.manager import SkillManager
from iagent_device.vncbridge.bridge import VNCBridge
from iagent_device.creds.relay import CredRelay
from iagent_device.monitor.monitor import Monitor

logger = logging.getLogger("iagent.device")


async def cmd_enroll(cfg, args):
    """POST /devices/enroll to exchange enrollment_code for device_id + device_token."""
    gateway = cfg.gateway_url.rstrip("/")
    code = args[0] if args else ""

    if not code:
        print("Usage: iagent-device enroll --code CODE")
        sys.exit(1)

    async with httpx.AsyncClient() as c:
        r = await c.post(f"{gateway}/api/v1/devices/enroll", json={"enrollment_code": code})
        r.raise_for_status()
        data = r.json()
        device_id = data["device_id"]
        device_token = data["device_token"]

    conn = connect(cfg.db_path)
    migrate(conn)
    DeviceRepo(conn).save_device(device_id, device_token, gateway, "local-device")
    print(f"Device enrolled: {device_id}")


async def cmd_run(cfg):
    """Main daemon loop."""
    conn = connect(cfg.db_path)
    migrate(conn)

    device_repo = DeviceRepo(conn)
    agent_repo = AgentRepo(conn)
    job_repo = JobRepo(conn)
    outbox_repo = OutboxRepo(conn)
    file_repo = FileRepo(conn)
    skill_repo = SkillRepo(conn)
    vnc_repo = VNCSessionRepo(conn)
    outbox_repo = OutboxRepo(conn)

    dev = device_repo.get_device()
    if not dev:
        logger.error("Device not enrolled. Run: iagent-device enroll")
        sys.exit(1)

    docker_mgr = DockerManager(
        agent_repo=agent_repo,
        image=cfg.agent_image,
        port_start=cfg.port_range_start,
        port_end=cfg.port_range_end,
        max_restarts=cfg.max_restarts,
        data_dir=str(cfg.device_data_dir),
    )

    await reconcile(docker_mgr, agent_repo, cfg.pool_size)

    outbox = Outbox(outbox_repo, None)  # send_fn set after tunnel created
    dispatcher = JobDispatcher(job_repo, agent_repo, docker_mgr, outbox)
    stager = FileStager(cfg.workspace_dir, file_repo, outbox)
    skills = SkillManager(cfg.skills_dir, skill_repo, agent_repo, docker_mgr, outbox)
    vnc_bridge = VNCBridge(vnc_repo, docker_mgr, outbox, cfg.session_dial_timeout_s)
    cred_relay = CredRelay(docker_mgr, outbox)
    monitor = Monitor(agent_repo, outbox)

    handlers = {
        str(FrameType.JOB_DISPATCH): lambda t, p: dispatcher.handle_job_dispatch(p),
        str(FrameType.JOB_CANCEL): lambda t, p: dispatcher.handle_job_cancel(p),
        str(FrameType.FILE_PUSH_BEGIN): lambda t, p: stager.handle_begin(p),
        str(FrameType.FILE_CHUNK): lambda t, p: stager.handle_chunk(p),
        str(FrameType.FILE_PUSH_END): lambda t, p: stager.handle_end(p),
        str(FrameType.SKILL_DISPATCH_BEGIN): lambda t, p: skills.handle_dispatch_begin(p),
        str(FrameType.SKILL_CHUNK): lambda t, p: skills.handle_chunk(p),
        str(FrameType.SKILL_DISPATCH_END): lambda t, p: skills.handle_dispatch_end(p),
        str(FrameType.SKILL_ACTION): lambda t, p: skills.handle_skill_action(p),
        str(FrameType.VNC_OPEN): lambda t, p: vnc_bridge.handle_vnc_open(p),
        str(FrameType.VNC_CLOSE): lambda t, p: vnc_bridge.handle_vnc_close(p),
        str(FrameType.CRED_PUSH): lambda t, p: cred_relay.handle_cred_push(p),
        str(FrameType.CRED_CAPTURE): lambda t, p: cred_relay.handle_cred_capture(p),
    }

    tunnel = TunnelClient(
        gateway_url=cfg.gateway_url,
        device_id=dev["device_id"],
        device_token=dev["token"],
        heartbeat_s=cfg.heartbeat_s,
        handlers=handlers,
        outbox=outbox,
    )
    outbox.send_fn = tunnel._send

    # Start background tasks
    async with asyncio.TaskGroup() as tg:
        tg.create_task(tunnel.run())
        tg.create_task(docker_mgr.health_loop())
        tg.create_task(monitor.run())


def cmd_status(cfg):
    cfg  # unused
    print("Status: see logs for local agent states")


def cmd_agents(cfg):
    cfg  # unused
    conn = connect(cfg.db_path)
    migrate(conn)
    for a in AgentRepo(conn).list_all():
        print(f"  {a['agent_id'][:8]}  {a['status']:10s}  port={a['port']}")


def cmd_logs(cfg, args):
    cfg; args
    print("Logs: check device log output")


def main():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(name)s] %(levelname)s: %(message)s")
    cfg = load_config()
    args = sys.argv[1:]

    if not args:
        print("Usage: iagent-device {enroll, run, status, agents, logs}")
        sys.exit(1)

    cmd = args[0]
    if cmd == "enroll":
        asyncio.run(cmd_enroll(cfg, args[1:]))
    elif cmd == "run":
        asyncio.run(cmd_run(cfg))
    elif cmd == "status":
        cmd_status(cfg)
    elif cmd == "agents":
        cmd_agents(cfg)
    elif cmd == "logs":
        cmd_logs(cfg, args[1:])
    else:
        print(f"Unknown command: {cmd}")
        sys.exit(1)


if __name__ == "__main__":
    main()
