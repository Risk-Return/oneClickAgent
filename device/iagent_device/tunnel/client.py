"""Reverse WSS tunnel client: dial-out to gateway, authenticate with device_token,
send HELLO/STATE_SYNC, maintain heartbeat loop, reconnect with exponential backoff + jitter.
"""
