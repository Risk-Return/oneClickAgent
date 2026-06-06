"""SQLite repositories: device_info, agents (pool state), jobs,
files, device_skills, agent_skills, outbox, vnc_sessions.
"""

import json
import sqlite3
import time


class DeviceRepo:
    def __init__(self, conn: sqlite3.Connection):
        self.conn = conn

    def save_device(self, device_id: str, token: str, gateway_url: str, name: str = ""):
        self.conn.execute(
            "INSERT OR REPLACE INTO device_info (device_id, name, token, gateway_url, enrolled_at) VALUES (?,?,?,?,?)",
            (device_id, name, token, gateway_url, time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())),
        )
        self.conn.commit()

    def get_device(self) -> dict | None:
        row = self.conn.execute("SELECT * FROM device_info LIMIT 1").fetchone()
        return dict(row) if row else None

    def get_token(self) -> str | None:
        d = self.get_device()
        return d["token"] if d else None

    def get_device_id(self) -> str | None:
        d = self.get_device()
        return d["device_id"] if d else None


class AgentRepo:
    def __init__(self, conn: sqlite3.Connection):
        self.conn = conn

    def upsert(self, agent_id: str, name: str, image: str, port: int, container_id: str = "", status: str = "creating"):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.conn.execute(
            """INSERT OR REPLACE INTO agents (agent_id, name, image, container_id, port, status, created_at, updated_at)
               VALUES (?,?,?,?,?,?,COALESCE((SELECT created_at FROM agents WHERE agent_id=?),?),?)""",
            (agent_id, name, image, container_id, port, status, agent_id, now, now),
        )
        self.conn.commit()

    def update_status(self, agent_id: str, status: str, container_id: str = ""):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        params = [status, now, agent_id]
        if container_id:
            self.conn.execute(
                "UPDATE agents SET status=?, container_id=?, updated_at=? WHERE agent_id=?",
                (status, container_id, now, agent_id),
            )
        else:
            self.conn.execute(
                "UPDATE agents SET status=?, updated_at=? WHERE agent_id=?", params
            )
        self.conn.commit()

    def allocate(self, agent_id: str, user_id: str, job_id: str):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.conn.execute(
            "UPDATE agents SET status='busy', user_id=?, job_id=?, updated_at=? WHERE agent_id=?",
            (user_id, job_id, now, agent_id),
        )
        self.conn.commit()

    def release(self, agent_id: str):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.conn.execute(
            "UPDATE agents SET status='idle', user_id=NULL, job_id=NULL, updated_at=? WHERE agent_id=?",
            (now, agent_id),
        )
        self.conn.commit()

    def list_idle(self) -> list[dict]:
        rows = self.conn.execute("SELECT * FROM agents WHERE status='idle'").fetchall()
        return [dict(r) for r in rows]

    def list_all(self) -> list[dict]:
        rows = self.conn.execute("SELECT * FROM agents ORDER BY created_at").fetchall()
        return [dict(r) for r in rows]

    def get_by_id(self, agent_id: str) -> dict | None:
        row = self.conn.execute("SELECT * FROM agents WHERE agent_id=?", (agent_id,)).fetchone()
        return dict(row) if row else None

    def increment_restarts(self, agent_id: str):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.conn.execute(
            "UPDATE agents SET restarts=restarts+1, status='unhealthy', updated_at=? WHERE agent_id=?",
            (now, agent_id),
        )
        self.conn.commit()

    def delete(self, agent_id: str):
        self.conn.execute("DELETE FROM agents WHERE agent_id=?", (agent_id,))
        self.conn.commit()


class JobRepo:
    def __init__(self, conn: sqlite3.Connection):
        self.conn = conn

    def create(self, job_id: str, agent_id: str, user_id: str, command: str, skill_id: str = "", credential_ids: str = ""):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.conn.execute(
            "INSERT OR REPLACE INTO jobs (job_id, agent_id, user_id, command, skill_id, credential_ids, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)",
            (job_id, agent_id, user_id, command, skill_id, credential_ids, "queued", now, now),
        )
        self.conn.commit()

    def update_status(self, job_id: str, status: str, percent: int = 0):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.conn.execute(
            "UPDATE jobs SET status=?, percent=?, updated_at=? WHERE job_id=?",
            (status, percent, now, job_id),
        )
        self.conn.commit()

    def update_result(self, job_id: str, status: str, result: dict | None = None):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.conn.execute(
            "UPDATE jobs SET status=?, result_json=?, updated_at=? WHERE job_id=?",
            (status, json.dumps(result) if result else None, now, job_id),
        )
        self.conn.commit()

    def get_by_id(self, job_id: str) -> dict | None:
        row = self.conn.execute("SELECT * FROM jobs WHERE job_id=?", (job_id,)).fetchone()
        return dict(row) if row else None

    def list_by_agent(self, agent_id: str) -> list[dict]:
        rows = self.conn.execute("SELECT * FROM jobs WHERE agent_id=? ORDER BY created_at DESC", (agent_id,)).fetchall()
        return [dict(r) for r in rows]

    def delete(self, job_id: str):
        self.conn.execute("DELETE FROM jobs WHERE job_id=?", (job_id,))
        self.conn.commit()


