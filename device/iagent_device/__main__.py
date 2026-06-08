"""CLI entrypoint: iagent-device {enroll, run, status, agents, logs, pull}.
- enroll: one-time setup with gateway enrolment code
- run: main daemon — prepull image, reconcile Docker, open tunnel, serve frames
- pull: force re-pull agent image
- status / agents / logs: local diagnostics
"""

import asyncio
import logging
import signal
import sys

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
from iagent_device.files.puller import FilePuller
from iagent_device.skills.manager import SkillManager
from iagent_device.vncbridge.bridge import VNCBridge
from iagent_device.creds.relay import CredRelay
from iagent_device.monitor.monitor import Monitor

logger = logging.getLogger("iagent.device")

_shutdown_event: asyncio.Event | None = None
_tunnel: TunnelClient | None = None
_tasks: list[asyncio.Task] | None = None


async def cmd_enroll(cfg, args):
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


async def cmd_pull(cfg):
    conn = connect(cfg.db_path)
    migrate(conn)

    docker_mgr = _make_docker_mgr(cfg, conn)
    await docker_mgr.prepull_image()
    print("Image pull complete.")


def _make_docker_mgr(cfg, conn) -> DockerManager:
    agent_repo = AgentRepo(conn)
    try:
        import docker
        dc = docker.from_env()
    except Exception:
        dc = None

    return DockerManager(
        agent_repo=agent_repo,
        image=cfg.agent_image,
        port_start=cfg.port_range_start,
        port_end=cfg.port_range_end,
        max_restarts=cfg.max_restarts,
        docker_client=dc,
        data_dir=str(cfg.device_data_dir),
        agent_env=cfg.agent_env,
    )


