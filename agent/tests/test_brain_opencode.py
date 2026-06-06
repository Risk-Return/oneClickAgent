"""Tests for OpenCodeBrain adapter — prompt building, init, cancel, error paths."""

import pytest

from iagent_agent.adapter.brain_opencode import OpenCodeBrain
from iagent_agent.adapter.protocol import JobContext


@pytest.fixture
def job_ctx(tmp_path):
    return JobContext(
        job_id="test-job-1",
        command="write a test file",
        workspace_dir=str(tmp_path / "work"),
        inputs_dir=str(tmp_path / "work" / "inputs"),
        output_dir=str(tmp_path / "work" / "output"),
        scratch_dir=str(tmp_path / "work" / "scratch"),
        skill_id=None,
        params={},
        limits={"cpu": 1, "mem_mb": 512, "disk_mb": 5120},
        credentials_injected=False,
    )


@pytest.fixture
def job_ctx_with_skill(tmp_path):
    import os
    skills_dir = tmp_path / ".claude" / "skills" / "my-skill"
    skills_dir.mkdir(parents=True)
    (skills_dir / "SKILL.md").write_text("# My Skill\n")

    old_home = os.environ.get("HOME", "")
    os.environ["HOME"] = str(tmp_path)

    from iagent_agent.adapter import brain_opencode
    brain_opencode.CLAUDE_SKILLS_DIR = skills_dir.parent

    ctx = JobContext(
        job_id="test-job-2",
        command="use the skill",
        workspace_dir=str(tmp_path / "work2"),
        inputs_dir=str(tmp_path / "work2" / "inputs"),
        output_dir=str(tmp_path / "work2" / "output"),
        scratch_dir=str(tmp_path / "work2" / "scratch"),
        skill_id="my-skill",
        params={},
        limits={"cpu": 1, "mem_mb": 512, "disk_mb": 5120},
        credentials_injected=False,
    )
    yield ctx
    if old_home:
        os.environ["HOME"] = old_home


class TestOpenCodeBrain:
    def test_init(self):
        brain = OpenCodeBrain()
        assert brain._procs == {}

    def test_build_prompt_no_skill(self, job_ctx):
        brain = OpenCodeBrain()
        prompt = brain._build_prompt(job_ctx)
        assert "write a test file" in prompt
        assert "default" in prompt
        assert job_ctx.output_dir in prompt

    def test_build_prompt_with_skill(self, job_ctx_with_skill):
        brain = OpenCodeBrain()
        prompt = brain._build_prompt(job_ctx_with_skill)
        assert "my-skill" in prompt
        assert "use the skill" in prompt

    @pytest.mark.asyncio
    async def test_cancel_removes_proc(self):
        brain = OpenCodeBrain()
        brain._procs["job-x"] = None  # type: ignore
        await brain.cancel("job-x")
        assert "job-x" not in brain._procs

    @pytest.mark.asyncio
    async def test_cancel_noop_unknown_job(self):
        brain = OpenCodeBrain()
        await brain.cancel("nonexistent")

    def test_guess_progress_raw(self):
        brain = OpenCodeBrain()
        assert brain._guess_progress("") is None
        assert brain._guess_progress("hello world") is None

    def test_collect_summary_no_file(self, job_ctx):
        brain = OpenCodeBrain()
        summary = brain._collect_summary(job_ctx)
        assert "completed" in summary.lower() or "result" in summary.lower() or summary == ""

    def test_collect_summary_with_file(self, job_ctx):
        import os
        os.makedirs(job_ctx.output_dir, exist_ok=True)
        summary_path = os.path.join(job_ctx.output_dir, "summary.md")
        with open(summary_path, "w") as f:
            f.write("# Done\nAll tasks complete.")

        brain = OpenCodeBrain()
        summary = brain._collect_summary(job_ctx)
        assert "Done" in summary
        assert "All tasks complete" in summary
