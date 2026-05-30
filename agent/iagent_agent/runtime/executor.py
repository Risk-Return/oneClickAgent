"""Job executor: spawns brain.run() as async task, collects progress events,
handles cancellation signal, transitions job state, and triggers workspace cleanup.
"""
