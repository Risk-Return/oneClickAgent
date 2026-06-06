# Drain agent containers doesn't stop Docker on local device

**Date:** 2026-06-06  
**Status:** Fixed on gateway — device code update required

## Root Cause

When admin clicks **Drain** in the Agent Pool page, the gateway only deleted the agent's database record. It never sent an `AGENT_ACTION` frame to the device, so the actual Docker container kept running.

## Fix Applied (Gateway)

The gateway now sends `AGENT_ACTION {"agent_id": "...", "action": "drain"}` before deleting the agent record. Committed to `main` as `7ee8374` and deployed to `deepwitai.cn`.

## Device-Side Code Update Required

The device's `_handle_agent_action` handler was updated in `device/iagent_device/__main__.py` to handle the `"drain"` action:

```python
async def _handle_agent_action(payload: dict, docker_mgr: DockerManager, outbox: Outbox):
    agent_id = payload.get("agent_id", "")
    action = payload.get("action", "")
    if not agent_id:
        return
    if action == "drain":
        logger.info("draining agent %s", agent_id)
        agent = docker_mgr.repo.get_by_id(agent_id)
        if agent:
            docker_mgr._remove_container(agent)
        docker_mgr.repo.delete(agent_id)
    elif action == "restart":
        docker_mgr._restart_container(agent_id)
```

The commit `7ee8374` is already on `main`.

## Steps on the Local Device

```bash
cd /path/to/oneClickAgent
git pull origin main
source device/venv/bin/activate
pip install -e device/

# Restart the device
sudo systemctl restart iagent-device
# or: Ctrl+C and re-run: iagent-device run
```

## Verification

1. After restart, go to Agent Pool page
2. Click **Drain** on any idle agent
3. On the device machine, run `docker ps --filter 'label=iagent.pool=true'`
4. The drained agent's container should be removed
5. The agent should disappear from the Agent Pool within seconds
