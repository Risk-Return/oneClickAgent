# Goal
make a friendly user interface to allow users to control an AI agent through a web UI in a remote environment

# key features
- safe Gateway
- multi-channel support, for example, web, feishu, qq, etc. only develop for web now, leave API for other channels
- multi-agent support, per docker container for each agent
- multi-local-device support
- web UI (default): command interface + file upload + skill management + button control (send job, cancel job, query job status) + result display. no terminal access, no detailed agent information feedback, only contain progress information
- user registration and authentication
- can manage multiple agents for each user, default to 1

# gateway flow 
example of web UI:
```
[User Input] --> [cloud Gateway] --> [local device] --> [Agent Container] --> [Result] --> [cloud Gateway] --> [User Output]
```
normally, cloud gateway is deployed on a remote server with public ip address, and local device is deployed on a local machine without public ip address.

# local device management
local device should be able to:
- register itself to cloud gateway
- receive commands from cloud gateway
- send results to cloud gateway
- manage multiple agents with docker containers

# user management (customer)
user (single or group) should be able to:
- register and login
- can register multiple agents, default to 1.
- send commands to agents
- receive results from agents

# agent container management
agent container should be able to:
- receive commands from local device
- receive user data from local device (such as upload files, default files or data sheet)
- remove user data after job is done (after result is sent to local device)
- send results to local device
- manage skills for agents
- special tag for agent container: specialized for specific tasks
- limited resources: cpu, memory, disk space. default: 2 cpu, 4GB memory, 10GB disk space
- live monitoring: monitor agent container status, resources usage, etc.
- health check: check agent container status, restart if necessary
- recovery: recover from unexpected shutdown

# user data management
user data should be stored in a database, including:
- user information: username, email, password, etc.
- agent information: agent name, agent description, agent skills, etc.
- job information: job id, job status, job result, etc.
- file information: file name, file path, file size, etc.

# local device data management
local device data should be stored in a database, including:
- local device information: device name, device description, device status, etc.
- agent information: agent name, agent description, agent skills, etc.
- job information: job id, job status, job result, etc.
- file information: file name, file path, file size, etc.

# cloud gateway data management
cloud gateway data should be stored in a database, including:
- user information: username, email, password, etc.
- local device information: device name, device description, device status, etc.
- agent information: agent name, agent description, agent skills, etc.
- job information: job id, job status, job result, etc.
- file information: file name, file path, file size, etc.

# tunnel management
tunnel should be able to:
- create tunnel between cloud gateway and local device
- manage tunnel status
- manage tunnel data transfer
- manage tunnel security
- manage tunnel recovery

# development
- local device development : one installation for managing multiple agents (docker containers) , tunnels and local database for device data
- cloud gateway development : one installation for managing multiple local devices, agents, tunnels, user data and web interface

# skill management
skill should be able to:
- install skills for all agents on a local device by command from cloud gateway by admin
- disable skills for all agents on a local device by command from cloud gateway by admin
- update skills for all agents on a local device by command from cloud gateway by admin
- delete skills for all agents on a local device by command from cloud gateway by admin
- cloud skill vault: store skills for agents on cloud gateway and dispatch to local devices
- user can select skills for agents on cloud gateway