async def cmd_run(cfg):
    global _shutdown_event, _tunnel, _tasks
    _shutdown_event = asyncio.Event()

    conn = connect(cfg.db_path)
    migrate(conn)

    device_repo = DeviceRepo(conn)
    agent_repo = AgentRepo(conn)
    job_repo = JobRepo(conn)
    outbox_repo = OutboxRepo(conn)
    file_repo = FileRepo(conn)
    skill_repo = SkillRepo(conn)
    vnc_repo = VNCSessionRepo(conn)

    dev = device_repo.get_device()
    if not dev:
        logger.error("Device not enrolled. Run: iagent-device enroll")
        sys.exit(1)

    docker_mgr = _make_docker_mgr(cfg, conn)

    if cfg.prepull_image:
        logger.info("pre-pulling agent image %s ...", cfg.agent_image)
        await docker_mgr.prepull_image()

    await reconcile(docker_mgr, agent_repo, cfg.pool_size)

    outbox = Outbox(outbox_repo, None)
    stager = FileStager(cfg.workspace_dir, file_repo, outbox)
    puller = FilePuller(cfg.workspace_dir, outbox)
    skills = SkillManager(cfg.skills_dir, skill_repo, agent_repo, docker_mgr, outbox)
    vnc_bridge = VNCBridge(vnc_repo, docker_mgr, outbox, cfg.session_dial_timeout_s)
    cred_relay = CredRelay(docker_mgr, outbox)
    monitor = Monitor(agent_repo, outbox, docker_mgr)

    from iagent_device.jobs.callback_server import CallbackServer
    callback_server = CallbackServer("127.0.0.1", 0, outbox)
    await callback_server.start()

    dispatcher = JobDispatcher(
        job_repo=job_repo,
        agent_repo=agent_repo,
        docker_mgr=docker_mgr,
        outbox=outbox,
        stager=stager,
        puller=puller,
        cred_relay=cred_relay,
        callback_url=callback_server.callback_url,
    )

    hello_extras = monitor.build_hello_extras(agent_repo, vnc_enabled=True)

    handlers = {
        str(FrameType.JOB_DISPATCH): lambda t, p: dispatcher.handle_job_dispatch(p),
        str(FrameType.JOB_CANCEL): lambda t, p: dispatcher.handle_job_cancel(p),
        str(FrameType.JOB_QUERY): lambda t, p: _handle_job_query(p, job_repo, outbox),
        str(FrameType.AGENT_CREATE): lambda t, p: _handle_agent_create(p, docker_mgr, outbox),
        str(FrameType.AGENT_ACTION): lambda t, p: _handle_agent_action(p, docker_mgr, outbox),
        str(FrameType.AGENT_STATUS_REQ): lambda t, p: _handle_agent_status_req(p, agent_repo, outbox),
        str(FrameType.SKILL_SYNC): lambda t, p: skills.handle_skill_sync(p),
        str(FrameType.FILE_PUSH_BEGIN): lambda t, p: stager.handle_begin(p),
        str(FrameType.FILE_CHUNK): lambda t, p: stager.handle_chunk(p),
        str(FrameType.FILE_PUSH_END): lambda t, p: stager.handle_end(p),
        str(FrameType.SKILL_DISPATCH_BEGIN): lambda t, p: skills.handle_dispatch_begin(p),
        str(FrameType.SKILL_CHUNK): lambda t, p: skills.handle_chunk(p),
        str(FrameType.SKILL_DISPATCH_END): lambda t, p: skills.handle_dispatch_end(p),
        str(FrameType.SKILL_ACTION): lambda t, p: skills.handle_skill_action(p),
        str(FrameType.SKILL_RETRY): lambda t, p: skills.handle_skill_retry(p),
        str(FrameType.VNC_OPEN): lambda t, p: vnc_bridge.handle_vnc_open(p),
        str(FrameType.VNC_CLOSE): lambda t, p: vnc_bridge.handle_vnc_close(p),
        str(FrameType.CRED_PUSH): lambda t, p: cred_relay.handle_cred_push(p),
        str(FrameType.CRED_CAPTURE): lambda t, p: cred_relay.handle_cred_capture(p),
        str(FrameType.FILE_PULL_ACK): lambda t, p: puller.handle_pull_ack(p),
    }

    _tunnel = TunnelClient(
        gateway_url=cfg.gateway_url,
        device_id=dev["device_id"],
        device_token=dev["token"],
        heartbeat_s=cfg.heartbeat_s,
        handlers=handlers,
        outbox=outbox,
        hello_extras=hello_extras,
    )
    outbox.send_fn = _tunnel._send

    _tasks = []
    async with asyncio.TaskGroup() as tg:
        t1 = tg.create_task(_tunnel.run())
        t2 = tg.create_task(docker_mgr.health_loop())
        t3 = tg.create_task(monitor.run())
        t4 = tg.create_task(_outbox_pruner(outbox))
        _tasks = [t1, t2, t3, t4]
        await _shutdown_event.wait()

    await callback_server.stop()


async def _handle_job_query(payload: dict, job_repo: JobRepo, outbox: Outbox):
    job_id = payload.get("job_id", "")
    job = job_repo.get_by_id(job_id)
    if job:
        await outbox.enqueue_and_send(FrameType.JOB_QUERY_ACK, {
            "job_id": job_id,
            "status": job["status"],
            "percent": job.get("percent", 0),
        })


async def _handle_agent_create(payload: dict, docker_mgr: DockerManager, outbox: Outbox):
    agent_id = payload.get("agent_id", "")
    if agent_id:
        await docker_mgr.create_agent_with_id(agent_id)
        await outbox.enqueue_and_send(FrameType.AGENT_STATUS, {
            "agent_id": agent_id,
            "status": "idle",
        })
    else:
        count = payload.get("count", 1)
        await docker_mgr.ensure_pool(count)
        await outbox.enqueue_and_send(FrameType.AGENT_STATUS, {
            "status": "created",
            "count": count,
        })


async def _handle_agent_action(payload: dict, docker_mgr: DockerManager, outbox: Outbox):
    agent_id = payload.get("agent_id", "")
    action = payload.get("action", "")
    if not agent_id:
        return
    if action == "drain":
        logger.info("draining agent %s", agent_id)
        agent = docker_mgr.repo.get_by_id(agent_id)
        if agent:
            docker_mgr._remove_container(agent)
        docker_mgr.repo.delete(agent_id)
        await outbox.enqueue_and_send(FrameType.AGENT_STATUS, {
            "agent_id": agent_id,
            "status": "drained",
            "health": "stopped",
            "restarts": 0,
            "ts": int(time.time() * 1000),
        })
    elif action == "restart":
        docker_mgr._restart_container(agent_id)


