# LocalSend Hub 代码设计文档

## 1. 项目架构

LocalSend Hub 采用 **双服务解耦架构**，核心文件接收服务与管理面板作为**独立进程**运行，通过共享配置文件通信，实现故障隔离。

```text
                   ┌───────────────────────────────────────┐
                   │       Container / Host System         │
                   │                                       │
  ┌────────────┐   │  ┌──────────────┐    ┌─────────────┐  │
  │ LocalSend  │   │  │ Core Service │    │ Admin Svc   │  │
  │ Clients    │◄──┤  │ Port 53317   │    │ Port 53318  │  │
  │ (Mobile)   │   │  │ (HTTPS)      │    │ (HTTP)      │  │
  └────────────┘   │  │              │    │             │  │
                   │  │ - TLS Gen    │    │ - Web UI    │  │
        ┌──────────┤  │ - File Save  │    │ - API       │  │
        │          │  │ - Multicast  │    │ - Config    │  │
        ▼          │  └──────┬───────┘    └──────┬──────┘  │
  Shared Config File (localsend_config.json)     │         │
  - Config  - Logs  - Device Identity ◄──────────┘         │
  (Admin polls every 2s)                                   │
└──────────────────────────────────────────────────────────┘

关键优势: Admin 崩溃不影响文件接收，两个服务完全隔离
```

## 2. 目录结构

```text
.
├── main.go                     # 核心服务入口
├── cmd/admin/main.go           # 管理服务入口（独立二进制）
├── go.mod                      # Go 模块
├── internal/                   # 私有包
│   ├── state/                  # 状态层
│   │   ├── state.go            # 核心服务状态
│   │   ├── admin_state.go      # 管理服务状态（轮询配置文件）
│   │   ├── shared.go           # 共享类型 (LogEntry, ConfigData)
│   │   ├── admin_provider.go   # 跨进程状态接口
│   │   └── persistence.go      # JSON 配置文件读写
│   ├── discovery/              # UDP 多播广播
│   │   └── multicast.go        # 周期性多播公告
│   ├── core/                   # HTTPS 服务 + LocalSend 协议
│   │   └── server.go           # TLS 证书生成、API 端点
│   └── admin/                  # 管理面板
│       ├── server.go           # HTTP 服务 (go:embed)
│       └── web/                # 前端静态资源
├── Dockerfile                  # 多阶段构建（两个二进制）
├── docker-compose.yml          # Docker Compose 配置
├── entrypoint.sh               # Docker 入口（启动两个服务）
├── localsend-hub               # [忽略] 核心服务二进制
├── localsend-hub-admin         # [忽略] 管理服务二进制
├── received/                   # [忽略] 文件接收目录
└── localsend_config.json       # [忽略] 共享配置文件
```

---

## 3. 核心组件设计

### 3.1 状态管理 (`internal/state`)

**职责**: 系统配置与日志的单一事实来源。

**双服务设计**:
- `State` — 核心服务状态，包含 sessions 等内存数据
- `AdminState` — 管理服务状态，每 2 秒轮询配置文件检测变化
- `AdminStateProvider` — 接口，两个状态类型都实现

**线程安全**: 所有公开方法使用 `sync.Mutex` 保护。

**日志管理**: 环形缓冲，超出 `MaxLogs`（默认 1000）丢弃最旧条目。

**会话管理**: `Sessions` map 存储 `sessionId → {fileId → fileName}`，仅存于核心服务内存，不持久化。

**配置持久化**:
- 核心服务每 15 秒定时保存 + 配置变更立即保存
- 管理服务在配置变更时立即保存，并轮询文件检测核心服务的变更

#### 配置加载流程

```
代码默认值 → 配置文件覆盖 → 环境变量覆盖 (最高优先级)
```

Admin UI 的修改会立即写入配置文件。容器重启时，若未设置环境变量则加载配置文件值。

#### 线程安全规则

- 持有 `mu` 时不调用 `saveToFile()`
- 需要保存时先 `Unlock()` 再调用保存
- `saveToFile()` 是内部方法，调用者不得持有锁

### 3.2 核心服务 (`internal/core`)

**职责**: LocalSend 协议实现与文件接收。

1. **TLS 自动生成**:
   - 启动时生成 RSA-2048 自签名证书（10 年有效期）
   - 计算 SHA-256 指纹作为设备唯一标识

2. **多播广播** (`internal/discovery`):
   - 启动突发: 100ms、500ms、2000ms 各发一次
   - 周期广播: 之后每 5 秒一次

3. **API 处理器**:
   - `Info/Register`: 返回设备 JSON（兼容 v1/v2 路径）
   - `PrepareUpload`: 生成 SessionID 和 Token，注册会话
   - `Upload`: 流式接收文件，文件名冲突自动追加时间戳

### 3.3 管理面板 (`internal/admin`)

**职责**: 可视化运维界面，监听 `0.0.0.0:53318`。

- 前端通过 `//go:embed web/*` 编译进二进制
- **API 端点**:
  | 端点 | 方法 | 说明 |
  |------|------|------|
  | `/api/logs` | GET/DELETE | 查看/清空传输日志 |
  | `/api/identity` | GET/POST | 获取/更新设备身份 |
  | `/api/config` | POST | 更新接收目录 |
  | `/api/files` | GET | 列出已接收文件 |
  | `/files/{name}` | GET | 下载文件 |

---

## 4. 关键设计决策

| 方面 | 决策 | 理由 |
|------|------|------|
| 网络库 | Go `net/http` 标准库 | 无外部依赖，性能极佳 |
| 并发模型 | Goroutines | 轻量级，适合高并发 I/O |
| 服务隔离 | 独立进程 + 共享配置文件 | 故障隔离，Admin 崩溃不影响接收 |
| 数据同步 | JSON 文件轮询 (2s) | 简单可靠，跨进程通信 |
| 证书管理 | 内存中生成 | 启动快，不依赖文件系统 |
| UI 方案 | `go:embed` + Vanilla JS | 二进制极小，无构建步骤 |
| 包可见性 | `internal/` 目录 | 强制模块化 |

---

## 5. 错误处理与安全性

1. **路径遍历防护**: `filepath.Base()` 剥离目录成分
2. **并发安全**: 所有状态方法包含 `Lock/Unlock`
3. **服务隔离**: 管理服务崩溃不影响核心文件接收；核心服务崩溃时 Docker 入口脚本会退出容器
4. **资源限制**: 日志 capped 在 1000 条，防止内存泄漏
5. **网络**: 管理面板绑定 `0.0.0.0`，LAN 可访问，远程管理建议加认证

---

## 6. 扩展性

- 发现协议可扩展为 mDNS/Bonjour
- 日志量增大时可替换为 SQLite
- 管理面板可添加 Basic Auth 中间件
- Docker 支持环境变量覆盖配置

---

## 7. 部署

### 二进制

```bash
./localsend-hub                   # 核心服务
./localsend-hub-admin             # 管理服务（另一个终端）
```

### Docker

```bash
docker compose up -d              # 一键启动两个服务
```

镜像: ~15MB (alpine:3.19 + 两个二进制)，健康检查探测核心服务 HTTPS 端点。
