"""Job dispatcher: receives JOB_DISPATCH from tunnel for a pre-allocated agent,
dispatches to that agent container, relays progress/results back through outbox,
handles cancellation. Signals pool reaper on terminal to release agent back to IDLE.
"""
