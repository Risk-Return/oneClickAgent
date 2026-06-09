from __future__ import annotations

import asyncio
import logging
import time as _time
from pathlib import Path

import httpx

from iagent_agent.adapter.protocol import AgentBrain, BrowserContext
from iagent_agent.browser.manager import BrowserManager, VNCStack
from iagent_agent.runtime.context import (
    CallbackClient,
    JobRecord,
    JobState,
    make_emit,
)
from iagent_agent.skills.loader import SkillManager
from iagent_agent.workspace import Workspace

logger = logging.getLogger(__name__)


class JobExecutor:
    def __init__(
        self,
        brain: AgentBrain,
        workspace: Workspace,
        skills: SkillManager,
        browser_manager: BrowserManager | None = None,
        vnc_stack: VNCStack | None = None,
    ):
        self._brain = brain
        self._workspace = workspace
        self._skills = skills
        self._browser = browser_manager
        self._vnc = vnc_stack
        self._current: JobRecord | None = None
        self._task: asyncio.Task[None] | None = None
        self._credentials_pending: bool = False
        self._job_events: list[dict] = []

    @property
    def busy(self) -> bool:
        return self._current is not None and not self._current.cancel_event.is_set()

    def current_job_id(self) -> str | None:
        if self._current is not None:
            return self._current.job_id
        return None

    def post_event(self, payload: dict) -> int:
        seq = len(self._job_events)
        payload["event_seq"] = seq
        self._job_events.append(payload)
        if len(self._job_events) > 32:
            self._job_events = self._job_events[-32:]
        return seq

    def get_events_since(self, since: int) -> list[dict]:
        return [e for e in self._job_events if e.get("event_seq", 0) >= since]

    def clear_events(self) -> None:
        self._job_events = []

    def get_job_record(self) -> JobRecord | None:
        return self._current

    def mark_credentials_injected(self) -> None:
        self._credentials_pending = True

    async def submit(
        self,
        job_id: str,
        command: str,
        params: dict,
        callback_url: str | None,
        skill_id: str | None,
        vnc_enabled: bool = False,
        browser_display: str = ":99",
        browser_profile: str = "",
        workspace_dir: str = "",
    ) -> None:
        if self.busy:
            raise RuntimeError("BUSY")

        self.clear_events()

        if skill_id:
            sk = self._skills.get_enabled_skill(skill_id)
            if sk is None:
                raise RuntimeError("SKILL_NOT_ENABLED")

        record = JobRecord(job_id=job_id)
        self._current = record
        self._task = asyncio.create_task(
            self._run(record, command, params, callback_url, skill_id, vnc_enabled, browser_display, browser_profile, workspace_dir)
        )

    async def cancel(self) -> None:
        if self._current is None:
            return
        self._current.cancel_event.set()
        await self._brain.cancel(self._current.job_id)
        if self._task and not self._task.done():
            self._task.cancel()
            try:
                await self._task
            except asyncio.CancelledError:
                pass

        if self._current is not None:
            self._current.status = JobState.CANCELLED
            self._current.message = "cancelled"
            self._teardown()
            self._current = None

    def _teardown(self) -> None:
        if self._browser:
            if hasattr(self._browser, "save_storage_state"):
                self._browser.save_storage_state()
            self._browser.kill()
        if self._vnc:
            self._vnc.stop()
        self._credentials_pending = False

    async def _run(
        self,
        record: JobRecord,
        command: str,
        params: dict,
        callback_url: str | None,
        skill_id: str | None,
        vnc_enabled: bool,
        browser_display: str,
        browser_profile: str,
        workspace_dir: str = "",
    ) -> None:
        credentials_injected = self._credentials_pending
        self._credentials_pending = False

        self._workspace.check_quota()

        try:
            async with httpx.AsyncClient() as client:
                callback = None
                if callback_url:
                    callback = CallbackClient(client, callback_url, record.job_id)

                emit = await make_emit(record, callback)

                from iagent_agent.adapter.protocol import JobContext

                inputs_dir = self._workspace.inputs
                output_dir = self._workspace.output
                if workspace_dir:
                    ws = Path(workspace_dir)
                    ws.mkdir(parents=True, exist_ok=True)
                    inputs_dir = str(ws / "inputs")
                    output_dir = str(ws / "output")
                    Path(inputs_dir).mkdir(parents=True, exist_ok=True)
                    Path(output_dir).mkdir(parents=True, exist_ok=True)

                ctx = JobContext(
                    job_id=record.job_id,
                    command=command,
                    params=params,
                    inputs_dir=inputs_dir,
                    output_dir=output_dir,
                    skill_id=skill_id,
                    credentials_injected=credentials_injected,
                    browser=BrowserContext(
                        display=browser_display,
                        profile_dir=browser_profile or self._workspace.profile,
                        vnc_enabled=vnc_enabled,
                    ),
                )

                record.status = JobState.RUNNING
                record.started_at = _time.time()
                record.event_seq += 1
                if callback:
                    await callback.post_event(record)

                result = await self._brain.run(ctx, emit)

                record.status = JobState.SUCCEEDED
                record.result = result
                record.percent = 100
                record.message = result.summary
                record.finished_at = _time.time()
                record.event_seq += 1
                if callback:
                    await callback.post_event(record)

        except asyncio.CancelledError:
            record.status = JobState.CANCELLED
            record.message = "cancelled"
            record.finished_at = _time.time()
            record.event_seq += 1
            try:
                if callback_url:
                    async with httpx.AsyncClient() as client:
                        cb = CallbackClient(client, callback_url, record.job_id)
                        await cb.post_event(record)
            except Exception:
                pass
            raise

        except Exception as exc:
            record.status = JobState.FAILED
            record.error = str(exc)
            record.message = str(exc)
            record.finished_at = _time.time()
            record.event_seq += 1
            try:
                if callback_url:
                    async with httpx.AsyncClient() as client:
                        cb = CallbackClient(client, callback_url, record.job_id)
                        await cb.post_event(record)
            except Exception:
                pass
            logger.exception("job %s failed", record.job_id)

        finally:
            self._teardown()
            self._current = None
