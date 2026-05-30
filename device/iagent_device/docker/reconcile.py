"""Boot reconciliation for the agent pool:
List containers by iagent.pool=true label, adopt matching SQLite records,
create missing idle containers up to desired pool size,
remove surplus containers, recycle any still-BUSY (orphaned after crash) → IDLE.
"""
