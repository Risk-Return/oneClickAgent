from __future__ import annotations

import json
import logging
import shutil
from pathlib import Path
from typing import Optional

logger = logging.getLogger(__name__)

CLAUDE_SKILLS_DIR = Path.home() / ".claude" / "skills"


class SkillManager:
    def __init__(self, skills_dir: str):
        self._dir = Path(skills_dir)
        self._dir.mkdir(parents=True, exist_ok=True)
        self._registry: dict[str, dict] = {}
        self._load_registry()

    def _reg_path(self) -> Path:
        return self._dir / "registry.json"

    def _artifacts_dir(self) -> Path:
        p = self._dir / "artifacts"
        p.mkdir(exist_ok=True)
        return p

    def _load_registry(self) -> None:
        p = self._reg_path()
        if p.exists():
            try:
                self._registry = json.loads(p.read_text())
            except (json.JSONDecodeError, OSError):
                self._registry = {}

    def _save_registry(self) -> None:
        self._reg_path().write_text(json.dumps(self._registry, indent=2))

    def _write_claude_skill(self, name: str, manifest: dict, artifact_path: Optional[str]) -> None:
        claude_dir = CLAUDE_SKILLS_DIR / name
        claude_dir.mkdir(parents=True, exist_ok=True)
        skill_md = claude_dir / "SKILL.md"

        # If the artifact is a single .md file, use it directly
        if artifact_path:
            src = Path(artifact_path)
            if src.is_file() and src.suffix == ".md":
                shutil.copy2(src, skill_md)
                logger.info("claude skill written from artifact: ~/.claude/skills/%s/SKILL.md", name)
                return
            if src.is_dir():
                # Look for SKILL.md in the artifact directory
                for f in src.rglob("SKILL.md"):
                    shutil.copy2(f, skill_md)
                    logger.info("claude skill written from artifact: ~/.claude/skills/%s/SKILL.md", name)
                    return

        # Fallback: generate from manifest
        lines = [f"# {name}", ""]
        if isinstance(manifest, dict):
            if manifest.get("description"):
                lines.append(manifest["description"])
                lines.append("")
            lines.append(f"Version: {manifest.get('version', 'unknown')}")
            if manifest.get("entrypoint"):
                lines.append(f"Entrypoint: {manifest['entrypoint']}")
        skill_md.write_text("\n".join(lines))
        logger.info("claude skill generated from manifest: ~/.claude/skills/%s/SKILL.md", name)

    def _remove_claude_skill(self, name: str) -> None:
        claude_dir = CLAUDE_SKILLS_DIR / name
        if claude_dir.exists():
            shutil.rmtree(claude_dir)
            logger.info("claude skill removed: ~/.claude/skills/%s", name)

    def list_skills(self) -> list[dict]:
        return [
            {"skill_id": sid, **info}
            for sid, info in self._registry.items()
        ]

    def get_enabled_skill(self, skill_id: str) -> Optional[dict]:
        s = self._registry.get(skill_id)
        if s and s.get("status") == "enabled":
            return s
        return None

    def install(self, skill_id: str, name: str, version: str, manifest: dict, artifact_path: Optional[str] = None) -> None:
        info = self._registry.get(skill_id, {})
        info["skill_id"] = skill_id
        info["name"] = name
        info["version"] = version
        info["manifest"] = manifest
        info["status"] = "enabled"

        if artifact_path:
            dest = self._artifacts_dir() / skill_id
            if dest.exists():
                shutil.rmtree(dest)
            shutil.copytree(artifact_path, dest)
            info["artifact_path"] = str(dest)

        self._registry[skill_id] = info
        self._save_registry()
        self._write_claude_skill(name, manifest, artifact_path)
        logger.info("skill installed: %s v%s", skill_id, version)

    def update(self, skill_id: str, version: str, manifest: dict, artifact_path: Optional[str] = None) -> None:
        if skill_id not in self._registry:
            raise KeyError(f"skill {skill_id} not found")
        info = self._registry[skill_id]
        info["version"] = version
        info["manifest"] = manifest
        if artifact_path:
            dest = self._artifacts_dir() / skill_id
            if dest.exists():
                shutil.rmtree(dest)
            shutil.copytree(artifact_path, dest)
            info["artifact_path"] = str(dest)
        self._registry[skill_id] = info
        self._save_registry()
        self._write_claude_skill(info["name"], manifest, artifact_path)
        logger.info("skill updated: %s v%s", skill_id, version)

    def disable(self, skill_id: str) -> None:
        if skill_id not in self._registry:
            raise KeyError(f"skill {skill_id} not found")
        self._registry[skill_id]["status"] = "disabled"
        self._save_registry()

    def enable(self, skill_id: str) -> None:
        if skill_id not in self._registry:
            raise KeyError(f"skill {skill_id} not found")
        self._registry[skill_id]["status"] = "enabled"
        self._save_registry()

    def delete(self, skill_id: str) -> None:
        info = self._registry.get(skill_id, {})
        name = info.get("name", "")
        if skill_id in self._registry:
            del self._registry[skill_id]
            self._save_registry()
        artifact = self._artifacts_dir() / skill_id
        if artifact.exists():
            shutil.rmtree(artifact)
        if name:
            self._remove_claude_skill(name)
        logger.info("skill deleted: %s", skill_id)
