[English](README.md)

# oneClickAgent

一个友好的 Web 界面，用于控制运行在本地设备 Docker 容器中的远程 AI Agent，通过公有云网关进行代理。

## 工作原理

```
                         公网                              私有网络（无公网 IP）
                          │                                      │
[浏览器 / Web UI] ─HTTPS/WSS─┤                                   │
                          ▼                                      │
                 ┌─────────────────┐   反向 WSS 隧道               │   ┌──────────────────┐
                 │  Cloud Gateway  │◄───（设备主动拨出）───────────┤   │   Local Device    │
                 │   (Go 服务)     │                              │   │  (Python 服务)    │
                 │  + PostgreSQL   │                              │   │  + SQLite         │
                 └─────────────────┘                              │   └────────┬─────────┘
                                                                  │            │ Docker API
                                                                  │   ┌────────┴─────────┐
                                                                  │   │  Agent Container  │
                                                                  │   │ (HTTP API, Python)│
                                                                  │   └──────────────────┘
```

用户通过 Web UI 提交任务。**Cloud Gateway** 从 Agent 池中选取空闲 Agent，通过反向隧道将命令路由到**本地设备**，由设备分发到 Docker 化的 **Agent 容器**中执行。结果沿原路径返回。任务完成后 Agent 释放回池中。

## 核心特性

- **安全网关** — 公有 Go 服务：TLS、认证、租户隔离。唯一面向互联网的组件。
- **Agent 池** — Agent 是池化资源，按任务临时分配，完成后释放。
- **分级任务队列** — enterprise > pro > free，支持超时和每用户上限。
- **反向隧道** — 设备通过 WSS 主动拨出，无需开放入站端口或公网 IP。
- **云端技能库** — 管理员集中管理技能；用户从可见技能中选择。
- **多渠道支持** — Web 已实现；飞书/QQ 适配器预留接口。
- **组织管理** — 用户分组；按组织授权技能可见性。
- **文件中继** — 通过隧道分块传输，SHA-256 完整性校验。

## 技术栈

| 层级 | 技术 |
|------|------|
| Cloud Gateway | **Go 1.22+**，chi，gorilla/websocket，pgx，golang-jwt，Argon2id |
| Local Device | **Python 3.11+**，FastAPI，uvicorn，websockets，docker-py |
| Agent Container | **Python** 运行时，固定 HTTP API |
| 前端 | **React 18 + TypeScript + Vite**，Tailwind CSS，shadcn/ui |
| 云端数据库 | **PostgreSQL 15+** |
| 本地数据库 | **SQLite**（WAL 模式） |
| 认证 | **JWT**（access + 轮换 refresh），**Argon2id** 哈希 |

## 快速开始

### 前置条件

- Go 1.22+
- PostgreSQL 15+
- Docker（用于 Agent 容器）

### 启动网关

```bash
cd gateway
cp .env.example .env  # 编辑配置
go run ./cmd/gateway
```

必需环境变量：`IAGENT_DB_URL`、`IAGENT_JWT_SECRET`（至少 32 字符）。

## 项目结构

```
oneClickAgent/
├── docs/
│   ├── braionstorm/goal.md          # 项目愿景
│   └── spec/                        # 详细规格文档
├── gateway/                         # Go 云网关
│   ├── cmd/gateway/main.go
│   ├── internal/{api,tunnel,auth,store,relay,pool,model}/
│   └── migrations/
├── device/                          # Python 本地设备服务
├── agent/                           # Agent 容器镜像与运行时
├── web/                             # React 前端
└── deploy/                          # compose 文件、环境变量模板
```

## 规格文档阅读顺序

`00-overview` → `01-architecture` → `05-tunnel-protocol` → `06-data-model` → `07-api` → `02-cloud-gateway` → `03-local-device` → `04-agent-container` → `08-auth-security` → `09-web-ui` → `10-deployment`

## 许可证

待定
