# LocalSend Hub 接收方功能需求文档

## 1. 项目概述

LocalSend Hub 是一个基于 **LocalSend 协议 v2** 的接收方（Receiver）实现。LocalSend 是一个局域网文件共享协议，通过 UDP 组播进行设备发现，通过 HTTP(S) 进行文件传输。

**当前实现**: 使用 **Go 1.21+** 编写的生产级应用，包含核心文件接收服务和管理面板，两个服务作为**独立进程**运行。

---

## 2. 基本要求

| 编号 | 要求 | 说明 |
|------|------|------|
| 1 | **基于 Go 1.21+ 实现** | 使用标准库优先，最小化外部依赖 |
| 2 | **高内聚，低耦合** | `internal/` 目录封装模块，职责单一 |
| 3 | **生产级代码** | 完整错误处理、资源管理、日志记录 |
| 4 | **线程安全** | 共享状态使用 `sync.Mutex` 保护 |
| 5 | **双二进制部署** | 核心服务与管理服务独立运行，故障隔离 |

---

## 3. 架构设计

### 3.1 双服务架构

| 服务 | 端口 | 协议 | 用途 |
|------|------|------|------|
| Core Service | `53317` | HTTPS | LocalSend 核心服务（发现 + 文件接收） |
| Admin Service | `53318` | HTTP | 管理面板（日志查看 + 配置管理） |

两个服务作为独立进程运行，通过共享 JSON 配置文件 (`localsend_config.json`) 同步状态。管理服务每 2 秒轮询配置文件检测变化。

### 3.2 项目结构

```text
.
├── main.go                     # 核心服务入口
├── cmd/admin/main.go           # 管理服务入口
├── internal/
│   ├── state/                  # 全局配置、日志、线程安全状态
│   │   ├── state.go            # 核心服务状态
│   │   ├── admin_state.go      # 管理服务状态
│   │   ├── shared.go           # 共享类型
│   │   ├── admin_provider.go   # 跨进程状态接口
│   │   └── persistence.go      # JSON 配置文件读写
│   ├── discovery/              # UDP 组播广播
│   │   └── multicast.go        # 周期性多播公告
│   ├── core/                   # HTTPS 服务器 + LocalSend 协议处理
│   │   └── server.go           # TLS 证书生成、API 端点
│   └── admin/                  # 管理面板服务器 + 嵌入式 Web UI
│       ├── server.go           # Admin HTTP 服务
│       └── web/                # 静态资源
├── Dockerfile                  # 多阶段 Docker 构建
├── docker-compose.yml          # Docker Compose 配置
├── entrypoint.sh               # Docker 入口脚本（启动两个服务）
└── localsend_config.json       # 持久化配置文件（自动生成，共享）
```

---

## 4. 默认配置

### 4.1 UDP 组播

| 参数 | 值 | 说明 |
|------|-----|------|
| 组播地址 | `224.0.0.167` | 使用 `224.0.0.0/24` 范围 |
| 端口 | `53317` | 与 HTTPS 端口一致 |

### 4.2 服务端口

| 参数 | 值 | 说明 |
|------|-----|------|
| Core Port | `53317` | HTTPS，可配置 |
| Admin Port | `53318` | HTTP，绑定 0.0.0.0 支持 LAN 访问 |

### 4.3 设备信息

| 参数 | 默认值 | 说明 |
|------|--------|------|
| 设备别名 | `LocalSend Hub` | 可配置 |
| 设备型号 | `LocalSend Hub Server` | 可配置 |
| 设备类型 | `server` | 无界面接收服务 |

### 4.4 文件存储

| 参数 | 默认值 | 说明 |
|------|--------|------|
| 接收目录 | `./received` | 自动创建，可配置 |

---

## 5. 设备指纹

指纹用于在网络中唯一区分设备。

| 模式 | 指纹生成方式 |
|------|-------------|
| **HTTPS** | 使用自签名 SSL/TLS 证书的 SHA-256 哈希值（大写十六进制） |

**实现细节**:
- 证书在应用启动时动态生成（RSA-2048）
- 证书有效期 10 年
- 指纹计算：`SHA256(cert.DER)` → 大写十六进制

---

## 6. 设备发现（组播公告）

### 6.1 发送公告

应用程序启动时，向组播地址发送以下 JSON：

```json
{
  "alias": "LocalSend Hub",
  "version": "2.0",
  "deviceModel": "LocalSend Hub Server",
  "deviceType": "server",
  "fingerprint": "<证书SHA-256>",
  "port": 53317,
  "protocol": "https",
  "download": false,
  "announcement": true,
  "announce": true
}
```

### 6.2 广播策略

| 阶段 | 间隔 | 说明 |
|------|------|------|
| 启动突发 | 100ms, 500ms, 2000ms | 快速被发现 |
| 周期广播 | 每 5 秒 | 持续可见性 |

---

## 7. HTTP API 端点

### 7.1 核心服务 (Port 53317 - HTTPS)

#### 设备信息（Info）

```
GET /api/localsend/v1/info?fingerprint={senderFingerprint}
GET /api/localsend/v2/info
```

**响应体**:
```json
{
  "alias": "LocalSend Hub",
  "version": "2.0",
  "deviceModel": "LocalSend Hub Server",
  "deviceType": "server",
  "fingerprint": "<证书SHA-256>",
  "port": 53317,
  "protocol": "https",
  "download": false,
  "announce": true,
  "announcement": true
}
```

#### 设备注册（Register）

```
POST /api/localsend/v1/register
POST /api/localsend/v2/register
```

