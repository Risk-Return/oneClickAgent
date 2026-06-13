# Xiaohongshu (小红书) Login

Get the login QR code from xiaohongshu.com, display it on VNC for the user to scan,
and wait for successful login (up to 120s). The browser-ready signal is sent automatically.

## Quick Start

Use the reusable login helper:

```bash
python3 /home/app/iagent_agent/browser_login.py \
    --url https://www.xiaohongshu.com/login \
    --output-dir /work/output \
    --profile-dir /work/profile \
    --wait-secs 120 \
    --anti-bot
```

This launches the browser in headed mode on display :99, prints `[BROWSER_READY]`
to trigger the web UI signal, and waits up to 120s for auth cookies to appear.

## What happens

1. Browser opens Xiaohongshu login page (headed, anti-bot config)
2. `[BROWSER_READY]` is printed → web UI shows "Open VNC to view" notification
3. Waits up to 120s, checking cookies every second
4. When auth cookies (a1, web_session, etc.) appear → login detected
5. Continues waiting until the full 120s elapses so the user has time to see
6. Saves storage_state.json and screenshots to output dir

## If the login page has a captcha

Try with a different fingerprint:

```bash
python3 /home/app/iagent_agent/browser_login.py \
    --url https://www.xiaohongshu.com/login \
    --output-dir /work/output \
    --profile-dir /work/profile \
    --wait-secs 120 \
    --anti-bot \
    --fingerprint 12345
```

## If you need the QR code image specifically

```python
from cloakbrowser import launch_persistent_context
import base64, time

ctx = launch_persistent_context(
    "/work/profile",
    headless=False,
    humanize=True,
    human_preset="careful",
    args=["--fingerprint-noise=false", "--fingerprint=42069", "--disable-http2"],
)
page = ctx.new_page()
page.set_viewport_size({"width": 1920, "height": 1080})

page.goto("https://www.xiaohongshu.com", timeout=30000, wait_until="networkidle")
time.sleep(5)

if "captcha" in page.url.lower():
    print("BLOCKED: Still on captcha. Try a different fingerprint seed.")
    ctx.close()
    exit(1)

page.goto("https://www.xiaohongshu.com/login", timeout=30000, wait_until="networkidle")
time.sleep(3)

qr = page.locator(".qrcode-img").first
src = qr.get_attribute("src")
if src and src.startswith("data:image"):
    b64 = src.split(",", 1)[1]
    with open("/work/output/xiaohongshu_qr.png", "wb") as f:
        f.write(base64.b64decode(b64))
    print("QR code saved")

page.screenshot(path="/work/output/xiaohongshu_login.png", full_page=True)
print("[BROWSER_READY]")

# Wait for login (the helper script approach above is preferred)
ctx.close()
```

## Output files

- `/work/output/initial.png` — screenshot after page load
- `/work/output/logged_in.png` — screenshot after login detected
- `/work/output/login_timeout.png` — screenshot if login timed out
- `/work/profile/storage_state.json` — cookies saved for reuse

## Note

The `print("[BROWSER_READY]")` marker triggers the web UI to show an "Open VNC" button.
Always emit this AFTER the page has loaded but BEFORE waiting for login.
