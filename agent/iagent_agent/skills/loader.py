from __future__ import annotations

import json
import logging
import shutil
from pathlib import Path
from typing import Optional

logger = logging.getLogger(__name__)


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
        if skill_id in self._registry:
            del self._registry[skill_id]
            self._save_registry()
        artifact = self._artifacts_dir() / skill_id
        if artifact.exists():
            shutil.rmtree(artifact)
        logger.info("skill deleted: %s", skill_id)
