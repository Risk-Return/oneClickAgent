"""Boot reconciliation for the agent pool:
List containers by iagent.pool=true label, adopt matching SQLite records,
create missing idle containers up to desired pool size,
remove surplus containers, recycle any still-BUSY (orphaned after crash) -> IDLE.
"""

import logging

from iagent_device.store.repositories import AgentRepo
from iagent_device.docker.manager import DockerManager

logger = logging.getLogger(__name__)


async def reconcile(manager: DockerManager, repo: AgentRepo, desired_size: int):
    logger.info("reconciling agent pool (desired=%d)", desired_size)

    pool_containers = manager.list_pool_containers()
    container_by_agent: dict[str, dict] = {}
    for c in pool_containers:
        aid = c["agent_id"]
        if aid:
            container_by_agent[aid] = c

    db_agents = {a["agent_id"]: a for a in repo.list_all()}

    # Adopt containers not in DB
    for c in pool_containers:
        aid = c["agent_id"]
        if aid and aid not in db_agents:
            logger.info("adopting container %s (agent %s) not in DB", c["name"], aid)
            repo.upsert(aid, c["name"], manager.image, 0, c["container_id"], status="idle")

    # Recycle orphaned BUSY agents (left over from crash)
    for agent_id, agent in db_agents.items():
        if agent["status"] == "busy":
            logger.warning("recycling orphaned BUSY agent %s", agent_id)
            repo.release(agent_id)

    # Remove DB entries for containers that no longer exist
    for agent_id, agent in db_agents.items():
        cid = agent.get("container_id", "")
        if cid and agent_id not in container_by_agent:
            logger.info("repairing stale DB entry for agent %s (container %s gone)", agent_id, cid)
            repo.update_status(agent_id, "idle", container_id="")

    await manager.ensure_pool(desired_size)
    logger.info("reconciliation complete")
