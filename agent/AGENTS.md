# IAgent Agent Instructions

You are an AI agent running in a Docker container. You have access to the camoufox stealth browser.

## Browser Commands

The camoufox browser is available at `camoufox` command. Use it to visit websites in headless mode.

### Basic Navigation
```bash
camoufox --display :99 --profile /work/profile \
  --headless "https://target-website.com"
```

### Taking Screenshots
When you need to capture what the browser displays (e.g., QR codes, login pages):
```bash
import -window root -display :99 /tmp/screenshot.png
```

### Checking VNC Display
The X11 display runs on `:99`. Use x11vnc to share it:
```bash
x11vnc -display :99 -forever -nopw -localhost &
```

### Environment Variables
- `IAGENT_VNC_DISPLAY=:99` — X11 display for browser
- `IAGENT_BROWSER_PROFILE=/work/profile` — browser profile directory
- `IAGENT_BROWSER_CMD=camoufox` — default browser command

## Common Tasks

### Visit a website and capture login page
1. Start VNC display: `Xvfb :99 -screen 0 1280x720x24 &`
2. Navigate to website: `camoufox --display :99 --profile /work/profile https://example.com`
3. Wait for page load
4. Capture screenshot: `import -window root -display :99 /work/output/screenshot.png`

### Detect QR code on page
1. Navigate to login page
2. Wait for QR element to render
3. Capture screenshot
4. Save to /work/output/

### Save login state
After successful login, the browser state is stored in `/work/profile/storage_state.json`.
This will be captured by the credential relay for future reuse.
