"""Unit tests for frame encoding/decoding."""

import json
import pytest
from iagent_device.tunnel.codec import (
    FrameType, encode_frame, decode_frame, decode_payload,
    FRAME_MAX_SIZE, FRAME_VERSION, new_msg_id,
)


class TestEncodeFrame:
    def test_encode_basic(self):
        raw = encode_frame(FrameType.HELLO, {"device_id": "dev-1"})
        frame = json.loads(raw)
        assert frame["v"] == FRAME_VERSION
        assert frame["type"] == "HELLO"
        assert "msg_id" in frame
        assert "ts" in frame
        assert frame["payload"] == {"device_id": "dev-1"}

    def test_encode_with_ack(self):
        raw = encode_frame(FrameType.ACK, {}, ack_id="msg-123")
        frame = json.loads(raw)
        assert frame["ack_id"] == "msg-123"

    def test_encode_null_payload(self):
        raw = encode_frame(FrameType.PING)
        frame = json.loads(raw)
        assert "payload" not in frame or frame.get("payload") is None

    def test_msg_id_unique(self):
        ids = {new_msg_id() for _ in range(100)}
        assert len(ids) == 100


class TestDecodeFrame:
    def test_decode_valid_frame(self):
        raw = json.dumps({"v": 1, "type": "HELLO", "msg_id": "abc", "ts": 123, "payload": {"x": 1}})
        frame = decode_frame(raw)
        assert frame["type"] == "HELLO"
        assert frame["payload"] == {"x": 1}

    def test_decode_oversized_frame(self):
        big = "x" * (FRAME_MAX_SIZE + 1)
        with pytest.raises(ValueError, match="max size"):
            decode_frame(big)

    def test_decode_wrong_version(self):
        raw = json.dumps({"v": 99, "type": "HELLO", "msg_id": "abc"})
        with pytest.raises(ValueError, match="unsupported version"):
            decode_frame(raw)

    def test_decode_missing_type(self):
        raw = json.dumps({"v": 1, "msg_id": "abc"})
        with pytest.raises(ValueError, match="missing frame type"):
            decode_frame(raw)

    def test_decode_missing_msg_id(self):
        raw = json.dumps({"v": 1, "type": "HELLO"})
        with pytest.raises(ValueError, match="missing msg_id"):
            decode_frame(raw)

    def test_decode_payload(self):
        frame = {"v": 1, "type": "HELLO", "msg_id": "abc", "payload": {"k": "v"}}
        assert decode_payload(frame) == {"k": "v"}

    def test_decode_payload_empty(self):
        frame = {"v": 1, "type": "HELLO", "msg_id": "abc"}
        assert decode_payload(frame) == {}


class TestFrameTypeEnum:
    def test_all_types_string(self):
        for ft in FrameType:
            assert isinstance(ft.value, str)
            assert ft.value == ft.value.upper()

    def test_has_required_types(self):
        required = [
            "HELLO", "HELLO_ACK", "PING", "PONG", "ACK", "ERROR", "STATE_SYNC",
            "JOB_DISPATCH", "JOB_CANCEL", "JOB_QUERY", "JOB_ACCEPTED",
            "JOB_PROGRESS", "JOB_RESULT", "JOB_REJECTED",
            "AGENT_CREATE", "AGENT_ACTION", "AGENT_STATUS_REQ", "AGENT_STATUS",
            "SKILL_DISPATCH_BEGIN", "SKILL_CHUNK", "SKILL_DISPATCH_END",
            "SKILL_ACTION", "SKILL_STATE", "SKILL_SYNC",
            "FILE_PUSH_BEGIN", "FILE_CHUNK", "FILE_PUSH_END", "FILE_ACK",
            "VNC_OPEN", "VNC_OPENED", "VNC_CLOSE",
            "CRED_PUSH", "CRED_PUSH_ACK", "CRED_CAPTURE", "CRED_CAPTURE_ACK",
            "SKILL_DISPATCH_ACK", "FILE_PURGED",
        ]
        for name in required:
            assert FrameType[name] is not None
