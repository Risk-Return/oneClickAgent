"""SQLite connection management: WAL mode, single writer,
versioned migration application on startup.
"""

import sqlite3
from pathlib import Path


def connect(db_path: Path) -> sqlite3.Connection:
    conn = sqlite3.connect(str(db_path))
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA journal_mode=WAL")
    conn.execute("PRAGMA foreign_keys=ON")
    conn.execute("PRAGMA busy_timeout=5000")
    return conn


def migrate(conn: sqlite3.Connection) -> None:
    conn.execute("""
        CREATE TABLE IF NOT EXISTS schema_version (
            version INTEGER PRIMARY KEY
        )
    """)
    cur = conn.execute("SELECT MAX(version) FROM schema_version")
    current = (cur.fetchone()[0] or 0)

    if current < 1:
        conn.executescript(SCHEMA_V1)
        conn.execute("INSERT INTO schema_version VALUES (1)")

    conn.commit()


SCHEMA_V1 = """
CREATE TABLE IF NOT EXISTS device_info (
    device_id   TEXT PRIMARY KEY,
    name        TEXT,
    token       TEXT,
    gateway_url TEXT,
    enrolled_at TEXT
);

CREATE TABLE IF NOT EXISTS agents (
    agent_id     TEXT PRIMARY KEY,
    name         TEXT,
    image        TEXT,
    container_id TEXT,
    port         INTEGER,
    tags         TEXT,
    status       TEXT,
    limits_json  TEXT,
    user_id      TEXT,
    job_id       TEXT,
    restarts     INTEGER DEFAULT 0,
    created_at   TEXT,
    updated_at   TEXT
);

CREATE TABLE IF NOT EXISTS jobs (
    job_id        TEXT PRIMARY KEY,
    agent_id      TEXT,
    user_id       TEXT,
    command       TEXT,
    params_json   TEXT,
    skill_id      TEXT,
    status        TEXT,
    percent       INTEGER DEFAULT 0,
    workspace_dir TEXT,
    result_json   TEXT,
    error_json    TEXT,
    credential_ids TEXT,
    acked_by_cloud INTEGER DEFAULT 0,
    created_at    TEXT,
    updated_at    TEXT
);

CREATE TABLE IF NOT EXISTS files (
    file_id     TEXT PRIMARY KEY,
    job_id      TEXT,
    name        TEXT,
    size        INTEGER,
    sha256      TEXT,
    local_path  TEXT,
    status      TEXT,
    created_at  TEXT,
    purged_at   TEXT
);

CREATE TABLE IF NOT EXISTS outbox (
    msg_id     TEXT PRIMARY KEY,
    type       TEXT,
    payload    TEXT,
    created_at TEXT,
    acked      INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS device_skills (
    skill_id     TEXT PRIMARY KEY,
    key          TEXT,
    name         TEXT,
    version      TEXT,
    manifest     TEXT,
    artifact_path TEXT,
    sha256       TEXT,
    status       TEXT,
    updated_at   TEXT
);

CREATE TABLE IF NOT EXISTS agent_skills (
    agent_id    TEXT,
    skill_id    TEXT,
    status      TEXT,
    updated_at  TEXT,
    PRIMARY KEY (agent_id, skill_id)
);

CREATE TABLE IF NOT EXISTS vnc_sessions (
    session_id  TEXT PRIMARY KEY,
    job_id      TEXT,
    agent_id    TEXT,
    rfb_port    INTEGER,
    rfb_password TEXT,
    relay_url   TEXT,
    session_token TEXT,
    status      TEXT,
    created_at  TEXT,
    closed_at   TEXT
);
"""
