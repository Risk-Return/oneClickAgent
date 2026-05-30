"""File stager: receives FILE_PUSH_*/CHUNK/END, verifies sha256,
writes to per-job workspace, mounts inputs read-only into agent container,
and cleans up workspace on job terminal state.
"""
