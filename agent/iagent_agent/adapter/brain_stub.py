import asyncio
import logging

from iagent_agent.adapter.protocol import JobContext, JobResult, ProgressEmitter

logger = logging.getLogger(__name__)


class StubBrain:
    STEPS = [
        (20, "gathering context"),
        (40, "processing request"),
        (60, "generating output"),
        (80, "finalizing"),
    ]

    def __init__(self, delay: float = 0.1):
        self._delay = delay
        self._cancelled: set[str] = set()

    async def run(self, ctx: JobContext, emit: ProgressEmitter) -> JobResult:
        self._cancelled.discard(ctx.job_id)
        for pct, msg in self.STEPS:
            if ctx.job_id in self._cancelled:
                logger.info("stub brain cancelled: %s", ctx.job_id)
                return JobResult(summary="cancelled")
            await emit(pct, msg)
            await asyncio.sleep(self._delay)
        summary = f"completed command: {ctx.command}"
        logger.info("stub brain completed: %s", ctx.job_id)
        return JobResult(summary=summary, artifacts=[])

    async def cancel(self, job_id: str) -> None:
        self._cancelled.add(job_id)
        logger.info("stub brain cancel requested: %s", job_id)
