"""Durable outbox for at-least-once delivery.
Progress/results are written to SQLite outbox before sending;
removed only after cloud ACK. Survives restarts and tunnel drops.
"""
