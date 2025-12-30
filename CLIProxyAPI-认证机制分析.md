# CLIProxyAPI 认证机制分析

## 项目概述

[CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 是一个开源代理服务器，它将基于 OAuth 的 CLI 工具（如 Claude Code、Gemini CLI、OpenAI Codex、Qwen Code）包装并暴露为标准的 OpenAI/Gemini/Claude 兼容 API 接口。

核心价值：让用户可以使用订阅账户（如 Claude Pro/Max、Gemini Advanced）的额度来调用 API，而无需单独购买 API credits。

---

## 认证架构

CLIProxyAPI 采用**双层认证架构**：

```
┌─────────────────┐     ┌──────────────────────┐     ┌─────────────────┐
│  AI 编码工具     │     │     CLIProxyAPI      │     │    官方 API     │
│  (Cline/n8n等)  │────▶│                      │────▶│ (Claude/Gemini) │
│                 │     │  Layer 1: API Key    │     │                 │
│  Authorization: │     │  Layer 2: OAuth注入  │     │  OAuth Token    │
│  Bearer <key>   │     │                      │     │  认证           │
└─────────────────┘     └──────────────────────┘     └─────────────────┘
```

---

## Layer 1: 客户端 API Key 验证（可选）

### 作用
保护代理服务本身，防止未授权访问。

### 配置方式

```yaml
# config.yaml
auth:
  providers:
    - type: api-key
      api-keys:
        - my-secure-client-key
        - another-key-for-team
```

### 使用场景

| 配置 | 效果 |
|------|------|
| `auth.providers: []` | 禁用 API key 验证，任何请求都可通过 |
| 配置 api-keys 列表 | 客户端必须在 Header 中提供有效密钥 |

### 客户端配置示例

```
API Base URL: http://localhost:8317/v1
API Key: my-secure-client-key  (或 dummy，如果禁用验证)
Model: claude-sonnet-4-5-20250929
```

---

## Layer 2: OAuth Token 认证（核心机制）

### 原理

CLIProxyAPI 复用官方 CLI 工具的 OAuth 认证机制，将用户订阅账户的 tokens 注入到 API 请求中。

### 认证流程

```
1. 用户执行登录命令
   └── cli-proxy-api --claude-login
   └── cli-proxy-api --gemini-login
   └── cli-proxy-api --codex-login

2. 系统启动临时认证服务器
   └── http://localhost:54545

3. 浏览器弹出官方 OAuth 页面
   └── 用户完成登录授权

4. Tokens 保存到本地
   └── ~/.cli-proxy-api/auths/
```

### Token 存储位置

| 服务 | 存储路径 |
|------|----------|
| CLIProxyAPI | `~/.cli-proxy-api/auths/` |
| Claude Code 原生 | `~/.claude/.credentials.json` |
| Gemini CLI | 系统 keychain 或配置目录 |

### Token 结构

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "refresh_token": "dGhpcyBpcyBhIHJlZnJlc2...",
  "expires_at": 1735689600
}
```

### 请求处理流程

```
1. 客户端发送请求 (带 dummy API key)
        ↓
2. CLIProxyAPI 拦截请求
        ↓
3. 移除客户端的 dummy API key
        ↓
4. 从本地读取 OAuth tokens
        ↓
5. 注入 OAuth token 到请求头
        ↓
6. 转发到官方 API (伪装为官方 CLI 工具)
        ↓
7. 接收响应并转换为 OpenAI 兼容格式
        ↓
8. 返回给客户端
```

---

## 高级认证特性

### 多账户配额管理

```yaml
quota:
  switch-project: true   # 配额耗尽时自动切换账户
  retry-count: 3         # 重试次数
```

支持配置多个 OAuth 账户，实现自动故障转移和负载均衡。

### 模型别名映射

```yaml
model-mapping:
  gpt-4: claude-sonnet-4-5-20250929
  gpt-4o: gemini-2.5-pro
```

允许客户端使用熟悉的模型名称，代理自动映射到实际模型。

---

## 安全配置

### 关键安全参数

```yaml
server:
  host: 127.0.0.1        # 仅本地访问
  port: 8317
  allow-remote: false    # 禁止远程访问管理界面
  secret-key: "强密码"    # 管理界面密码
```

### 安全建议

1. **生产环境**：始终设置 `allow-remote: false`
2. **多用户场景**：配置强 API keys
3. **VPS 部署**：使用 SSH 隧道进行认证
   ```bash
   ssh -L 54545:127.0.0.1:54545 user@vps-ip
   ```

---

## 快速开始

### 安装

```bash
# macOS
brew install cliproxyapi

# Windows
winget install LuisPater.CLIProxyAPI

# Docker
docker pull ghcr.io/router-for-me/cliproxyapi
```

### 登录认证

```bash
# Claude
cli-proxy-api --claude-login

# Gemini
cli-proxy-api --gemini-login

# OpenAI Codex
cli-proxy-api --codex-login
```

### 启动服务

```bash
cli-proxy-api --config config.yaml
```

### 测试连接

```bash
curl -X POST http://127.0.0.1:8317/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer dummy" \
  -d '{
    "model": "claude-sonnet-4-5-20250929",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

---

## 总结

| 认证层 | 目的 | 必需性 |
|--------|------|--------|
| API Key 验证 | 保护代理服务 | 可选 |
| OAuth Token | 访问官方 API | 必需 |

CLIProxyAPI 的认证本质是一个**合法的中间人代理**，它复用官方 CLI 工具的 OAuth 机制，让订阅用户能够以 API 方式使用其订阅额度，同时提供灵活的客户端认证保护。

---

## 参考资料

