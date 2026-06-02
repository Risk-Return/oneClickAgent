import logging
import os
import shutil
from pathlib import Path

logger = logging.getLogger(__name__)

_WIPE_DIRS = ("inputs", "scratch", "output", "profile")

_DEFAULT_QUOTA_MB = 10 * 1024


class Workspace:
    def __init__(self, work_dir: str, quota_mb: int = _DEFAULT_QUOTA_MB):
        self._root = Path(work_dir)
        self._root.mkdir(parents=True, exist_ok=True)
        self._quota_mb = quota_mb

    @property
    def inputs(self) -> str:
        p = self._root / "inputs"
        if not p.exists():
            logger.warning("inputs directory missing — device mount may be absent")
        p.mkdir(exist_ok=True)
        return str(p)

    @property
    def scratch(self) -> str:
        p = self._root / "scratch"
        p.mkdir(exist_ok=True)
        return str(p)

    @property
    def output(self) -> str:
        p = self._root / "output"
        p.mkdir(exist_ok=True)
        return str(p)

    @property
    def root(self) -> Path:
        return self._root

    @property
    def profile(self) -> str:
        p = self._root / "profile"
        p.mkdir(exist_ok=True)
        return str(p)

    def check_quota(self) -> None:
        used_mb = self._disk_usage_mb()
        if used_mb > self._quota_mb:
            raise RuntimeError(
                f"disk quota exceeded: {used_mb:.1f} MB used, {self._quota_mb} MB limit"
            )

    def _disk_usage_mb(self) -> float:
        total = 0
        for name in _WIPE_DIRS:
            d = self._root / name
            if d.exists():
                for dirpath, _dirnames, filenames in os.walk(d):
                    for f in filenames:
                        try:
                            total += os.path.getsize(os.path.join(dirpath, f))
                        except OSError:
                            pass
        return total / (1024 * 1024)

    def wipe(self) -> None:
        for name in _WIPE_DIRS:
            d = self._root / name
            if d.exists():
                shutil.rmtree(d, ignore_errors=True)
                d.mkdir(exist_ok=True)
        logger.info("workspace wiped")
