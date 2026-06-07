# IAgent Agent Instructions

You are an AI agent running in a Docker container. You have access to stealth browsers for web automation.

## Browser Selection

Two browsers are available:
- **camoufox** — Firefox-based stealth browser (CLI-driven)
- **cloakbrowser** — Chromium-based stealth browser (Python API)

Check which browser is active: `echo $IAGENT_BROWSER_CMD`

---

## Camoufox (CLI Browser)

The camoufox browser is available at `camoufox` command. Use it to visit websites in headless mode.

### Basic Navigation
```bash
camoufox --display :99 --profile /work/profile \
  --headless "https://target-website.com"
```

### Taking Screenshots
```bash
import -window root -display :99 /tmp/screenshot.png
```

---

## CloakBrowser (Chromium Python API)

CloakBrowser is a Playwright-compatible stealth Chromium with 58 C++ source-level fingerprint patches.
Use it via Python scripts — the `cloakbrowser` package is pre-installed.

### Quick Start Script

Write and run Python scripts using cloakbrowser:

```python
from cloakbrowser import launch

browser = launch()
page = browser.new_page()
page.goto("https://example.com")
print(page.title())
browser.close()
```

### Basic Navigation

```python
from cloakbrowser import launch

# Basic — headless, random fingerprint
browser = launch()
page = browser.new_page()
page.goto("https://target-website.com")

# Get page content
content = page.content()
title = page.title()

# Screenshot
page.screenshot(path="/work/output/screenshot.png")

browser.close()
```

### Headed Mode (for VNC / login pages)

When VNC is active (display :99), run headed for visual interaction:

```python
from cloakbrowser import launch

browser = launch(headless=False)
page = browser.new_page()
page.goto("https://site-with-login.com")
# User can see and interact via VNC
browser.close()
```

### Taking Screenshots in Headed Mode

```python
page.screenshot(path="/work/output/page.png", full_page=True)
```

Or with ImageMagick for the full display:
```bash
import -window root -display :99 /work/output/screenshot.png
```

### Human-like Behavior

Enable human-like mouse, keyboard, and scroll patterns:

```python
browser = launch(humanize=True)
page = browser.new_page()
page.goto("https://example.com")
page.locator("#email").fill("user@example.com")
page.locator("button[type=submit]").click()
browser.close()
```

Presets: `"default"` (normal) or `"careful"` (slower, more deliberate):
```python
browser = launch(humanize=True, human_preset="careful")
```

### Persistent Profile (Stay Logged In)

```python
from cloakbrowser import launch_persistent_context

ctx = launch_persistent_context("/work/profile", headless=False)
page = ctx.new_page()
page.goto("https://site.com")
# Cookies and localStorage persist across runs
ctx.close()
```

### With Proxy

```python
browser = launch(
    proxy="http://user:pass@proxy:8080",
    geoip=True,       # auto-detect timezone/locale from proxy IP
    headless=False,
    humanize=True,
)
```

### Deterministic Fingerprint (Returning Visitor)

```python
browser = launch(args=["--fingerprint=12345"])
# Same seed = same fingerprint every launch
```

### Wait for Elements

```python
page.goto("https://example.com")
page.wait_for_selector(".target-element", timeout=10000)
page.locator(".target-element").click()
```

### Fill Forms

```python
page.locator("#username").fill("myuser")
page.locator("#password").fill("mypassword")
page.locator("button[type=submit]").click()
page.wait_for_load_state("networkidle")
```

### Read Page Content

```python
text = page.inner_text("body")
links = page.locator("a").all()
for link in links:
    print(link.get_attribute("href"))
```

### Storage State (Cookies / Login Export)

CloakBrowser auto-saves storage state to `/work/profile/storage_state.json`.
To read it from Python:

```python
import json
with open("/work/profile/storage_state.json") as f:
    state = json.load(f)
```

### Full Anti-Bot Config

For sites with aggressive bot detection (DataDome, Cloudflare, Kasada):

```python
browser = launch(
    proxy="http://residential-proxy:port",
    geoip=True,
    headless=False,
    humanize=True,
    human_preset="careful",
    args=[
        "--fingerprint-noise=false",
        "--fingerprint-screen-width=1920",
        "--fingerprint-screen-height=1080",
    ],
)
```

### Important Notes

- Always call `browser.close()` when done to free resources
- Use `page.locator(selector)` not `page.query_selector()` for humanize to work
- Storage state format is Playwright-compatible (same as camoufox)
- The binary is pre-downloaded at `/home/app/.cloakbrowser/`

---

## Screenshots (Both Browsers)

```bash
import -window root -display :99 /work/output/screenshot.png
```

## VNC Display

The X11 display runs on `:99`. When VNC is enabled, x11vnc shares it on loopback.

## Environment Variables

- `IAGENT_VNC_DISPLAY=:99` — X11 display for browser
- `IAGENT_BROWSER_PROFILE=/work/profile` — browser profile directory
- `IAGENT_BROWSER_CMD=camoufox|cloakbrowser` — active browser
- `CLOAKBROWSER_CACHE_DIR=/home/app/.cloakbrowser` — cloakbrowser binary cache

## Common Tasks

### Visit a website and capture login page (camoufox)
1. Start VNC display: `Xvfb :99 -screen 0 1280x720x24 &`
2. Navigate: `camoufox --display :99 --profile /work/profile https://example.com`
3. Wait for page load
4. Screenshot: `import -window root -display :99 /work/output/screenshot.png`

### Visit a website and capture login page (cloakbrowser)
1. Start VNC: `Xvfb :99 -screen 0 1280x720x24 &`
2. Write + run a Python script using `from cloakbrowser import launch`
3. Navigate, wait, screenshot via `page.screenshot(path=...)`

### Detect QR code on page
1. Navigate to login page
2. Wait for QR element to render
3. Capture screenshot
4. Save to /work/output/

### Save login state
After successful login, the browser state is stored in `/work/profile/storage_state.json`.
This will be captured by the credential relay for future reuse.

---

## Site-Specific Patterns

### Xiaohongshu (小红书) Login

Xiaohongshu uses a QR code login system. The site blocks headless browsers with a captcha.
Use persistent context + HTTP/1.1 + headed mode to bypass.

```python
from cloakbrowser import launch_persistent_context
import time

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

title = page.title()
url = page.url
print(f"Title: {title}\nURL: {url}")

if "captcha" in url.lower():
    print("Captcha blocked — try again with a different fingerprint seed")
elif "login" in url.lower():
    print("On login page")

page.screenshot(path="/work/output/xiaohongshu_login.png", full_page=True)
print("Login page screenshot saved")

ctx.close()
```

**QR Code Extraction:** Xiaohongshu login QR codes are rendered as base64 `<img>` with class `.qrcode-img`. In headed mode via VNC, the QR code is displayed directly in the browser window for the user to scan. To extract and save separately:

```python
import base64
qr = page.locator(".qrcode-img").first
src = qr.get_attribute("src")
if src and src.startswith("data:image"):
    b64 = src.split(",", 1)[1]
    with open("/work/output/xiaohongshu_qr.png", "wb") as f:
        f.write(base64.b64decode(b64))
```

**Key flags for xiaohongshu:**
- `--disable-http2` — required to bypass captcha challenge
- `headless=False` — captcha detects headless mode
- `launch_persistent_context` — keeps cookies between visits
- `humanize=True` + `human_preset="careful"` — avoids behavioral detection
- `--fingerprint=NNNNN` — consistent fingerprint for returning visitor
