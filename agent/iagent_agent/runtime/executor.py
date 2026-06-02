from __future__ import annotations

import asyncio
import logging

import httpx

from iagent_agent.adapter.protocol import AgentBrain, BrowserContext
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
        browser_manager: object | None = None,
    ):
        self._brain = brain
        self._workspace = workspace
        self._skills = skills
        self._browser = browser_manager
        self._current: JobRecord | None = None
        self._task: asyncio.Task[None] | None = None

    @property
    def busy(self) -> bool:
        return self._current is not None and not self._current.cancel_event.is_set()

    def current_job_id(self) -> str | None:
        if self._current is not None:
            return self._current.job_id
        return None

    def get_job_record(self) -> JobRecord | None:
        return self._current

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
    ) -> None:
        if self.busy:
            raise RuntimeError("BUSY")

        if skill_id:
            sk = self._skills.get_enabled_skill(skill_id)
            if sk is None:
                raise RuntimeError("SKILL_NOT_ENABLED")

        record = JobRecord(job_id=job_id)
        self._current = record
        self._task = asyncio.create_task(
            self._run(record, command, params, callback_url, skill_id, vnc_enabled, browser_display, browser_profile)
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
            self._workspace.wipe()
            self._current = None

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
    ) -> None:
        try:
            async with httpx.AsyncClient() as client:
                callback = None
                if callback_url:
                    callback = CallbackClient(client, callback_url, record.job_id)

                emit = await make_emit(record, callback)

                from iagent_agent.adapter.protocol import JobContext

                ctx = JobContext(
                    job_id=record.job_id,
                    command=command,
                    params=params,
                    inputs_dir=self._workspace.inputs,
                    output_dir=self._workspace.output,
                    skill_id=skill_id,
                    credentials_injected=False,
                    browser=BrowserContext(
                        display=browser_display,
                        profile_dir=browser_profile or self._workspace.profile,
                        vnc_enabled=vnc_enabled,
                    ),
                )

                record.status = JobState.RUNNING
                record.event_seq += 1
                if callback:
                    await callback.post_event(record)

                result = await self._brain.run(ctx, emit)

                record.status = JobState.SUCCEEDED
                record.result = result
                record.percent = 100
                record.message = result.summary
                record.event_seq += 1
                if callback:
                    await callback.post_event(record)

        except asyncio.CancelledError:
            record.status = JobState.CANCELLED
            record.message = "cancelled"
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
            self._workspace.wipe()
            self._current = None
