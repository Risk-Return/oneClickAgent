import os

import uvicorn


def main():
    port = int(os.getenv("IAGENT_AGENT_PORT", "8090"))
    host = os.getenv("IAGENT_AGENT_HOST", "0.0.0.0")
    uvicorn.run(
        "iagent_agent.server:app",
        host=host,
        port=port,
        log_level="info",
        loop="asyncio",
    )


if __name__ == "__main__":
    main()
