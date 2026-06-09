"""Env + file config loader with OS-aware defaults.
Reads IAGENT_GATEWAY_URL, IAGENT_DEVICE_DATA_DIR, IAGENT_DOCKER_HOST,
IAGENT_MAX_RESTARTS, IAGENT_HEARTBEAT_S, IAGENT_AGENT_IMAGE, IAGENT_PORT_RANGE,
IAGENT_AGENT_ENV (JSON dict of env vars to pass to agent containers), etc.
"""

import json
import os
import sys
from pathlib import Path
from dataclasses import dataclass, field

import platformdirs


def _default_data_dir() -> Path:
    return Path(platformdirs.user_data_dir("iagent-device", "oneClickAgent"))


def _default_docker_host() -> str:
    if sys.platform == "win32":
        return "npipe:////./pipe/docker_engine"
    return "unix:///var/run/docker.sock"


@dataclass
class Config:
    gateway_url: str = ""
    device_data_dir: Path = field(default_factory=_default_data_dir)
    docker_host: str = field(default_factory=_default_docker_host)
    max_restarts: int = 3
    heartbeat_s: int = 15
    agent_image: str = "iagent/agent:latest"
    prepull_image: bool = True
    port_range_start: int = 42000
    port_range_end: int = 42999
    session_dial_timeout_s: int = 15
    pool_size: int = 4
    agent_env: dict = field(default_factory=dict)

    _device_id: str | None = None
    _device_token: str | None = None
    _enrollment_code: str | None = None

    @property
    def db_path(self) -> Path:
        return self.device_data_dir / "device.db"

    @property
    def workspace_dir(self) -> Path:
        return self.device_data_dir / "work"

    @property
    def skills_dir(self) -> Path:
        return self.device_data_dir / "skills"

    @property
    def token_path(self) -> Path:
        return self.device_data_dir / "token"


def _load_llm_provider(data_dir: Path) -> dict:
    provider_file = data_dir / "llm_provider.json"
    if not provider_file.exists():
        return {}

    try:
        with open(provider_file) as f:
            cfg = json.load(f)
    except (json.JSONDecodeError, OSError):
        return {}

    provider = cfg.get("provider", "").lower()
    api = cfg.get("api", "").lower()
    api_key = cfg.get("api_key", "")
    base_url = cfg.get("base_url", "")
    model = cfg.get("model", "")

    env: dict = {}

    if api == "anthropic":
        env["ANTHROPIC_API_KEY"] = api_key
        if base_url:
            env["ANTHROPIC_BASE_URL"] = base_url
        if model:
            env["ANTHROPIC_MODEL"] = model
    elif api == "openai":
        env["OPENAI_API_KEY"] = api_key
        if base_url:
            env["OPENAI_BASE_URL"] = base_url
        if model:
            env["OPENAI_MODEL"] = model

    return env


def load() -> Config:
    cfg = Config()

    cfg.gateway_url = os.getenv("IAGENT_GATEWAY_URL", cfg.gateway_url)
    if data_dir := os.getenv("IAGENT_DEVICE_DATA_DIR"):
        cfg.device_data_dir = Path(data_dir)
    cfg.docker_host = os.getenv("IAGENT_DOCKER_HOST", cfg.docker_host)
    cfg.max_restarts = int(os.getenv("IAGENT_MAX_RESTARTS", str(cfg.max_restarts)))
    cfg.heartbeat_s = int(os.getenv("IAGENT_HEARTBEAT_S", str(cfg.heartbeat_s)))
    cfg.agent_image = os.getenv("IAGENT_AGENT_IMAGE", cfg.agent_image)
    cfg.prepull_image = os.getenv("IAGENT_PREPULL_IMAGE", str(cfg.prepull_image)).lower() != "false"
    cfg.session_dial_timeout_s = int(os.getenv("IAGENT_SESSION_DIAL_TIMEOUT_S", str(cfg.session_dial_timeout_s)))
    cfg.pool_size = int(os.getenv("IAGENT_POOL_SIZE", str(cfg.pool_size)))

    if agent_env_raw := os.getenv("IAGENT_AGENT_ENV"):
        try:
            cfg.agent_env = json.loads(agent_env_raw)
        except json.JSONDecodeError:
            pass

    if not cfg.agent_env:
        cfg.agent_env = _load_llm_provider(cfg.device_data_dir)

    if port_range := os.getenv("IAGENT_PORT_RANGE"):
        parts = port_range.split("-")
        if len(parts) == 2:
            cfg.port_range_start = int(parts[0])
            cfg.port_range_end = int(parts[1])

    # Ensure directories exist
    cfg.device_data_dir.mkdir(parents=True, exist_ok=True)
    cfg.workspace_dir.mkdir(parents=True, exist_ok=True)
    cfg.workspace_dir.chmod(0o777)
    (cfg.workspace_dir / "workspaces").mkdir(parents=True, exist_ok=True)
    (cfg.workspace_dir / "workspaces").chmod(0o777)
    cfg.skills_dir.mkdir(parents=True, exist_ok=True)

    return cfg
