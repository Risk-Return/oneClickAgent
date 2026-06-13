"""Reusable browser login / wait-for-user helper.
Opens a URL in headed cloakbrowser, signals [BROWSER_READY] to the agent,
watches for login (cookies), and enforces a minimum wait before closing.

Usage:
    python3 /home/app/iagent_agent/browser_login.py \
        --url https://www.xiaohongshu.com/login \
        --output-dir /work/output \
        --profile-dir /work/profile \
        --wait-secs 120 \
        --anti-bot
"""

import argparse
import base64
import json
import os
import sys
import time
from pathlib import Path


def parse_args():
    p = argparse.ArgumentParser()
    p.add_argument("--url", required=True)
    p.add_argument("--output-dir", required=True)
    p.add_argument("--profile-dir", required=True)
    p.add_argument("--wait-secs", type=int, default=120)
    p.add_argument("--anti-bot", action="store_true")
    p.add_argument("--fingerprint", default="42069")
    p.add_argument("--viewport-width", type=int, default=1920)
    p.add_argument("--viewport-height", type=int, default=1080)
    p.add_argument("--login-url-pattern", default="")
    p.add_argument("--headless", action="store_true")
    return p.parse_args()


def main():
    args = parse_args()

    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    profile_dir = Path(args.profile_dir)
    profile_dir.mkdir(parents=True, exist_ok=True)

    from cloakbrowser import launch_persistent_context

    launch_kwargs = {}
    launch_kwargs["headless"] = args.headless
    launch_kwargs["humanize"] = True
    launch_kwargs["args"] = [f"--fingerprint={args.fingerprint}"]
    if args.anti_bot:
        launch_kwargs["human_preset"] = "careful"
        launch_kwargs["args"].extend(["--fingerprint-noise=false", "--disable-http2"])

    ctx = launch_persistent_context(str(profile_dir), **launch_kwargs)
    page = ctx.new_page()
    page.set_viewport_size({"width": args.viewport_width, "height": args.viewport_height})

    page.goto(args.url, timeout=30000, wait_until="networkidle")
    time.sleep(3)

    page.screenshot(path=str(output_dir / "initial.png"), full_page=True)

    print(f"[BROWSER_READY] {args.url}")
    print(f"Browser is ready on VNC display :99. Waiting up to {args.wait_secs}s for login...")
    print(f"Open the web UI VNC viewer to log in.")

    deadline = time.time() + args.wait_secs
    login_detected = False
    while time.time() < deadline:
        remaining = int(deadline - time.time())
        cookies = page.context.cookies()

        # Check for auth-like cookies (session, token, etc.)
        auth_cookies = [
            c for c in cookies
            if c["name"].lower() in ("web_session", "session", "a1", "token",
                                      "auth", "PHPSESSID", "JSESSIONID",
                                      "connect.sid", "sessionid", "sid",
                                      "x-user-session", "user_session")
            or "session" in c["name"].lower()
            or "token" in c["name"].lower()
            or "auth" in c["name"].lower()
        ]

        if auth_cookies and not login_detected:
            login_detected = True
            print(f"Login detected at +{args.wait_secs - remaining}s. "
                  f"Still waiting {remaining}s before closing...")
            page.screenshot(path=str(output_dir / "logged_in.png"), full_page=True)
            # Save storage state immediately on login detection
            page.context.storage_state(path=str(profile_dir / "storage_state.json"))
            print(f"Session saved to {profile_dir}/storage_state.json")

        # Check URL pattern if specified
        if not login_detected and args.login_url_pattern:
            try:
                if args.login_url_pattern in page.url:
                    login_detected = True
                    print(f"Login detected via URL pattern at +{args.wait_secs - remaining}s. "
                          f"Still waiting {remaining}s before closing...")
            except Exception:
                pass

        if remaining % 15 == 0 and remaining > 0:
            print(f"[{remaining}s] waiting...")

        time.sleep(1)

    if not login_detected:
        page.screenshot(path=str(output_dir / "login_timeout.png"), full_page=True)
        print(f"Login timeout after {args.wait_secs}s. Saving current state anyway.")

    page.context.storage_state(path=str(profile_dir / "storage_state.json"))
    cookie_count = len(page.context.cookies())
    print(f"Final: {cookie_count} cookies saved to {profile_dir}/storage_state.json")
    ctx.close()


if __name__ == "__main__":
    main()
