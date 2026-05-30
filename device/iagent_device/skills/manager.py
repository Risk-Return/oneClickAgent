"""Device-wide skill manager: receives SKILL_DISPATCH_* chunked packages,
caches locally, applies SKILL_ACTION (device-wide: install/disable/update/delete
to all agents; per-agent: enable/disable), reports SKILL_STATE,
and reconciles against SKILL_SYNC on reconnect.
"""
