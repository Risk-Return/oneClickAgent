# Web Automation (CloakBrowser)

Use the cloakbrowser Python package for all web automation tasks.
The browser binary is pre-installed at `/home/app/.cloakbrowser/`.

## Browser Login Helper (recommended)

For any task that requires user login via VNC, use the reusable helper:

```bash
python3 /home/app/iagent_agent/browser_login.py \
    --url https://TARGET-SITE.com/login \
    --output-dir /work/output \
    --profile-dir /work/profile \
    --wait-secs 120
```

Add `--anti-bot` for sites with captcha protection. The script prints `[BROWSER_READY]`
after page load to signal the web UI.

## Quick Start

```python
from cloakbrowser import launch

browser = launch()
page = browser.new_page()
page.goto("https://target-website.com")
print(page.title())
print("[BROWSER_READY]")  # signal web UI that browser is visible
page.screenshot(path="/work/output/screenshot.png", full_page=True)
browser.close()
```

## Anti-Bot Sites (Captcha Bypass)

```python
from cloakbrowser import launch_persistent_context

ctx = launch_persistent_context(
    "/work/profile",
    headless=False,
    humanize=True,
    human_preset="careful",
    args=["--fingerprint-noise=false", "--fingerprint=42069", "--disable-http2"],
)
page = ctx.new_page()
page.set_viewport_size({"width": 1920, "height": 1080})
page.goto("https://target-site.com", timeout=30000, wait_until="networkidle")
print("[BROWSER_READY]")  # signal web UI that browser is visible
# ... do work ...
ctx.close()
```

## Extract Base64 Images (QR Codes)

```python
import base64

img = page.locator("img.qrcode, .qrcode-img, img[src*='base64']").first
src = img.get_attribute("src")
if src and src.startswith("data:image"):
    b64 = src.split(",", 1)[1]
    with open("/work/output/qr_code.png", "wb") as f:
        f.write(base64.b64decode(b64))
```

## Fill Forms

```python
page.locator("#username").fill("myuser")
page.locator("#password").fill("mypassword")
page.locator("button[type=submit]").click()
page.wait_for_load_state("networkidle")
```

## Wait for Elements

```python
page.wait_for_selector(".target", timeout=10000)
```

## Read Page Content

```python
text = page.inner_text("body")
links = page.locator("a").all()
for link in links[:10]:
    print(link.get_attribute("href"), link.inner_text())
```

## Environment

- Display: `:99` (Xvfb virtual display)
- Profile: `/work/profile` (ephemeral, wiped after job)
- Output: write all results to `/work/output/`
- VNC: available at `127.0.0.1:5901` for viewing browser visually

## Important

- Always call `browser.close()` or `ctx.close()` when done
- Screenshots go to `/work/output/`
- After page load, print `[BROWSER_READY]` to trigger the web UI VNC prompt
- The browser window is visible via VNC on display `:99`
