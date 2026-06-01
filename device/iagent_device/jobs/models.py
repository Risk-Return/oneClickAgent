"""Local job state models (Pydantic): mirrors cloud job fields for in-flight tracking,
plus device-local fields (workspace_dir, acked_by_cloud).
"""

from pydantic import BaseModel


class JobState(BaseModel):
    job_id: str
    agent_id: str = ""
    user_id: str = ""
    command: str = ""
    skill_id: str = ""
    credential_ids: str = ""
    status: str = "queued"
    percent: int = 0
    workspace_dir: str = ""
    acked_by_cloud: bool = False
