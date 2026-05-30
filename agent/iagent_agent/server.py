"""FastAPI app with all HTTP routes (healthz, status, jobs, skills).
Validates single-job concurrency (409 BUSY), relays progress to device callback,
and enforces one-skill-per-job constraint.
"""
