"""Unit tests for outbox durable delivery."""

import pytest

from iagent_device.tunnel.codec import FrameType
from iagent_device.tunnel.outbox import Outbox


class TestOutbox:
    @pytest.mark.asyncio
    async def test_enqueue_and_send(self, outbox_repo):
        sent = []

        async def capture_send(frame_type, payload):
            sent.append((frame_type, payload))

        ob = Outbox(outbox_repo, capture_send)
        await ob.enqueue_and_send(FrameType.JOB_PROGRESS, {"job_id": "j1", "percent": 50})

        assert len(sent) == 1
        assert sent[0][0] == FrameType.JOB_PROGRESS
        assert sent[0][1]["job_id"] == "j1"

        unacked = outbox_repo.list_unacked()
        assert len(unacked) == 1
        assert unacked[0]["type"] == "JOB_PROGRESS"

    @pytest.mark.asyncio
    async def test_ack_removes_from_unacked(self, outbox_repo):
        sent = []

        async def capture_send(frame_type, payload):
            sent.append((frame_type, payload))

        ob = Outbox(outbox_repo, capture_send)
        await ob.enqueue_and_send(FrameType.JOB_RESULT, {"job_id": "j2"})

        unacked = outbox_repo.list_unacked()
        assert len(unacked) == 1
        msg_id = unacked[0]["msg_id"]

        ob.ack(msg_id)
        assert len(outbox_repo.list_unacked()) == 0

    @pytest.mark.asyncio
    async def test_flush_resends_unacked(self, outbox_repo):
        sent = []

        async def capture_send(frame_type, payload):
            sent.append((frame_type, payload))

        ob = Outbox(outbox_repo, capture_send)

        outbox_repo.enqueue("m1", "JOB_PROGRESS", {"job_id": "j3"})
        outbox_repo.enqueue("m2", "JOB_RESULT", {"job_id": "j3"})

        await ob.flush()
        assert len(sent) == 2

    @pytest.mark.asyncio
    async def test_flush_empty(self, outbox_repo):
        sent = []

        async def capture_send(frame_type, payload):
            sent.append((frame_type, payload))

        ob = Outbox(outbox_repo, capture_send)
        await ob.flush()
        assert len(sent) == 0

    @pytest.mark.asyncio
    async def test_send_fn_receives_two_args(self, outbox_repo):
        arg_count = 0

        async def check_args(frame_type, payload):
            nonlocal arg_count
            arg_count += 1

        ob = Outbox(outbox_repo, check_args)
        await ob.enqueue_and_send(FrameType.PING, {})
        assert arg_count == 1

    @pytest.mark.asyncio
    async def test_delete_acked_cleans_up(self, outbox_repo):
        async def noop_send(frame_type, payload):
            pass

        ob = Outbox(outbox_repo, noop_send)
        await ob.enqueue_and_send(FrameType.JOB_PROGRESS, {"job_id": "j4"})
        unacked = outbox_repo.list_unacked()
        msg_id = unacked[0]["msg_id"]
        ob.ack(msg_id)

        outbox_repo.delete_acked()
        assert len(outbox_repo.list_unacked()) == 0