**行为**: 返回与 Info 相同的设备信息。

#### 准备上传（Prepare Upload）

```
POST /api/localsend/v2/prepare-upload
```

**请求体**:
```json
{
  "files": {
    "fileId1": {
      "id": "fileId1",
      "fileName": "photo.jpg"
    }
  }
}
```

**响应体**:
```json
{
  "sessionId": "1712345678901234567",
  "files": {
    "fileId1": "1712345678901234568"
  }
}
```

**实现说明**:
- Session ID 使用 Unix 纳秒时间戳
- Token 使用 Unix 纳秒时间戳
- 会话信息存储在内存中（不持久化）

#### 接收文件（Upload）

```
POST /api/localsend/v2/upload?sessionId={sessionId}&fileId={fileId}&token={token}
Content-Type: application/octet-stream
```

**行为**:
- 根据 sessionId + fileId 解析文件名
- 自动处理文件名冲突（添加时间戳后缀）
- 直接写入目标文件（非临时文件 + 重命名）

### 7.2 管理面板 (Port 53318 - HTTP, LAN accessible)

| 端点 | 方法 | 说明 |
|------|------|------|
| `/` | GET | Web UI 仪表盘 |
| `/api/logs` | GET | 获取传输日志（倒序） |
| `/api/logs` | DELETE | 清空日志 |
| `/api/config` | POST | 更新接收目录 |
| `/api/identity` | GET | 获取设备身份信息 |
| `/api/identity` | POST | 更新设备身份信息 |
| `/api/files` | GET | 列出已接收文件 |
| `/files/{filename}` | GET | 下载文件 |

---

## 8. 状态管理

### 8.1 双服务状态

核心服务和管理服务各自维护独立的状态对象，通过共享配置文件同步数据：

- **核心服务 (`State`)**: 包含配置、日志、会话映射（内存）
- **管理服务 (`AdminState`)**: 包含配置、日志，每 2 秒轮询配置文件检测核心服务的变更
- **共享文件**: `localsend_config.json` 作为跨进程通信媒介

### 8.2 线程安全

所有共享状态通过 `sync.Mutex` 保护：

| 操作 | 锁策略 |
|------|--------|
| 读取/写入配置 | `mu.Lock()` |
| 添加日志 | `mu.Lock()` |
| 注册/解析会话 | `mu.Lock()`（仅核心服务） |

### 8.3 配置持久化

| 机制 | 说明 |
|------|------|
| 文件格式 | `localsend_config.json` |
| 核心服务 | 每 15 秒定时保存 + 配置变更立即保存 |
| 管理服务 | 配置变更立即保存 + 轮询文件检测变化 (2s) |
| 默认值 | 首次启动时创建 |

### 8.4 日志管理

| 参数 | 值 | 说明 |
|------|-----|------|
| 最大日志数 | 1000 | 环形缓冲，超出删除最旧 |
| 存储 | 内存 + JSON 文件 | 重启不丢失 |

---

## 9. 安全性

| 项目 | 实现 |
|------|------|
| 传输加密 | RSA-2048 自签名 TLS 证书 |
| 文件名安全 | 使用 `filepath.Base()` 防止路径遍历 |
| 管理面板 | 绑定 0.0.0.0，LAN 可访问，建议配合认证 |

### 9.1 未实现（可扩展）

- [ ] PIN 认证
- [ ] SHA256 文件完整性校验
- [ ] Token 随机化（当前使用时间戳）
- [ ] 速率限制
- [ ] 会话 TTL 过期
- [ ] 并发上传限制
- [ ] 原子文件写入（临时文件 + 重命名）

---

## 10. 优雅关闭

| 信号 | 行为 |
|------|------|
| SIGTERM / SIGINT | Go runtime 自动清理 |

**注意**: Docker 入口脚本 (`entrypoint.sh`) 会监控两个服务进程，核心服务崩溃时退出容器。

---

## 11. 网络兼容性

| 特性 | 说明 |
|------|------|
| IPv4 | 默认使用 |
| 多播 | UDP 组播 224.0.0.167 |

---

## 12. 构建与部署

### 12.1 编译

```bash
# 核心服务
go build -o localsend-hub .

# 管理服务
go build -o localsend-hub-admin ./cmd/admin

# 或使用脚本编译两个
```

### 12.2 运行

```bash
# 核心服务（文件接收）
./localsend-hub

# 管理服务（另一个终端）
./localsend-hub-admin
```

### 12.3 产物

| 文件 | 大小 | 说明 |
|------|------|------|
| `localsend-hub` | ~9MB | 核心服务二进制 |
| `localsend-hub-admin` | ~9MB | 管理服务二进制 |

### 12.4 Docker 部署

```bash
docker compose up -d
```

- 多阶段构建: `golang:1.21-alpine` → `alpine:3.19`（~15MB）
- 入口脚本启动并监控两个服务进程
- 健康检查探测核心服务 HTTPS 端点

---

## 13. 后续扩展

| 功能 | 状态 |
|------|------|
| SHA256 文件校验 | ❌ 未实现 |
| PIN 认证 | ❌ 未实现 |
| Token 随机化 | ❌ 使用时间戳 |
| 速率限制 | ❌ 未实现 |
| 会话 TTL | ❌ 未实现 |
| 原子写入 | ❌ 直接写入 |
| 自定义 CA 证书 | ❌ 未实现 |
| 设备黑名单 | ❌ 未实现 |
| 传输进度回调 | ❌ 未实现 |
| 云存储后端 | ❌ 未实现 |
