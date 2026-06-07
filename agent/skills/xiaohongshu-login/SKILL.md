# Xiaohongshu (小红书) Login QR Code

Get the login QR code from xiaohongshu.com so the user can scan it via the VNC viewer.

## Prerequisites

- User must have the Xiaohongshu mobile app installed
- VNC must be running (display :99, visible in web UI)

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

# Step 1: Bypass captcha
page.goto("https://www.xiaohongshu.com", timeout=30000, wait_until="networkidle")
time.sleep(5)

if "captcha" in page.url.lower():
    print("BLOCKED: Still on captcha. Try a different fingerprint seed.")
    ctx.close()
    exit(1)

# Step 2: Go to login page
page.goto("https://www.xiaohongshu.com/login", timeout=30000, wait_until="networkidle")
time.sleep(3)

# Step 3: Extract QR code (base64 in .qrcode-img)
qr = page.locator(".qrcode-img").first
src = qr.get_attribute("src")
if src and src.startswith("data:image"):
    b64 = src.split(",", 1)[1]
    with open("/work/output/xiaohongshu_qr.png", "wb") as f:
        f.write(base64.b64decode(b64))
    print("QR code saved to /work/output/xiaohongshu_qr.png")
else:
    print("QR code not found as base64 — trying fallback selectors")
    for sel in ["img[src*='base64']", "img[src*='qr']", "[class*=qrcode]"]:
        el = page.locator(sel).first
        if el.count() > 0:
            s = el.get_attribute("src")
            if s and s.startswith("data:image"):
                with open("/work/output/xiaohongshu_qr.png", "wb") as f:
                    f.write(base64.b64decode(s.split(",", 1)[1]))
                print(f"QR extracted via: {sel}")
                break

page.screenshot(path="/work/output/xiaohongshu_login.png", full_page=True)

# Step 4: Wait for scan via VNC
print("QR displayed on VNC — waiting for user to scan...")
print("User: open VNC viewer, scan QR with Xiaohongshu app")

try:
    page.wait_for_url("**/explore**", timeout=120000)
    print("Login successful! Redirected to explore page.")
except Exception:
    print("Login timeout — QR may have expired. Retry with a fresh QR.")

# Step 5: Save session
page.context.storage_state(path="/work/profile/storage_state.json")
print("Session saved to /work/profile/storage_state.json")

ctx.close()
```

## Key Configuration

| Flag | Purpose |
|------|---------|
| `--disable-http2` | **Required** — xiaohongshu challenges fresh HTTP/2 connections with a captcha |
| `headless=False` | **Required** — captcha detects headless mode even with stealth patches |
| `launch_persistent_context` | Keeps cookies across visits, warming prevents re-challenge |
| `humanize=True` + `human_preset="careful"` | Avoids behavioral detection |
| `--fingerprint=NNNNN` | Consistent fingerprint for returning visitor |
| `--fingerprint-noise=false` | Prevents ML tampering detection |

## Troubleshooting

### Still getting captcha?
- Change `--fingerprint=42069` to a different seed
- Ensure `--disable-http2` is present
- Verify `headless=False` is set

### QR code not found?
- XiaoHongShu may change the CSS class — check page source for new selectors
- Try visiting the login page directly: `https://www.xiaohongshu.com/login`

### Login timeout?
- QR codes expire after ~2 minutes — restart the process to get a fresh one
- User must scan promptly from the VNC viewer