class OutboxRepo:
    def __init__(self, conn: sqlite3.Connection):
        self.conn = conn

    def enqueue(self, msg_id: str, frame_type: str, payload: dict):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.conn.execute(
            "INSERT INTO outbox (msg_id, type, payload, created_at) VALUES (?,?,?,?)",
            (msg_id, frame_type, json.dumps(payload), now),
        )
        self.conn.commit()

    def mark_acked(self, msg_id: str):
        self.conn.execute("UPDATE outbox SET acked=1 WHERE msg_id=?", (msg_id,))
        self.conn.commit()

    def list_unacked(self) -> list[dict]:
        rows = self.conn.execute("SELECT * FROM outbox WHERE acked=0 ORDER BY created_at").fetchall()
        return [dict(r) for r in rows]

    def delete_acked(self):
        self.conn.execute("DELETE FROM outbox WHERE acked=1")
        self.conn.commit()


class FileRepo:
    def __init__(self, conn: sqlite3.Connection):
        self.conn = conn

    def create(self, file_id: str, job_id: str, name: str, size: int, sha256: str, local_path: str):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.conn.execute(
            "INSERT INTO files (file_id, job_id, name, size, sha256, local_path, status, created_at) VALUES (?,?,?,?,?,?,?,?)",
            (file_id, job_id, name, size, sha256, local_path, "staged_device", now),
        )
        self.conn.commit()

    def list_by_job(self, job_id: str) -> list[dict]:
        rows = self.conn.execute("SELECT * FROM files WHERE job_id=?", (job_id,)).fetchall()
        return [dict(r) for r in rows]

    def count_pending(self, job_id: str) -> int:
        rows = self.conn.execute(
            "SELECT COUNT(*) FROM files WHERE job_id=? AND status NOT IN ('staged','staged_device','purged')",
            (job_id,),
        ).fetchone()
        return rows[0] if rows else 0

    def purge(self, file_id: str):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.conn.execute("UPDATE files SET status='purged', purged_at=? WHERE file_id=?", (now, file_id))
        self.conn.commit()


class SkillRepo:
    def __init__(self, conn: sqlite3.Connection):
        self.conn = conn

    def upsert_device_skill(self, skill_id: str, key: str, name: str, version: str, manifest: str, artifact_path: str, sha256: str, status: str):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.conn.execute(
            "INSERT OR REPLACE INTO device_skills (skill_id, key, name, version, manifest, artifact_path, sha256, status, updated_at) VALUES (?,?,?,?,?,?,?,?,?)",
            (skill_id, key, name, version, manifest, artifact_path, sha256, status, now),
        )
        self.conn.commit()

    def update_device_skill_status(self, skill_id: str, status: str):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.conn.execute("UPDATE device_skills SET status=?, updated_at=? WHERE skill_id=?", (status, now, skill_id))
        self.conn.commit()

    def list_device_skills(self) -> list[dict]:
        rows = self.conn.execute("SELECT * FROM device_skills").fetchall()
        return [dict(r) for r in rows]

    def delete_device_skill(self, skill_id: str):
        self.conn.execute("DELETE FROM device_skills WHERE skill_id=?", (skill_id,))
        self.conn.commit()

    def upsert_agent_skill(self, agent_id: str, skill_id: str, status: str):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.conn.execute(
            "INSERT OR REPLACE INTO agent_skills (agent_id, skill_id, status, updated_at) VALUES (?,?,?,?)",
            (agent_id, skill_id, status, now),
        )
        self.conn.commit()

    def list_agent_skills(self, agent_id: str) -> list[dict]:
        rows = self.conn.execute("SELECT * FROM agent_skills WHERE agent_id=?", (agent_id,)).fetchall()
        return [dict(r) for r in rows]


class VNCSessionRepo:
    def __init__(self, conn: sqlite3.Connection):
        self.conn = conn

    def create(self, session_id: str, job_id: str, agent_id: str, relay_url: str, session_token: str):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.conn.execute(
            "INSERT INTO vnc_sessions (session_id, job_id, agent_id, relay_url, session_token, status, created_at) VALUES (?,?,?,?,?,?,?)",
            (session_id, job_id, agent_id, relay_url, session_token, "pending", now),
        )
        self.conn.commit()

    def update_status(self, session_id: str, status: str, rfb_port: int = 0, rfb_password: str = ""):
        self.conn.execute(
            "UPDATE vnc_sessions SET status=?, rfb_port=?, rfb_password=? WHERE session_id=?",
            (status, rfb_port, rfb_password, session_id),
        )
        self.conn.commit()

    def close(self, session_id: str):
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.conn.execute("UPDATE vnc_sessions SET status='closed', ended_at=? WHERE session_id=?", (now, session_id))
        self.conn.commit()
