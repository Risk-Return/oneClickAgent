#!/bin/bash
# VNC stack entrypoint scripts for the agent container.
# These are called on-demand by the iagent_agent runtime (not used at boot).
set -e

case "${1:-}" in
  start)
    Xvfb "${IAGENT_VNC_DISPLAY:-:99}" -screen 0 1280x720x24 &
    sleep 1
    x11vnc \
      -display "${IAGENT_VNC_DISPLAY:-:99}" \
      -rfbport "${IAGENT_VNC_PORT:-5901}" \
      -localhost \
      -passwd "${IAGENT_VNC_PASSWORD:-default}" \
      -nopw \
      -shared \
      -forever \
      -loop &
    echo "VNC started on 127.0.0.1:${IAGENT_VNC_PORT:-5901}"
    ;;

  stop)
    pkill x11vnc || true
    pkill Xvfb || true
    echo "VNC stopped"
    ;;

  *)
    echo "Usage: $0 {start|stop}"
    exit 1
    ;;
esac
