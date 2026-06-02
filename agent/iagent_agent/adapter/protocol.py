from typing import Awaitable, Callable, Optional, Protocol

from pydantic import BaseModel


class BrowserContext(BaseModel):
    display: str
    profile_dir: str
    vnc_enabled: bool


class JobContext(BaseModel):
    job_id: str
    command: str
    params: dict = {}
    inputs_dir: str
    output_dir: str
    skill_id: Optional[str] = None
    credentials_injected: bool = False
    browser: Optional[BrowserContext] = None


class JobResult(BaseModel):
    summary: str
    artifacts: list[str] = []


ProgressEmitter = Callable[[int, str], Awaitable[None]]


class AgentBrain(Protocol):
    async def run(self, ctx: JobContext, emit: ProgressEmitter) -> JobResult: ...

    async def cancel(self, job_id: str) -> None: ...
