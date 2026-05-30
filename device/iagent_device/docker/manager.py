"""Docker agent pool manager via docker-py:
- Create N idle containers (pull image, resource limits, security hardening, labels)
- Start/stop/restart/remove individual agents
- Health checks per agent, recovery with max_restarts cap
- Pool reaper: recycle agents after job completion (clear workspace → IDLE)
- Scale up/down the pool to match desired size
"""
