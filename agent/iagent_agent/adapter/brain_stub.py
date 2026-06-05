import asyncio
import logging
from pathlib import Path

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

        # Ensure output directory exists
        Path(ctx.output_dir).mkdir(parents=True, exist_ok=True)

        artifacts: list[str] = []
        for pct, msg in self.STEPS:
            if ctx.job_id in self._cancelled:
                logger.info("stub brain cancelled: %s", ctx.job_id)
                return JobResult(summary="cancelled", artifacts=[])
            await emit(pct, msg)
            await asyncio.sleep(self._delay)

        # Write a summary file to output_dir (real implementation should save actual results)
        summary_path = Path(ctx.output_dir) / "summary.md"
        summary_path.write_text(f"# Job Result\n\nCommand: {ctx.command}\n\nStatus: completed\n")
        artifacts.append(str(summary_path))

        summary = f"completed command. Results saved to {ctx.output_dir}"
        logger.info("stub brain completed: %s", ctx.job_id)
        return JobResult(summary=summary, artifacts=artifacts)

    async def cancel(self, job_id: str) -> None:
        self._cancelled.add(job_id)
        logger.info("stub brain cancel requested: %s", job_id)
