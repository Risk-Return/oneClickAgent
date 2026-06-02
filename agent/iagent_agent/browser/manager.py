from __future__ import annotations

import json
import logging
import os
import secrets
import subprocess
import time
from pathlib import Path
from typing import Optional

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

    def start(self) -> str:
        if self._running:
            return self._rfb_password or ""

        self._rfb_password = secrets.token_urlsafe(16)
        self._xvfb = subprocess.Popen(
            ["Xvfb", self._display, "-screen", "0", "1280x720x24"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

        time.sleep(1)

        self._x11vnc = subprocess.Popen(
            [
                "x11vnc",
                "-display", self._display,
                "-rfbport", str(self._rfb_port),
                "-localhost",
                "-passwd", self._rfb_password,
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
        for proc in (self._x11vnc, self._xvfb):
            if proc and proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    proc.kill()
        self._x11vnc = None
        self._xvfb = None
        self._running = False
        self._rfb_password = None
        logger.info("VNC stack stopped")
