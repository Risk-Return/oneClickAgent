import asyncio
import json
import logging
import os
import shutil
from pathlib import Path

from iagent_agent.adapter.protocol import JobContext, JobResult, ProgressEmitter

logger = logging.getLogger(__name__)

OPENDIR = shutil.which("opencode", path=os.environ.get("PATH", ""))
CLAUDE_SKILLS_DIR = Path.home() / ".claude" / "skills"

SKILL_PROMPT_TEMPLATE = """\
Use the skill instructions from ~/.claude/skills/{skill_name}/SKILL.md to complete this task.
{command}

Write all output files to {output_dir}. Create a summary at {output_dir}/summary.md when done."""


class OpenCodeBrain:
    def __init__(self):
        self._procs: dict[str, asyncio.subprocess.Process] = {}

    async def run(self, ctx: JobContext, emit: ProgressEmitter) -> JobResult:
        Path(ctx.output_dir).mkdir(parents=True, exist_ok=True)

        prompt = self._build_prompt(ctx)
        self._write_model_config()

        await emit(5, "starting opencode")
        logger.info("opencode[%s]: %s", ctx.job_id, ctx.command)

        try:
            proc = await asyncio.create_subprocess_exec(
                OPENDIR or "opencode",
                "run",
                "--dangerously-skip-permissions",
                prompt,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                env={**os.environ, "OPENCODE_OUTPUT_DIR": ctx.output_dir},
            )
            self._procs[ctx.job_id] = proc
            self._stderr = proc.stderr
        except FileNotFoundError:
            self._procs.pop(ctx.job_id, None)
            raise RuntimeError("opencode not found in PATH or IAGENT_BRAIN_PATH")

        try:
            await emit(10, "processing")
            stderr_task = asyncio.create_task(self._stream_stderr(ctx.job_id))
            await self._stream_output(ctx.job_id, proc, emit)
            stderr_task.cancel()
            await emit(95, "finalizing")

            stdout, stderr = await asyncio.wait_for(
                proc.communicate(), timeout=3600
            )

            rc = proc.returncode
            if rc == 0:
                summary = self._collect_summary(ctx)
                return JobResult(summary=summary, artifacts=[str(ctx.output_dir)])
            else:
                err = stderr.decode(errors="replace").strip() or stdout.decode(errors="replace").strip()
                raise RuntimeError(f"opencode exited {rc}: {err[:500]}")

        except asyncio.CancelledError:
            self._kill(ctx.job_id)
            raise
        finally:
            self._procs.pop(ctx.job_id, None)
            self._stderr = None

    async def cancel(self, job_id: str) -> None:
        self._kill(job_id)

    def _write_model_config(self) -> None:
        api_key = os.environ.get("OPENAI_API_KEY", "") or os.environ.get("ANTHROPIC_API_KEY", "")
        model = os.environ.get("OPENAI_MODEL", "") or os.environ.get("ANTHROPIC_MODEL", "")
        base_url = os.environ.get("OPENAI_BASE_URL", "") or os.environ.get("ANTHROPIC_BASE_URL", "")
        if not api_key or not model:
            return

        is_openai = bool(os.environ.get("OPENAI_API_KEY"))
        provider_id = "hboom"
        api_protocol = "openai" if is_openai else "anthropic"

        config_dir = Path(os.environ.get("XDG_CONFIG_HOME", Path.home() / ".config"))
        config_dir = config_dir / "opencode"
        config_dir.mkdir(parents=True, exist_ok=True)

        config: dict = {
            "model": f"{provider_id}/{model}",
            "provider": {
                provider_id: {
                    "id": provider_id,
                    "api": api_protocol,
                    "env": ["OPENAI_API_KEY"] if is_openai else ["ANTHROPIC_API_KEY"],
                    "models": {
                        model: {"name": model, "id": model},
                    },
                }
            },
        }

        if base_url:
            config["provider"][provider_id]["options"] = {"baseURL": base_url}

        config_path = config_dir / "opencode.jsonc"
        try:
            config_path.write_text(json.dumps(config))
            logger.info("opencode provider configured: %s/%s", provider_id, model)
        except OSError:
            pass

    def _build_prompt(self, ctx: JobContext) -> str:
        skill_name = ""
        if ctx.skill_id:
            skill_dir = CLAUDE_SKILLS_DIR / ctx.skill_id
            if not skill_dir.exists():
                for d in CLAUDE_SKILLS_DIR.iterdir():
                    if d.is_dir() and d.name == ctx.skill_id:
                        skill_dir = d
                        break
            if skill_dir.exists():
                skill_name = skill_dir.name

        return SKILL_PROMPT_TEMPLATE.format(
            skill_name=skill_name or "default",
            command=ctx.command,
            output_dir=ctx.output_dir,
        )

    async def _stream_output(self, job_id: str, proc: asyncio.subprocess.Process, emit: ProgressEmitter) -> None:
        last_pct = 10
        async for line in self._read_lines(proc.stdout):
            text = line.decode(errors="replace").strip()
            if not text:
                continue
            logger.info("opencode[%s]: %s", job_id, text[:500])
            pct = self._guess_progress(text)
            if pct is not None and pct > last_pct:
                last_pct = min(pct, 90)
                await emit(last_pct, text[:200])

    async def _stream_stderr(self, job_id: str) -> None:
        try:
            async for line in self._read_lines(self._stderr):
                text = line.decode(errors="replace").strip()
                if text:
                    logger.info("opencode[%s][stderr]: %s", job_id, text[:500])
        except asyncio.CancelledError:
            pass

    async def _read_lines(self, stream):
        while True:
            try:
                line = await asyncio.wait_for(stream.readline(), timeout=300)
            except asyncio.TimeoutError:
                continue
            if not line:
                break
            yield line

    def _guess_progress(self, text: str) -> int | None:
        import re

        m = re.search(r"(?:progress|done|complete)\D*(\d{1,3})%?", text, re.IGNORECASE)
        if m:
            return int(m.group(1))
        m = re.search(r"(\d{1,3})%", text)
        if m:
            return int(m.group(1))
        return None

    def _collect_summary(self, ctx: JobContext) -> str:
        summary_path = Path(ctx.output_dir) / "summary.md"
        if summary_path.exists():
            return summary_path.read_text()[:2000]
        md_files = sorted(Path(ctx.output_dir).rglob("*.md"))
        if md_files:
            return md_files[0].read_text()[:2000]
        return f"completed. Results saved to {ctx.output_dir}"

    def _kill(self, job_id: str) -> None:
        proc = self._procs.pop(job_id, None)
        if proc is None:
            return
        try:
            proc.kill()
        except ProcessLookupError:
            pass
        logger.info("opencode[%s]: killed", job_id)
