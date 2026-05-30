"""AgentBrain protocol (typing.Protocol): the pluggable "brain" interface.
brain.run(ctx, emit) → JobResult, brain.cancel(job_id).
Concrete implementations are swappable per agent type.
"""
