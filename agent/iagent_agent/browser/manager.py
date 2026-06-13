from __future__ import annotations

import json
import logging
import os
import secrets
import subprocess
import time
from pathlib import Path
from typing import Any, Optional

logger = logging.getLogger(__name__)


class BrowserManager:
    def __init__(
        self,
        browser_cmd: str = "camoufox",
        display: str = ":99",
        profile_dir: str = "/work/profile",
    ):
        self._browser_cmd = browser_cmd
        self._display = display
        self._profile_dir = profile_dir
        self._process: Optional[subprocess.Popen[bytes]] = None

    @property
    def profile_dir(self) -> str:
        return self._profile_dir

    def set_profile_dir(self, path: str) -> None:
        self._profile_dir = path

    def inject_state(self, storage_state: dict) -> None:
        profile = Path(self._profile_dir)
        profile.mkdir(parents=True, exist_ok=True)
        state_file = profile / "storage_state.json"
        state_file.write_text(json.dumps(storage_state, indent=2))
        logger.info("storage-state injected into %s", state_file)

    def export_state(self, origin: str = "") -> dict:
        profile = Path(self._profile_dir)
        state_file = profile / "storage_state.json"
        if not state_file.exists():
            return {}
        try:
            full_state = json.loads(state_file.read_text())
        except (json.JSONDecodeError, OSError):
            return {}

        if not origin:
            return full_state

        origin = origin.rstrip("/")
        filtered_cookies = [
            c for c in full_state.get("cookies", [])
            if c.get("domain", "") in origin or origin.endswith(c.get("domain", ""))
        ]
        filtered_origins = [
            o for o in full_state.get("origins", [])
            if o.get("origin", "").rstrip("/") == origin
        ]
        return {"cookies": filtered_cookies, "origins": filtered_origins}

    def launch_headless(self) -> None:
        env = os.environ.copy()
        env["DISPLAY"] = self._display
        env["HOME"] = str(Path(self._profile_dir).parent)
        Path(self._profile_dir).mkdir(parents=True, exist_ok=True)
        self._process = subprocess.Popen(
            [self._browser_cmd, "--profile", self._profile_dir, "--headless"],
            env=env,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        logger.info("browser launched pid=%s display=%s", self._process.pid, self._display)

    def kill(self) -> None:
        if self._process and self._process.poll() is None:
            self._process.terminate()
            try:
                self._process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self._process.kill()
            logger.info("browser killed pid=%s", self._process.pid)
        self._process = None


class CloakBrowserManager:
    def __init__(
        self,
        display: str = ":99",
        profile_dir: str = "/work/profile",
    ):
        self._display = display
        self._profile_dir = profile_dir
        self._browser: Any = None
        self._page: Any = None

    @property
    def profile_dir(self) -> str:
        return self._profile_dir

    def set_profile_dir(self, path: str) -> None:
        self._profile_dir = path

    def _storage_state_path(self) -> Path:
        return Path(self._profile_dir) / "storage_state.json"

    def inject_state(self, storage_state: dict) -> None:
        profile = Path(self._profile_dir)
        profile.mkdir(parents=True, exist_ok=True)
        self._storage_state_path().write_text(json.dumps(storage_state, indent=2))
        logger.info("storage-state injected into %s", self._storage_state_path())

    def export_state(self, origin: str = "") -> dict:
        state_file = self._storage_state_path()
        if not state_file.exists():
            return {}
        try:
            full_state = json.loads(state_file.read_text())
        except (json.JSONDecodeError, OSError):
            return {}

        if not origin:
            return full_state

        origin = origin.rstrip("/")
        filtered_cookies = [
            c for c in full_state.get("cookies", [])
            if c.get("domain", "") in origin or origin.endswith(c.get("domain", ""))
        ]
        filtered_origins = [
            o for o in full_state.get("origins", [])
            if o.get("origin", "").rstrip("/") == origin
        ]
        return {"cookies": filtered_cookies, "origins": filtered_origins}

    def launch_headless(self) -> None:
        os.environ["DISPLAY"] = self._display
        profile = Path(self._profile_dir)
        profile.mkdir(parents=True, exist_ok=True)

        from cloakbrowser import launch  # type: ignore[import-untyped]

        launch_kwargs: dict = {
            "headless": False,
            "humanize": True,
            "args": [
                "--fingerprint-platform=linux",
            ],
        }

        state_file = self._storage_state_path()
        if state_file.exists():
            try:
                stored = json.loads(state_file.read_text())
                if stored:
                    launch_kwargs["storage_state"] = stored
            except (json.JSONDecodeError, OSError):
                pass

        self._browser = launch(**launch_kwargs)
        self._page = self._browser.new_page()
        logger.info("cloakbrowser launched display=%s profile=%s", self._display, self._profile_dir)

    def kill(self) -> None:
        if self._browser is not None:
            try:
                self._browser.close()
            except Exception:
                pass
            self._browser = None
            self._page = None
            logger.info("cloakbrowser killed")

    def save_storage_state(self) -> None:
        if self._page is not None and self._browser is not None:
            try:
                state = self._page.context.storage_state()
                self._storage_state_path().write_text(json.dumps(state, indent=2))
                logger.info("cloakbrowser storage state saved")
            except Exception as exc:
                logger.warning("failed to save cloakbrowser storage state: %s", exc)


class VNCStack:
    def __init__(
        self,
        display: str = ":99",
        rfb_port: int = 5901,
    ):
        self._display = display
        self._rfb_port = rfb_port
        self._xvfb: Optional[subprocess.Popen[bytes]] = None
        self._x11vnc: Optional[subprocess.Popen[bytes]] = None
        self._rfb_password: Optional[str] = None
        self._running = False

    @property
    def running(self) -> bool:
        return self._running

    @property
    def rfb_port(self) -> int:
        return self._rfb_port

    @property
    def rfb_password(self) -> Optional[str]:
        return self._rfb_password

    def ensure_xvfb(self) -> None:
        if self._xvfb is not None:
            if self._xvfb.poll() is None:
                return
            self._xvfb.wait()
        self._xvfb = subprocess.Popen(
            ["Xvfb", self._display, "-screen", "0", "1280x720x24"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        time.sleep(1)
        logger.info("Xvfb started on display=%s", self._display)

    def start(self) -> str:
        if self._running:
            return self._rfb_password or ""

        self.ensure_xvfb()

        self._x11vnc = subprocess.Popen(
            [
                "x11vnc",
                "-display", self._display,
                "-rfbport", str(self._rfb_port),
                "-nopw",
                "-shared",
                "-forever",
                "-loop",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(1)
        self._running = True
        logger.info(
            "VNC stack started display=%s port=%d",
            self._display,
            self._rfb_port,
        )
        return self._rfb_password

    def stop(self) -> None:
        if self._x11vnc and self._x11vnc.poll() is None:
            self._x11vnc.terminate()
            try:
                self._x11vnc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self._x11vnc.kill()
        self._x11vnc = None
        self._running = False
        self._rfb_password = None
        logger.info("VNC stack stopped")
