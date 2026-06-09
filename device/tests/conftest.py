"""Shared test fixtures for iagent_device tests."""

import sqlite3
import tempfile
import pytest
from pathlib import Path

from iagent_device.store.repositories import (
    DeviceRepo, AgentRepo, JobRepo, OutboxRepo, FileRepo, SkillRepo, VNCSessionRepo,
)
from iagent_device.tunnel.outbox import Outbox


@pytest.fixture
def db_conn():
    conn = sqlite3.connect(":memory:")
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA journal_mode=WAL")
    conn.execute("PRAGMA foreign_keys=ON")

    conn.executescript("""
    CREATE TABLE IF NOT EXISTS device_info (
        device_id TEXT PRIMARY KEY, name TEXT, token TEXT,
        gateway_url TEXT, enrolled_at TEXT
    );
    CREATE TABLE IF NOT EXISTS agents (
        agent_id TEXT PRIMARY KEY, name TEXT, image TEXT,
        container_id TEXT, port INTEGER, tags TEXT, status TEXT,
        limits_json TEXT, user_id TEXT, job_id TEXT,
        restarts INTEGER DEFAULT 0, created_at TEXT, updated_at TEXT
    );
    CREATE TABLE IF NOT EXISTS jobs (
        job_id TEXT PRIMARY KEY, agent_id TEXT, user_id TEXT,
        command TEXT, params_json TEXT, skill_id TEXT, status TEXT,
        percent INTEGER DEFAULT 0, workspace_dir TEXT, result_json TEXT,
        error_json TEXT, credential_ids TEXT, acked_by_cloud INTEGER DEFAULT 0,
        created_at TEXT, updated_at TEXT
    );
    CREATE TABLE IF NOT EXISTS files (
        file_id TEXT PRIMARY KEY, job_id TEXT, name TEXT,
        size INTEGER, sha256 TEXT, local_path TEXT,
        status TEXT, created_at TEXT, purged_at TEXT
    );
    CREATE TABLE IF NOT EXISTS outbox (
        msg_id TEXT PRIMARY KEY, type TEXT, payload TEXT,
        created_at TEXT, acked INTEGER DEFAULT 0
    );
    CREATE TABLE IF NOT EXISTS device_skills (
        skill_id TEXT PRIMARY KEY, key TEXT, name TEXT,
        version TEXT, manifest TEXT, artifact_path TEXT,
        sha256 TEXT, status TEXT, updated_at TEXT
    );
    CREATE TABLE IF NOT EXISTS agent_skills (
        agent_id TEXT, skill_id TEXT, status TEXT, updated_at TEXT,
        PRIMARY KEY (agent_id, skill_id)
    );
    CREATE TABLE IF NOT EXISTS vnc_sessions (
        session_id TEXT PRIMARY KEY, job_id TEXT, agent_id TEXT,
        rfb_port INTEGER, rfb_password TEXT, relay_url TEXT,
        session_token TEXT, status TEXT, created_at TEXT, ended_at TEXT
    );
    """)
    conn.commit()
    yield conn
    conn.close()


@pytest.fixture
def agent_repo(db_conn):
    return AgentRepo(db_conn)


@pytest.fixture
def job_repo(db_conn):
    return JobRepo(db_conn)


@pytest.fixture
def outbox_repo(db_conn):
    return OutboxRepo(db_conn)


@pytest.fixture
def file_repo(db_conn):
    return FileRepo(db_conn)


@pytest.fixture
def skill_repo(db_conn):
    return SkillRepo(db_conn)


@pytest.fixture
def vnc_repo(db_conn):
    return VNCSessionRepo(db_conn)


@pytest.fixture
def device_repo(db_conn):
    return DeviceRepo(db_conn)


@pytest.fixture
def outbox(outbox_repo):
    def noop_send(frame_type, payload, msg_id=None):
        pass

    return Outbox(outbox_repo, noop_send)


@pytest.fixture
def tmp_workspace():
    with tempfile.TemporaryDirectory() as d:
        yield Path(d)


@pytest.fixture
def tmp_skills_dir():
    with tempfile.TemporaryDirectory() as d:
        yield Path(d)