- [CLIProxyAPI GitHub](https://github.com/router-for-me/CLIProxyAPI)
- [官方文档](https://help.router-for.me/)
- [博客教程](https://antran.app/2025/claude_code_max_api/)


---

# 深入实现分析

## 一、OAuth Token 结构与生命周期

### 1.1 Claude Code 凭证文件结构

Claude Code 的认证信息存储在 `~/.claude/.credentials.json`：

```json
{
  "claudeAiOauth": {
    "accessToken": "sk-ant-oat01-xxxxx...",
    "refreshToken": "sk-ant-ort01-xxxxx...",
    "expiresAt": 1748658860401,
    "scopes": ["user:inference", "user:profile"]
  }
}
```

| 字段 | 说明 | 有效期 |
|------|------|--------|
| `accessToken` | 访问令牌，用于 API 调用 | 8-12 小时 |
| `refreshToken` | 刷新令牌，用于获取新的 access token | 较长，但也会过期 |
| `expiresAt` | access token 过期时间戳（毫秒） | - |
| `scopes` | 授权范围 | - |

### 1.2 Token 前缀含义

```
sk-ant-oat01-xxx  → OAuth Access Token (oat)
sk-ant-ort01-xxx  → OAuth Refresh Token (ort)
sk-ant-api03-xxx  → API Key (传统方式)
```

### 1.3 Token 刷新机制

```
┌─────────────────────────────────────────────────────────────┐
│                    Token 生命周期                            │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  [用户登录] ──▶ [获取 access + refresh token]               │
│                         │                                   │
│                         ▼                                   │
│              ┌─────────────────────┐                        │
│              │   access token      │                        │
│              │   有效期: 8-12小时   │                        │
│              └─────────────────────┘                        │
│                         │                                   │
│            ┌────────────┴────────────┐                      │
│            ▼                         ▼                      │
│      [token 有效]              [token 过期]                 │
│            │                         │                      │
│            ▼                         ▼                      │
│      [正常调用 API]          [使用 refresh token]           │
│                                      │                      │
│                         ┌────────────┴────────────┐         │
│                         ▼                         ▼         │
│                  [刷新成功]               [refresh 也过期]   │
│                  获取新 access            需要重新登录       │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

---

## 二、代理请求处理流程

### 2.1 请求拦截与转换

CLIProxyAPI 作为中间代理，执行以下转换：

```
┌──────────────────────────────────────────────────────────────────┐
│                     请求处理管道                                  │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. 接收客户端请求 (OpenAI 格式)                                  │
│     POST /v1/chat/completions                                    │
│     Authorization: Bearer dummy-key                              │
│     {                                                            │
│       "model": "gpt-4",                                          │
│       "messages": [...]                                          │
│     }                                                            │
│                         │                                        │
│                         ▼                                        │
│  2. API Key 验证层 (可选)                                         │
│     - 检查 config.yaml 中的 auth.providers                       │
│     - 验证客户端提供的 key 是否在白名单中                          │
│                         │                                        │
│                         ▼                                        │
│  3. 模型映射                                                      │
│     gpt-4 → claude-sonnet-4-5-20250929                           │
│     (根据 model-mapping 配置)                                     │
│                         │                                        │
│                         ▼                                        │
│  4. OAuth Token 注入                                              │
│     - 读取 ~/.cli-proxy-api/auths/ 中的凭证                       │
│     - 检查 token 是否过期                                         │
│     - 如过期，使用 refresh_token 刷新                             │
│     - 替换 Authorization header                                  │
│                         │                                        │
│                         ▼                                        │
│  5. 协议转换 (OpenAI → Claude/Gemini)                             │
│     - 转换消息格式                                                │
│     - 转换参数名称                                                │
│     - 处理 system prompt                                         │
│                         │                                        │
│                         ▼                                        │
│  6. 转发到官方 API                                                │
│     POST https://api.anthropic.com/v1/messages                   │
│     Authorization: Bearer sk-ant-oat01-xxx                       │
│     X-Api-Key: (OAuth token)                                     │
│                         │                                        │
│                         ▼                                        │
│  7. 响应转换 (Claude → OpenAI 格式)                               │
│     - SSE 流转换                                                  │
│     - JSON 结构映射                                               │
│     - 错误码转换                                                  │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

### 2.2 协议转换细节

**OpenAI 请求格式：**
```json
{
  "model": "gpt-4",
  "messages": [
    {"role": "system", "content": "You are helpful"},
    {"role": "user", "content": "Hello"}
  ],
  "temperature": 0.7,
  "max_tokens": 1000,
  "stream": true
}
```

**转换为 Claude 格式：**
```json
{
  "model": "claude-sonnet-4-5-20250929",
  "system": "You are helpful",
  "messages": [
    {"role": "user", "content": "Hello"}
  ],
  "temperature": 0.7,
  "max_tokens": 1000,
  "stream": true
}
```

**关键差异：**
| 特性 | OpenAI | Claude |
|------|--------|--------|
| System Prompt | messages 数组中 | 独立 system 字段 |
| 认证 Header | `Authorization: Bearer` | `x-api-key` 或 OAuth |
| 流式响应 | `data: {...}` | `event: content_block_delta` |

---

## 三、OAuth 登录实现

### 3.1 登录流程时序图

```
┌────────┐     ┌─────────────┐     ┌──────────────┐     ┌─────────────┐
│  用户   │     │ CLIProxyAPI │     │   浏览器      │     │ Anthropic   │
└───┬────┘     └──────┬──────┘     └──────┬───────┘     └──────┬──────┘
    │                 │                   │                    │
    │ cli-proxy-api   │                   │                    │
    │ --claude-login  │                   │                    │
    │────────────────▶│                   │                    │
    │                 │                   │                    │
    │                 │ 启动本地服务器      │                    │
    │                 │ localhost:54545   │                    │
    │                 │──────────────────▶│                    │
    │                 │                   │                    │
    │                 │ 打开 OAuth URL     │                    │
    │                 │──────────────────▶│                    │
    │                 │                   │                    │
    │                 │                   │ 重定向到 Anthropic  │
    │                 │                   │───────────────────▶│
    │                 │                   │                    │
    │                 │                   │◀───────────────────│
    │                 │                   │   登录页面          │
    │                 │                   │                    │
    │  输入凭证        │                   │                    │
    │─────────────────────────────────────▶                    │
    │                 │                   │                    │
    │                 │                   │ 授权回调            │
    │                 │                   │───────────────────▶│
    │                 │                   │                    │
    │                 │                   │◀───────────────────│
    │                 │                   │ code + state       │
    │                 │                   │                    │
    │                 │◀──────────────────│                    │
    │                 │ 回调到 localhost   │                    │
    │                 │                   │                    │
    │                 │ 用 code 换 token   │                    │
    │                 │────────────────────────────────────────▶
    │                 │                   │                    │
    │                 │◀────────────────────────────────────────
    │                 │ access_token +    │                    │
    │                 │ refresh_token     │                    │
    │                 │                   │                    │
    │                 │ 保存到本地文件      │                    │
    │◀────────────────│                   │                    │
    │  登录成功        │                   │                    │
    │                 │                   │                    │
```

### 3.2 OAuth 端点（推测）

基于 Claude Code 的行为，OAuth 流程可能使用以下端点：

```
授权端点: https://claude.ai/oauth/authorize
Token 端点: https://claude.ai/oauth/token
回调地址: http://localhost:54545/callback
```

**授权请求参数：**
```
client_id=claude-code-cli
redirect_uri=http://localhost:54545/callback
response_type=code
scope=user:inference user:profile
state=<random_state>
code_challenge=<PKCE_challenge>
code_challenge_method=S256
```

---

## 四、多 Provider 支持

### 4.1 支持的 Provider

| Provider | CLI 工具 | OAuth 提供商 | Token 存储 |
|----------|----------|--------------|------------|
| Claude | Claude Code | Anthropic | `~/.claude/.credentials.json` |
| Gemini | Gemini CLI | Google | `~/.gemini/` 或 Keychain |
| OpenAI Codex | Codex CLI | OpenAI | `~/.codex/` |
| Qwen Code | Qwen CLI | Alibaba | `~/.qwen/` |

### 4.2 Provider 选择逻辑

```yaml
# config.yaml
providers:
  - name: claude
    priority: 1
    models:
      - claude-sonnet-4-5-20250929
      - claude-opus-4-20250514
  
  - name: gemini
    priority: 2
    models:
      - gemini-2.5-pro
      - gemini-2.5-flash
```

**路由逻辑：**
```
1. 解析请求中的 model 参数
2. 查找 model-mapping 获取实际模型名
3. 根据模型名确定 provider
4. 检查该 provider 的 OAuth token 是否有效
5. 如果无效，尝试下一个 priority 的 provider
6. 转发请求到选定的 provider
```

---

## 五、Token 管理与刷新

### 5.1 自动刷新实现

```go
// 伪代码：Token 刷新逻辑
func getValidToken(provider string) (*Token, error) {
    token := loadTokenFromFile(provider)
    
    // 检查是否过期（提前 5 分钟刷新）
    if token.ExpiresAt.Add(-5*time.Minute).Before(time.Now()) {
        newToken, err := refreshToken(token.RefreshToken)
        if err != nil {
            return nil, err // 需要重新登录
        }
        saveTokenToFile(provider, newToken)
        return newToken, nil
    }
    
    return token, nil
}

func refreshToken(refreshToken string) (*Token, error) {
    resp, err := http.Post(tokenEndpoint, "application/json", 
        map[string]string{
            "grant_type":    "refresh_token",
            "refresh_token": refreshToken,
            "client_id":     clientID,
        })
    // 解析响应...
}
```

### 5.2 Token 存储结构

```
~/.cli-proxy-api/
├── auths/
│   ├── claude/
│   │   ├── default.json      # 默认账户
│   │   ├── account-2.json    # 多账户支持
│   │   └── account-3.json
│   ├── gemini/
│   │   └── default.json
│   └── codex/
│       └── default.json
├── config.yaml               # 配置文件
└── logs/                     # 日志目录
```

---

## 六、安全机制分析

### 6.1 请求伪装

CLIProxyAPI 伪装成官方 CLI 工具发送请求：

```http
POST /v1/messages HTTP/1.1
Host: api.anthropic.com
Authorization: Bearer sk-ant-oat01-xxx
User-Agent: claude-code/1.0.0
X-Client-Type: cli
X-Session-Id: <session_id>
```

### 6.2 风险与限制

| 风险点 | 说明 | 缓解措施 |
|--------|------|----------|
| Token 泄露 | 本地存储的 token 可能被窃取 | 文件权限 600，加密存储 |
| 账户封禁 | 违反 ToS 可能导致封号 | 合理使用，避免滥用 |
| Token 过期 | 需要定期重新登录 | 自动刷新机制 |
| API 变更 | 官方可能修改 API | 持续更新适配 |

### 6.3 与官方 API Key 的区别

| 特性 | OAuth Token | API Key |
|------|-------------|---------|
| 获取方式 | 浏览器登录 | Console 生成 |
| 计费方式 | 订阅额度 | 按 token 计费 |
| 有效期 | 8-12 小时 | 长期有效 |
| 刷新机制 | 自动刷新 | 无需刷新 |
| 使用限制 | 订阅计划限制 | 按量付费 |

---

## 七、核心代码结构（推测）

基于 Go 语言实现的典型结构：

```
cliproxyapi/
├── cmd/
│   └── main.go              # 入口点
├── internal/
│   ├── auth/
│   │   ├── oauth.go         # OAuth 登录流程
│   │   ├── token.go         # Token 管理
│   │   └── refresh.go       # Token 刷新
│   ├── proxy/
│   │   ├── handler.go       # HTTP 处理器
│   │   ├── middleware.go    # 中间件（认证、日志）
│   │   └── transform.go     # 协议转换
│   ├── provider/
│   │   ├── claude.go        # Claude 适配器
│   │   ├── gemini.go        # Gemini 适配器
│   │   └── codex.go         # Codex 适配器
│   └── config/
│       └── config.go        # 配置解析
├── config.yaml.example
└── go.mod
```

---

## 八、实现要点总结

1. **OAuth 复用**：核心是复用官方 CLI 工具的 OAuth 认证，而非破解或绕过
2. **协议转换**：在 OpenAI 兼容格式和各 Provider 原生格式之间转换
3. **Token 生命周期**：自动管理 token 刷新，处理过期情况
4. **多账户支持**：支持配置多个账户实现负载均衡和故障转移
5. **安全分层**：可选的 API Key 层保护代理服务本身

这种设计让用户可以用订阅账户的额度来调用 API，同时保持与现有工具的兼容性。

---

## 九、生态系统与衍生项目

### 9.1 基于 CLIProxyAPI 的项目

| 项目名 | 功能描述 | 平台 |
|--------|----------|------|
| **VibeProxy** | macOS 菜单栏应用，使用 Claude Code 和 ChatGPT 订阅 | macOS |
| **Subtitle Translator** | 浏览器工具，通过 CLIProxyAPI 翻译 SRT 字幕 | Web |
| **CCS (Claude Code Switch)** | CLI 工具，快速切换多个 Claude 账户和模型 | CLI |
| **ProxyPal** | macOS GUI，管理 CLIProxyAPI 配置和 OAuth | macOS |
| **Quotio** | macOS 菜单栏应用，统一管理多个 AI 订阅配额 | macOS |

### 9.2 支持的 AI 编码工具

CLIProxyAPI 可与以下工具集成：

- **Cline** - VS Code AI 编码助手
- **Continue** - 开源 AI 编码助手
- **Factory Droid** - AI 开发助手 CLI
- **n8n** - 工作流自动化平台
- **Amp CLI/IDE** - AI 编码工具
- **OpenCode** - 开源编码助手
- **Cursor** - AI 代码编辑器

---

## 十、配置详解

### 10.1 完整配置示例

```yaml
# config.yaml - 完整配置参考

# 服务器配置
server:
  host: 127.0.0.1          # 绑定地址
  port: 8317               # 监听端口
  allow-remote: false      # 是否允许远程访问管理界面
  secret-key: "your-strong-password"  # 管理界面密码

# 认证配置
auth:
  providers:
    - type: api-key
      api-keys:
        - my-secure-client-key
        - team-member-key

# 配额管理
quota:
  switch-project: true     # 配额耗尽时自动切换账户
  retry-count: 3           # 重试次数

# 模型映射
model-mapping:
  # OpenAI 模型别名 → 实际模型
  gpt-4: claude-sonnet-4-5-20250929
  gpt-4o: gemini-2.5-pro
  gpt-4-turbo: claude-opus-4-20250514

# 日志配置
logging:
  level: info              # debug, info, warn, error
  directory: ./logs
```

### 10.2 Docker Compose 部署

```yaml
version: '3.8'
services:
  cliproxyapi:
    image: ghcr.io/router-for-me/cliproxyapi:latest
    container_name: cliproxyapi
    restart: unless-stopped
    ports:
      - "8317:8317"
    volumes:
      - ./config.yaml:/app/config.yaml:ro
      - ./auths:/root/.cli-proxy-api/auths
    environment:
      - TZ=Asia/Shanghai
```

### 10.3 Systemd 服务配置

```ini
# /etc/systemd/system/cli-proxy-api.service
[Unit]
Description=CLI Proxy API Service
After=network.target

[Service]
Type=simple
User=your-user
WorkingDirectory=/opt/cliproxyapi
ExecStart=/opt/cliproxyapi/cli-proxy-api --config /opt/cliproxyapi/config.yaml
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

---

## 十一、API 端点参考

### 11.1 OpenAI 兼容端点

| 端点 | 方法 | 描述 |
|------|------|------|
| `/v1/chat/completions` | POST | 聊天补全（主要端点） |
| `/v1/models` | GET | 列出可用模型 |
| `/v1/embeddings` | POST | 文本嵌入（如支持） |

### 11.2 管理端点

| 端点 | 方法 | 描述 |
|------|------|------|
| `/management.html` | GET | Web 管理界面 |
| `/health` | GET | 健康检查 |
| `/metrics` | GET | 性能指标 |

### 11.3 请求示例

**基础聊天请求：**
```bash
curl -X POST http://127.0.0.1:8317/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "claude-sonnet-4-5-20250929",
    "messages": [
      {"role": "user", "content": "Hello!"}
    ],
    "stream": false
  }'
```

**流式响应请求：**
```bash
curl -X POST http://127.0.0.1:8317/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "claude-sonnet-4-5-20250929",
    "messages": [
      {"role": "user", "content": "Write a poem"}
    ],
    "stream": true
  }'
```

**带图片的多模态请求（Base64）：**
```bash
curl -X POST http://127.0.0.1:8317/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "claude-sonnet-4-5-20250929",
    "messages": [
      {
        "role": "user",
        "content": [
          {"type": "text", "text": "What is in this image?"},
          {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64,/9j/4AAQ..."}}
        ]
      }
    ]
  }'
```

---

## 十二、故障排除指南

### 12.1 常见错误

| 错误 | 原因 | 解决方案 |
|------|------|----------|
| `HTTP 401` | Token 过期或无效 | 重新执行 `--claude-login` |
| `HTTP 404 Model not found` | 模型名称错误 | 检查 model-mapping 配置 |
| `Connection refused` | 服务未启动或端口错误 | 检查服务状态和端口配置 |
| `Address in use` | 端口被占用 | 更换端口或停止占用进程 |
| `Token unavailable` | 未登录或凭证丢失 | 重新执行登录命令 |

### 12.2 调试模式

```bash
# 启用详细日志
cli-proxy-api --config config.yaml --debug

# 查看日志
tail -f logs/cliproxyapi.log
```

### 12.3 VPS 远程认证

在无 GUI 的服务器上进行 OAuth 登录：

```bash
# 本地机器：建立 SSH 隧道
ssh -L 54545:127.0.0.1:54545 user@vps-ip

# VPS 上：执行登录
cli-proxy-api --claude-login

# 在本地浏览器打开认证 URL 完成登录
```

---

## 十三、技术实现深度分析

### 13.1 为什么能工作？

CLIProxyAPI 的核心原理是**复用官方 CLI 工具的认证机制**：

1. **官方 CLI 工具**（如 Claude Code）使用 OAuth 2.0 进行用户认证
2. 认证后获得的 **access_token** 可以调用 Anthropic 的 API
3. 这些 token 与用户的**订阅计划**绑定，而非按 token 计费
4. CLIProxyAPI 读取这些 token，注入到 API 请求中
5. 从 Anthropic 服务器角度看，请求来自"官方 CLI 工具"

### 13.2 与直接使用 API Key 的区别

```
┌─────────────────────────────────────────────────────────────────┐
│                    API Key 方式                                  │
├─────────────────────────────────────────────────────────────────┤
│  用户 → Console 生成 API Key → 按 token 计费 → 无限制使用        │
│                                                                 │
│  优点：简单、稳定、无需刷新                                       │
│  缺点：按量付费，成本高                                          │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                    OAuth Token 方式 (CLIProxyAPI)                │
├─────────────────────────────────────────────────────────────────┤
│  用户 → 浏览器登录 → OAuth Token → 订阅额度 → 有使用限制          │
│                                                                 │
│  优点：使用订阅额度，成本固定                                     │
│  缺点：需要刷新、可能有速率限制                                   │
└─────────────────────────────────────────────────────────────────┘
```

### 13.3 协议转换层实现

CLIProxyAPI 需要处理不同 AI 提供商的 API 差异：

```
┌──────────────────────────────────────────────────────────────────┐
│                     协议转换矩阵                                  │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  OpenAI 格式 (输入)          Claude 格式 (输出)                   │
│  ─────────────────          ─────────────────                    │
│  model: "gpt-4"       →     model: "claude-sonnet-4-5-20250929"  │
│  messages[0].role:    →     system: "..."                        │
│    "system"                                                      │
│  max_tokens           →     max_tokens                           │
│  temperature          →     temperature                          │
│  stream: true         →     stream: true                         │
│                                                                  │
│  响应转换：                                                       │
│  ─────────                                                       │
│  Claude SSE:                OpenAI SSE:                          │
│  event: content_block  →    data: {"choices":[...]}              │
│  data: {"delta":...}                                             │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

---

## 十四、法律与合规考量

### 14.1 服务条款分析

使用 CLIProxyAPI 可能涉及以下 ToS 条款：

| 提供商 | 潜在风险 | 说明 |
|--------|----------|------|
| Anthropic | 中等 | Claude Code 的 OAuth 机制本身是合法的 |
| Google | 低 | Gemini CLI 设计上支持 API 调用 |
| OpenAI | 中等 | Codex CLI 的使用范围可能有限制 |

### 14.2 使用建议

1. **个人使用**：风险较低，合理使用订阅额度
2. **商业使用**：建议使用官方 API Key
3. **大规模使用**：可能触发速率限制或账户审查
4. **多账户**：避免滥用，可能违反 ToS

---

## 十五、未来展望

### 15.1 可能的发展方向

- **更多 Provider 支持**：如 Mistral、Cohere 等
- **增强的配额管理**：智能负载均衡
- **企业级功能**：审计日志、访问控制
- **GUI 管理界面**：更友好的配置体验

### 15.2 潜在风险

- **API 变更**：官方可能修改 OAuth 机制
- **封禁风险**：大规模使用可能导致账户封禁
- **功能限制**：订阅计划可能增加使用限制

---

## 总结

CLIProxyAPI 是一个精巧的中间件，它通过复用官方 CLI 工具的 OAuth 认证机制，让用户能够以 API 方式使用其 AI 订阅服务。其核心价值在于：

1. **成本优化**：将固定订阅费用转化为 API 调用能力
2. **兼容性**：提供 OpenAI 兼容接口，支持众多工具
3. **灵活性**：支持多 Provider、多账户、模型映射
4. **开源透明**：MIT 许可，社区驱动

对于希望最大化 AI 订阅价值的开发者来说，这是一个值得关注的工具。


---

# 第二部分：深入源码级实现分析

## 十六、各 Provider OAuth 认证机制详解

### 16.1 Claude Code OAuth 认证

#### 凭证文件结构

Claude Code 的认证信息存储在 `~/.claude/.credentials.json`：

```json
{
  "claudeAiOauth": {
    "accessToken": "sk-ant-oat01-xxxxx...",
    "refreshToken": "sk-ant-ort01-xxxxx...",
    "expiresAt": 1748658860401,
    "scopes": ["user:inference", "user:profile"]
  }
}
```

#### Token 前缀解析

| 前缀 | 含义 | 用途 |
|------|------|------|
| `sk-ant-oat01-` | OAuth Access Token | API 调用认证 |
| `sk-ant-ort01-` | OAuth Refresh Token | 刷新 access token |
| `sk-ant-api03-` | API Key | 传统 Console 生成的密钥 |

#### OAuth 端点（逆向分析）

```
授权端点: https://claude.ai/oauth/authorize
Token 端点: https://claude.ai/oauth/token
回调地址: http://localhost:54545/callback

授权请求参数:
- client_id: claude-code-cli
- redirect_uri: http://localhost:54545/callback
- response_type: code
- scope: user:inference user:profile
- state: <random_state>
- code_challenge: <PKCE_challenge>
- code_challenge_method: S256
```

#### Token 生命周期

- **Access Token**: 8-12 小时有效期
- **Refresh Token**: 较长有效期，但也会过期
- **自动刷新**: Claude CLI 在 token 过期前自动刷新

---

### 16.2 Gemini CLI OAuth 认证

#### 认证方式

Gemini CLI 支持多种认证方式：

| 方式 | 适用场景 | 配置方法 |
|------|----------|----------|
| Google 账户登录 | 本地开发，Google AI Pro/Ultra 订阅用户 | `gemini auth login` |
| API Key | 按量付费用户 | `GEMINI_API_KEY` 环境变量 |
| Vertex AI ADC | Google Cloud 用户 | `gcloud auth application-default login` |
| Service Account | CI/CD、服务器环境 | `GOOGLE_APPLICATION_CREDENTIALS` |

#### Google 账户登录流程

```
1. 用户执行 gemini auth login
2. CLI 启动本地服务器监听回调
3. 浏览器打开 Google OAuth 授权页面
4. 用户授权后，Google 重定向回本地服务器
5. CLI 获取 authorization code
6. 用 code 换取 access_token 和 refresh_token
7. 凭证缓存到本地
```

#### 凭证存储位置

```
~/.gemini/.env           # 环境变量配置
~/.gemini/credentials    # OAuth 凭证（可能）
系统 Keychain            # macOS 上可能使用系统钥匙串
```

---

### 16.3 OpenAI Codex CLI OAuth 认证

#### 认证方式

Codex CLI 支持两种认证：

| 方式 | 计费模式 | 配置方法 |
|------|----------|----------|
| ChatGPT 登录 | 使用订阅额度 | `codex login` (OAuth) |
| API Key | 按量付费 | `codex login --with-api-key` |

#### OAuth 登录流程

```
1. 用户执行 codex login
2. CLI 启动本地服务器 localhost:1455
3. 浏览器打开 OpenAI OAuth 页面 ("Sign in with ChatGPT")
4. 用户完成登录授权
5. 凭证保存到 ~/.codex/auth.json
```

#### 凭证文件结构

```json
// ~/.codex/auth.json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "refresh_token": "...",
  "expires_at": 1735689600,
  "token_type": "Bearer"
}
```

#### Headless 环境认证

```bash
# 方法1: SSH 隧道
ssh -L 1455:localhost:1455 user@remote-host
# 然后在远程执行 codex login

# 方法2: 本地认证后复制凭证
scp ~/.codex/auth.json user@remote:~/.codex/auth.json
```

---

## 十七、协议转换层深度分析

### 17.1 OpenAI → Claude 格式转换

#### 请求转换

```go
// 伪代码：OpenAI 请求转 Claude 请求
func convertOpenAIToClaude(openaiReq *OpenAIRequest) *ClaudeRequest {
    claudeReq := &ClaudeRequest{
        Model:      mapModel(openaiReq.Model),
        MaxTokens:  openaiReq.MaxTokens,
        Temperature: openaiReq.Temperature,
        Stream:     openaiReq.Stream,
    }
    
    // 提取 system message
    for _, msg := range openaiReq.Messages {
        if msg.Role == "system" {
            claudeReq.System = msg.Content
        } else {
            claudeReq.Messages = append(claudeReq.Messages, ClaudeMessage{
                Role:    msg.Role,
                Content: convertContent(msg.Content),
            })
        }
    }
    
    return claudeReq
}
```

#### 消息格式差异

**OpenAI 格式：**
```json
{
  "messages": [
    {"role": "system", "content": "You are helpful"},
    {"role": "user", "content": "Hello"},
    {"role": "assistant", "content": "Hi there!"},
    {"role": "user", "content": "How are you?"}
  ]
}
```

**Claude 格式：**
```json
{
  "system": "You are helpful",
  "messages": [
    {"role": "user", "content": "Hello"},
    {"role": "assistant", "content": "Hi there!"},
    {"role": "user", "content": "How are you?"}
  ]
}
```

### 17.2 多模态内容转换

#### 图片处理

**OpenAI 格式（URL 或 Base64）：**
```json
{
  "role": "user",
  "content": [
    {"type": "text", "text": "What's in this image?"},
    {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64,/9j/4AAQ..."}}
  ]
}
```

**Claude 格式：**
```json
{
  "role": "user",
  "content": [
    {"type": "text", "text": "What's in this image?"},
    {
      "type": "image",
      "source": {
        "type": "base64",
        "media_type": "image/jpeg",
        "data": "/9j/4AAQ..."
      }
    }
  ]
}
```

### 17.3 SSE 流式响应转换

#### Claude SSE 格式

```
event: message_start
data: {"type":"message_start","message":{"id":"msg_01XFDUDYJgAACzvnptvVoYEL","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-5-20250929","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":25,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"!"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":12}}

event: message_stop
data: {"type":"message_stop"}
```

#### OpenAI SSE 格式

```
data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1694268190,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1694268190,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1694268190,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1694268190,"model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

#### 转换逻辑

```go
// 伪代码：Claude SSE → OpenAI SSE
func convertClaudeSSEToOpenAI(claudeEvent *ClaudeSSEEvent) *OpenAISSEEvent {
    switch claudeEvent.Type {
    case "content_block_delta":
        return &OpenAISSEEvent{
            ID:      generateID(),
            Object:  "chat.completion.chunk",
            Created: time.Now().Unix(),
            Model:   claudeEvent.Model,
            Choices: []Choice{{
                Index: 0,
                Delta: Delta{
                    Content: claudeEvent.Delta.Text,
                },
                FinishReason: nil,
            }},
        }
    case "message_stop":
        return &OpenAISSEEvent{
            // ... 设置 finish_reason: "stop"
        }
    }
    return nil
}
```

---

## 十八、Token 管理与刷新机制

### 18.1 Token 刷新策略

```go
// 伪代码：Token 管理器
type TokenManager struct {
    tokens     map[string]*Token  // provider -> token
    refreshMu  sync.Mutex
}

func (tm *TokenManager) GetValidToken(provider string) (*Token, error) {
    token := tm.tokens[provider]
    if token == nil {
        return nil, ErrNotLoggedIn
    }
    
    // 提前 5 分钟刷新
    if token.ExpiresAt.Add(-5*time.Minute).Before(time.Now()) {
        tm.refreshMu.Lock()
        defer tm.refreshMu.Unlock()
        
        // 双重检查
        if token.ExpiresAt.Add(-5*time.Minute).Before(time.Now()) {
            newToken, err := tm.refreshToken(provider, token.RefreshToken)
            if err != nil {
                return nil, err
            }
            tm.tokens[provider] = newToken
            tm.saveToFile(provider, newToken)
            return newToken, nil
        }
    }
    
    return token, nil
}

func (tm *TokenManager) refreshToken(provider string, refreshToken string) (*Token, error) {
    endpoint := getTokenEndpoint(provider)
    
    resp, err := http.PostForm(endpoint, url.Values{
        "grant_type":    {"refresh_token"},
        "refresh_token": {refreshToken},
        "client_id":     {getClientID(provider)},
    })
    if err != nil {
        return nil, err
    }
    
    var tokenResp TokenResponse
    json.NewDecoder(resp.Body).Decode(&tokenResp)
    
    return &Token{
        AccessToken:  tokenResp.AccessToken,
        RefreshToken: tokenResp.RefreshToken,
        ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
    }, nil
}
```

### 18.2 多账户轮换

```go
// 伪代码：配额管理与账户轮换
type QuotaManager struct {
    accounts    []*Account
    currentIdx  int
    mu          sync.Mutex
}

func (qm *QuotaManager) GetNextAccount() *Account {
    qm.mu.Lock()
    defer qm.mu.Unlock()
    
    startIdx := qm.currentIdx
    for {
        account := qm.accounts[qm.currentIdx]
        qm.currentIdx = (qm.currentIdx + 1) % len(qm.accounts)
        
        if account.IsAvailable() {
            return account
        }
        
        // 所有账户都不可用
        if qm.currentIdx == startIdx {
            return nil
        }
    }
}

func (qm *QuotaManager) HandleRateLimitError(account *Account) {
    account.MarkUnavailable(time.Now().Add(1 * time.Hour))
}
```

---

## 十九、请求伪装与安全机制

### 19.1 请求头伪装

CLIProxyAPI 伪装成官方 CLI 工具发送请求：

```http
POST /v1/messages HTTP/1.1
Host: api.anthropic.com
Authorization: Bearer sk-ant-oat01-xxx
User-Agent: claude-code/1.0.0
X-Client-Type: cli
X-Session-Id: <session_id>
anthropic-version: 2023-06-01
anthropic-beta: messages-2024-01-01
Content-Type: application/json
```

### 19.2 关键请求头

| Header | 说明 | 必需性 |
|--------|------|--------|
| `Authorization` | OAuth token 或 API key | 必需 |
| `anthropic-version` | API 版本 | 必需 |
| `anthropic-beta` | Beta 功能标识 | 可选 |
| `User-Agent` | 客户端标识 | 推荐 |
| `X-Client-Type` | 客户端类型 | 可选 |

### 19.3 安全考量

```yaml
# 安全配置最佳实践
server:
  host: 127.0.0.1        # 仅本地访问
  allow-remote: false    # 禁止远程管理
  
auth:
  providers:
    - type: api-key
      api-keys:
        - "${PROXY_API_KEY}"  # 使用环境变量

# Token 文件权限
# chmod 600 ~/.cli-proxy-api/auths/*
```

---

## 二十、核心代码架构（推测）

### 20.1 项目结构

```
cliproxyapi/
├── cmd/
│   └── main.go                    # 入口点，CLI 参数解析
├── internal/
│   ├── auth/
│   │   ├── manager.go             # Token 管理器
│   │   ├── oauth.go               # OAuth 登录流程
│   │   ├── refresh.go             # Token 刷新
│   │   └── storage.go             # Token 持久化
│   ├── proxy/
│   │   ├── server.go              # HTTP 服务器
│   │   ├── handler.go             # 请求处理器
│   │   ├── middleware.go          # 中间件（认证、日志、限流）
│   │   └── router.go              # 路由配置
│   ├── transform/
│   │   ├── openai_to_claude.go    # OpenAI → Claude 转换
│   │   ├── claude_to_openai.go    # Claude → OpenAI 转换
│   │   ├── openai_to_gemini.go    # OpenAI → Gemini 转换
│   │   ├── gemini_to_openai.go    # Gemini → OpenAI 转换
│   │   └── sse.go                 # SSE 流转换
│   ├── provider/
│   │   ├── interface.go           # Provider 接口定义
│   │   ├── claude.go              # Claude Provider
│   │   ├── gemini.go              # Gemini Provider
│   │   ├── codex.go               # Codex Provider
│   │   └── qwen.go                # Qwen Provider
│   ├── quota/
│   │   ├── manager.go             # 配额管理
│   │   └── failover.go            # 故障转移
│   └── config/
│       ├── config.go              # 配置解析
│       └── model_mapping.go       # 模型映射
├── web/
│   └── management.html            # 管理界面
├── config.yaml.example
├── go.mod
└── go.sum
```

### 20.2 核心接口定义

```go
// Provider 接口
type Provider interface {
    Name() string
    SupportedModels() []string
    GetToken() (*Token, error)
    RefreshToken() error
    SendRequest(ctx context.Context, req *Request) (*Response, error)
    StreamRequest(ctx context.Context, req *Request) (<-chan *StreamEvent, error)
}

// Transformer 接口
type Transformer interface {
    TransformRequest(openaiReq *OpenAIRequest) (interface{}, error)
    TransformResponse(providerResp interface{}) (*OpenAIResponse, error)
    TransformStreamEvent(event interface{}) (*OpenAIStreamEvent, error)
}

// TokenManager 接口
type TokenManager interface {
    GetToken(provider string) (*Token, error)
    RefreshToken(provider string) error
    SaveToken(provider string, token *Token) error
    LoadToken(provider string) (*Token, error)
}
```

### 20.3 请求处理流程

```go
// 伪代码：主请求处理器
func (h *Handler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
    // 1. 解析请求
    var openaiReq OpenAIRequest
    json.NewDecoder(r.Body).Decode(&openaiReq)
    
    // 2. 确定 Provider
    provider := h.selectProvider(openaiReq.Model)
    
    // 3. 获取有效 Token
    token, err := h.tokenManager.GetToken(provider.Name())
    if err != nil {
        h.handleError(w, err)
        return
    }
    
    // 4. 转换请求格式
    transformer := h.getTransformer(provider.Name())
    providerReq, err := transformer.TransformRequest(&openaiReq)
    
    // 5. 发送请求
    if openaiReq.Stream {
        h.handleStreamRequest(w, provider, transformer, providerReq, token)
    } else {
        h.handleSyncRequest(w, provider, transformer, providerReq, token)
    }
}

func (h *Handler) handleStreamRequest(w http.ResponseWriter, provider Provider, 
    transformer Transformer, req interface{}, token *Token) {
    
    // 设置 SSE headers
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    
    flusher := w.(http.Flusher)
    
    // 获取 Provider 的流式响应
    eventChan, err := provider.StreamRequest(r.Context(), req)
    if err != nil {
        // 处理错误
        return
    }
    
    // 转换并转发每个事件
    for event := range eventChan {
        openaiEvent, err := transformer.TransformStreamEvent(event)
        if err != nil {
            continue
        }
        
        data, _ := json.Marshal(openaiEvent)
        fmt.Fprintf(w, "data: %s\n\n", data)
        flusher.Flush()
    }
    
    fmt.Fprintf(w, "data: [DONE]\n\n")
    flusher.Flush()
}
```

---

## 二十一、与类似项目对比

### 21.1 项目对比表

| 项目 | 方向 | 语言 | 特点 |
|------|------|------|------|
| **CLIProxyAPI** | CLI OAuth → OpenAI API | Go | 多 Provider、配额管理、模型映射 |
| **claude2openai** | Claude API → OpenAI API | Go | 轻量、专注 Claude |
| **claude-code-router** | Claude API → OpenRouter | TypeScript | Cloudflare Workers 部署 |
| **claude-code-proxy** | Claude Code → OpenAI | Python | 简单代理 |
| **LiteLLM** | 统一 LLM 接口 | Python | 企业级、功能丰富 |

### 21.2 CLIProxyAPI 独特优势

1. **OAuth Token 复用**: 唯一支持复用官方 CLI 工具 OAuth 的项目
2. **多 Provider 支持**: Claude、Gemini、Codex、Qwen 一站式
3. **配额管理**: 多账户轮换、自动故障转移
4. **模型映射**: 灵活的别名配置
5. **Go 实现**: 高性能、单二进制部署

---

## 二十二、实现一个简化版本的思路

如果要自己实现类似功能，核心步骤：

### 22.1 OAuth 登录模块

```go
// 1. 启动本地 OAuth 回调服务器
func startOAuthServer(port int) (chan string, error) {
    codeChan := make(chan string, 1)
    
    http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
        code := r.URL.Query().Get("code")
        codeChan <- code
        fmt.Fprintf(w, "Login successful! You can close this window.")
    })
    
    go http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
    return codeChan, nil
}

