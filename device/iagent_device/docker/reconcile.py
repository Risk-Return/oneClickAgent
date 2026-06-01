"""Boot reconciliation for the agent pool:
List containers by iagent.pool=true label, adopt matching SQLite records,
create missing idle containers up to desired pool size,
remove surplus containers, recycle any still-BUSY (orphaned after crash) → IDLE.
"""

import logging

from iagent_device.store.repositories import AgentRepo
from iagent_device.docker.manager import DockerManager

logger = logging.getLogger(__name__)


async def reconcile(manager: DockerManager, repo: AgentRepo, desired_size: int):
    logger.info("reconciling agent pool (desired=%d)", desired_size)

    # Recycle orphaned BUSY agents (left over from crash)
    for agent in repo.list_all():
        if agent["status"] == "busy":
            logger.warning("recycling orphaned BUSY agent %s", agent["agent_id"])
            repo.release(agent["agent_id"])

    await manager.ensure_pool(desired_size)
    logger.info("reconciliation complete")
