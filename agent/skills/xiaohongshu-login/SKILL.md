# Xiaohongshu (小红书) Login QR Code

Get the login QR code from xiaohongshu.com so the user can scan it via the VNC viewer.

## Step 1: Bypass Captcha + Reach Login Page

Xiaohongshu blocks headless browsers. Use persistent context + HTTP/1.1 + headed mode:

```python
from cloakbrowser import launch_persistent_context
import time, base64

ctx = launch_persistent_context(
    "/work/profile",
    headless=False,
    humanize=True,
    human_preset="careful",
    args=[
        "--fingerprint-noise=false",
        "--fingerprint=42069",
        "--disable-http2",
    ],
)
page = ctx.new_page()
page.set_viewport_size({"width": 1920, "height": 1080})

page.goto("https://www.xiaohongshu.com", timeout=30000, wait_until="networkidle")
time.sleep(5)

url = page.url
title = page.title()

if "captcha" in url.lower():
    print("BLOCKED: Still on captcha. Try different fingerprint seed.")
    ctx.close()
    exit(1)

print(f"Page: {title}")
```

## Step 2: Navigate to Login Page

```python
page.goto("https://www.xiaohongshu.com/login", timeout=30000, wait_until="networkidle")
time.sleep(3)
```

## Step 3: Extract QR Code

The login QR code is in an `<img class="qrcode-img">` element as a base64 data URI:

```python
qr = page.locator(".qrcode-img").first
src = qr.get_attribute("src")
if src and src.startswith("data:image"):
    b64 = src.split(",", 1)[1]
    with open("/work/output/xiaohongshu_qr.png", "wb") as f:
        f.write(base64.b64decode(b64))
    print("QR code saved to /work/output/xiaohongshu_qr.png")
else:
    print("QR code not found as base64")

page.screenshot(path="/work/output/xiaohongshu_login.png", full_page=True)
```

## Step 4: VNC Display

The browser renders to display `:99`. The user can see the QR code live by:
1. Opening the VNC viewer in the web UI
2. Scanning the QR code with the xiaohongshu mobile app
3. After login, the session is stored in `/work/profile/storage_state.json`

## Step 5: Wait for Login

```python
print("Waiting for user to scan QR code via VNC...")
page.wait_for_url("**/explore**", timeout=120000)  # wait up to 2 min
print("Login successful!")

ctx.close()
```

## Complete Script

```python
from cloakbrowser import launch_persistent_context
import time, base64

ctx = launch_persistent_context(
    "/work/profile",
    headless=False,
    humanize=True,
    human_preset="careful",
    args=[
        "--fingerprint-noise=false",
        "--fingerprint=42069",
        "--disable-http2",
    ],
)
page = ctx.new_page()
page.set_viewport_size({"width": 1920, "height": 1080})

page.goto("https://www.xiaohongshu.com", timeout=30000, wait_until="networkidle")
time.sleep(5)

if "captcha" in page.url.lower():
    print("Captcha blocked — retry with different fingerprint")
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

page.screenshot(path="/work/output/xiaohongshu_login.png", full_page=True)
print("QR code ready — scan via VNC viewer in web UI")
print("Waiting for login...")

page.wait_for_url("**/explore**", timeout=120000)
print("Login successful!")
ctx.close()
```
