import logging
import shutil
from pathlib import Path

logger = logging.getLogger(__name__)

_WIPE_DIRS = ("inputs", "scratch", "output", "profile")


class Workspace:
    def __init__(self, work_dir: str):
        self._root = Path(work_dir)
        self._root.mkdir(parents=True, exist_ok=True)

    @property
    def inputs(self) -> str:
        p = self._root / "inputs"
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

    def wipe(self) -> None:
        for name in _WIPE_DIRS:
            d = self._root / name
            if d.exists():
                shutil.rmtree(d, ignore_errors=True)
                d.mkdir(exist_ok=True)
        logger.info("workspace wiped")
