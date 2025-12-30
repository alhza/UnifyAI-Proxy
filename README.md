# UnifyAI-Proxy

统一的 AI API 代理服务器，将 Claude Code、Gemini CLI、OpenAI Codex 等 CLI 工具的 OAuth 认证包装为标准 OpenAI 兼容 API。

## 核心价值

让用户可以使用订阅账户（如 Claude Pro/Max、Gemini Advanced）的额度来调用 API，而无需单独购买 API credits。

## 功能特性

- 🔐 **OAuth 认证复用** - 复用官方 CLI 工具的 OAuth 流程
- 🔄 **协议转换** - OpenAI 兼容格式与各 Provider 原生格式双向转换
- 📡 **SSE 流式支持** - 实时转换流式响应
- 🛠️ **Tool Calling** - 完整的 Function Calling 支持
- 👥 **多账户管理** - 配额轮换、故障转移

## 支持的 Provider

| Provider | 认证方式 | 状态 |
|----------|----------|------|
| Claude | OAuth (Claude Pro/Max) | 📋 计划中 |
| Gemini | Google OAuth | 📋 计划中 |
| OpenAI Codex | OAuth (ChatGPT Plus) | 📋 计划中 |

## 文档

详细技术文档请查看 [docs/unifyai_proxy/](./docs/unifyai_proxy/00-README.md)

## 快速开始

```bash
# 克隆项目
git clone https://github.com/alhza/UnifyAI-Proxy.git
cd UnifyAI-Proxy

# 待实现...
```

## 参考项目

- [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)

## License

MIT