// 2. 构建授权 URL 并打开浏览器
func buildAuthURL(provider string) string {
    params := url.Values{
        "client_id":     {getClientID(provider)},
        "redirect_uri":  {"http://localhost:54545/callback"},
        "response_type": {"code"},
        "scope":         {getScopes(provider)},
        "state":         {generateState()},
    }
    return fmt.Sprintf("%s?%s", getAuthEndpoint(provider), params.Encode())
}

// 3. 用 code 换 token
func exchangeCodeForToken(provider, code string) (*Token, error) {
    resp, err := http.PostForm(getTokenEndpoint(provider), url.Values{
        "grant_type":   {"authorization_code"},
        "code":         {code},
        "redirect_uri": {"http://localhost:54545/callback"},
        "client_id":    {getClientID(provider)},
    })
    // 解析响应...
}
```

### 22.2 代理服务器模块

```go
// 核心代理处理
func proxyHandler(w http.ResponseWriter, r *http.Request) {
    // 1. 验证客户端 API key（可选）
    if !validateClientKey(r) {
        http.Error(w, "Unauthorized", 401)
        return
    }
    
    // 2. 解析请求，确定目标 Provider
    var req OpenAIRequest
    json.NewDecoder(r.Body).Decode(&req)
    provider := selectProvider(req.Model)
    
    // 3. 获取 OAuth token
    token, _ := getToken(provider)
    
    // 4. 转换请求格式
    providerReq := transformRequest(provider, &req)
    
    // 5. 注入认证头，转发请求
    proxyReq := buildProxyRequest(provider, providerReq, token)
    resp, _ := http.DefaultClient.Do(proxyReq)
    
    // 6. 转换响应格式
    openaiResp := transformResponse(provider, resp)
    json.NewEncoder(w).Encode(openaiResp)
}
```

---

## 总结

CLIProxyAPI 的核心技术实现包括：

1. **OAuth 认证复用**: 模拟官方 CLI 工具的 OAuth 流程，获取并管理 access/refresh token
2. **协议转换层**: 在 OpenAI 兼容格式与各 Provider 原生格式之间双向转换
3. **SSE 流处理**: 实时转换流式响应格式
4. **Token 生命周期管理**: 自动刷新、多账户轮换、故障转移
5. **安全分层**: 可选的客户端 API key 验证 + OAuth token 注入

这是一个精巧的中间件设计，通过合法复用官方 CLI 工具的认证机制，让订阅用户能够以 API 方式使用其 AI 服务额度。
