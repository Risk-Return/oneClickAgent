"""Frame encode/decode for the tunnel protocol (JSON text frames).
Validates envelope structure, max size, and provides type constants
matching 05-tunnel-protocol.md.
"""

import json
import uuid
import time
from enum import StrEnum


FRAME_VERSION = 1
FRAME_MAX_SIZE = 1 << 20  # 1 MiB


class FrameType(StrEnum):
    # Control
    HELLO = "HELLO"
    HELLO_ACK = "HELLO_ACK"
    PING = "PING"
    PONG = "PONG"
    ACK = "ACK"
    ERROR = "ERROR"
    STATE_SYNC = "STATE_SYNC"

    # Job
    JOB_DISPATCH = "JOB_DISPATCH"
    JOB_CANCEL = "JOB_CANCEL"
    JOB_QUERY = "JOB_QUERY"
    JOB_ACCEPTED = "JOB_ACCEPTED"
    JOB_PROGRESS = "JOB_PROGRESS"
    JOB_RESULT = "JOB_RESULT"
    JOB_REJECTED = "JOB_REJECTED"

    # Agent
    AGENT_CREATE = "AGENT_CREATE"
    AGENT_ACTION = "AGENT_ACTION"
    AGENT_STATUS_REQ = "AGENT_STATUS_REQ"
    AGENT_STATUS = "AGENT_STATUS"

    # Skills
    SKILL_DISPATCH_BEGIN = "SKILL_DISPATCH_BEGIN"
    SKILL_CHUNK = "SKILL_CHUNK"
    SKILL_DISPATCH_END = "SKILL_DISPATCH_END"
    SKILL_ACTION = "SKILL_ACTION"
    SKILL_RETRY = "SKILL_RETRY"
    SKILL_STATE = "SKILL_STATE"
    SKILL_SYNC = "SKILL_SYNC"

    # Files
    FILE_PUSH_BEGIN = "FILE_PUSH_BEGIN"
    FILE_CHUNK = "FILE_CHUNK"
    FILE_PUSH_END = "FILE_PUSH_END"
    FILE_ACK = "FILE_ACK"

    # VNC
    VNC_OPEN = "VNC_OPEN"
    VNC_OPENED = "VNC_OPENED"
    VNC_CLOSE = "VNC_CLOSE"

    # Credentials
    CRED_PUSH = "CRED_PUSH"
    CRED_PUSH_ACK = "CRED_PUSH_ACK"
    CRED_CAPTURE = "CRED_CAPTURE"
    CRED_CAPTURE_ACK = "CRED_CAPTURE_ACK"

    SKILL_DISPATCH_ACK = "SKILL_DISPATCH_ACK"
    FILE_PURGED = "FILE_PURGED"

    JOB_QUERY_ACK = "JOB_QUERY_ACK"
    AGENT_STATUS_ACK = "AGENT_STATUS_ACK"
    SKILL_STATE_ACK = "SKILL_STATE_ACK"


def new_msg_id() -> str:
    return str(uuid.uuid4())


def encode_frame(frame_type: FrameType, payload: dict | None = None, ack_id: str | None = None) -> str:
    frame = {
        "v": FRAME_VERSION,
        "type": str(frame_type),
        "msg_id": new_msg_id(),
        "ts": int(time.time() * 1000),
    }
    if ack_id:
        frame["ack_id"] = ack_id
    if payload:
        frame["payload"] = payload
    return json.dumps(frame)


def decode_frame(raw: str) -> dict:
    if len(raw) > FRAME_MAX_SIZE:
        raise ValueError(f"frame exceeds max size: {len(raw)} > {FRAME_MAX_SIZE}")
    frame = json.loads(raw)
    if frame.get("v") != FRAME_VERSION:
        raise ValueError(f"unsupported version: {frame.get('v')}")
    if "type" not in frame:
        raise ValueError("missing frame type")
    if "msg_id" not in frame:
        raise ValueError("missing msg_id")
    return frame


def decode_payload(frame: dict) -> dict:
    return frame.get("payload", {})