async def _handle_agent_status_req(payload: dict, agent_repo: AgentRepo, outbox: Outbox):
    agent_id = payload.get("agent_id", "")
    if agent_id:
        agent = agent_repo.get_by_id(agent_id)
        if agent:
            await outbox.enqueue_and_send(FrameType.AGENT_STATUS, {
                "agent_id": agent_id,
                "status": agent.get("status", ""),
                "health": "healthy" if agent.get("status") not in ("failed", "unhealthy") else "unhealthy",
                "restarts": agent.get("restarts", 0),
                "usage": {"cpu_pct": 0, "mem_mb": 0, "disk_mb": 0},
                "ts": int(asyncio.get_event_loop().time() * 1000),
            })


async def _outbox_pruner(outbox: Outbox):
    while True:
        try:
            outbox.repo.delete_acked()
        except Exception:
            pass
        await asyncio.sleep(60)


def cmd_status(cfg):
    conn = connect(cfg.db_path)
    migrate(conn)
    device = DeviceRepo(conn).get_device()
    agents = AgentRepo(conn).list_all()

    if device:
        print(f"Device ID:    {device['device_id'][:16]}...")
        print(f"Gateway URL:  {device.get('gateway_url', '')}")
        print(f"Enrolled:     {device.get('enrolled_at', '')}")
    print(f"\nAgents ({len(agents)} total):")
    for a in agents:
        restarts = a.get("restarts", 0)
        print(f"  {a['agent_id'][:12]}  {a['status']:10s}  port={a['port']:>5}  restarts={restarts}")


def cmd_agents(cfg):
    conn = connect(cfg.db_path)
    migrate(conn)
    agents = AgentRepo(conn).list_all()
    if not agents:
        print("No agents found.")
        return
    for a in agents:
        cid = (a.get("container_id", "") or "")[:12]
        restarts = a.get("restarts", 0)
        job_id = (a.get("job_id", "") or "")[:12]
        print(f"  {a['agent_id'][:12]}  {a['status']:10s}  port={a['port']:>5}  restarts={restarts}  container={cid}  job={job_id}")


def cmd_logs(cfg, args):
    if args and args[0] == "--agent":
        agent_id = args[1] if len(args) > 1 else ""
        print(f"Logs for agent {agent_id}: check device log output for agent events")
    else:
        log_path = cfg.device_data_dir / "device.log"
        if log_path.exists():
            with open(log_path) as f:
                for line in f:
                    print(line, end="")
        else:
            print("No log file found. Check device log output.")


def _install_signal_handlers():
    try:
        loop = asyncio.get_running_loop()
    except RuntimeError:
        loop = asyncio.new_event_loop()
        asyncio.set_event_loop(loop)
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(
            sig,
            lambda: asyncio.create_task(_graceful_shutdown()),
        )


async def _graceful_shutdown():
    logger.info("shutting down gracefully...")
    if _shutdown_event:
        _shutdown_event.set()
    if _tunnel:
        try:
            await _tunnel.close()
        except Exception:
            pass


def main():
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(name)s] %(levelname)s: %(message)s",
        handlers=[
            logging.StreamHandler(),
        ],
    )
    cfg = load_config()

    log_file = cfg.device_data_dir / "device.log"
    file_handler = logging.FileHandler(str(log_file))
    file_handler.setFormatter(logging.Formatter("%(asctime)s [%(name)s] %(levelname)s: %(message)s"))
    logging.getLogger("iagent").addHandler(file_handler)

    args = sys.argv[1:]

    if not args:
        print("Usage: iagent-device {enroll, run, pull, status, agents, logs}")
        sys.exit(1)

    cmd = args[0]
    if cmd == "enroll":
        asyncio.run(cmd_enroll(cfg, args[1:]))
    elif cmd == "run":
        _install_signal_handlers()
        asyncio.run(cmd_run(cfg))
    elif cmd == "pull":
        asyncio.run(cmd_pull(cfg))
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
