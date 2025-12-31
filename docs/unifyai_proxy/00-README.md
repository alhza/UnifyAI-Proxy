# UnifyAI Proxy 技术分析文档

本目录包含 UnifyAI Proxy 项目的技术分析和参考文档。

## 项目背景

UnifyAI Proxy 参考了 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 的设计思路，实现一个统一的 AI API 代理服务。

## 文档索引

| 文档 | 内容 |
|------|------|
| [01-项目概述](./01-项目概述.md) | 项目介绍、核心价值、快速开始 |
| [02-认证架构](./02-认证架构.md) | 双层认证机制、OAuth Token 详解 |
| [03-协议转换](./03-协议转换.md) | OpenAI/Claude/Gemini 格式转换、SSE 流处理 |
| [04-配置参考](./04-配置参考.md) | 完整配置示例、Docker/Systemd 部署 |
| [05-API参考](./05-API参考.md) | API 端点、请求示例 |
| [06-故障排除](./06-故障排除.md) | 常见错误、调试方法 |
| [07-源码架构](./07-源码架构.md) | 代码结构、核心接口、实现思路 |
| [08-生态与对比](./08-生态与对比.md) | 衍生项目、类似项目对比 |
| [09-API格式详解](./09-API格式详解.md) | OpenAI/Claude/Gemini API 格式深度对比、转换实现 |
| [10-Tool-Calling格式详解](./10-Tool-Calling格式详解.md) | Function Calling 格式转换、流式处理 |
| [11-OAuth认证流程详解](./11-OAuth认证流程详解.md) | PKCE、Token 管理、各 Provider OAuth 实现 |
| [12-实现路线图](./12-实现路线图.md) | 分阶段实现计划、核心数据结构、部署配置 |
| [14-架构设计分析](./14-架构设计分析.md) | 架构分层、设计模式与扩展性分析 |
| [15-任务拆解-基础设计](./15-任务拆解-基础设计.md) | 全局基础设计、接口契约、配置字段定义 |
| [16-任务A-网关与配置安全](./16-任务A-网关与配置安全.md) | 网关/中间件/配置/管理端与安全 |
| [17-任务B-认证账户与配额](./17-任务B-认证账户与配额.md) | OAuth/账户状态机/配额与调度 |
| [18-任务C-协议转换与Provider适配](./18-任务C-协议转换与Provider适配.md) | 协议转换/Provider 适配/流式与 Tool Calling |

## 核心价值

UnifyAI Proxy 是一个统一的 AI API 代理服务器，它将基于 OAuth 的 CLI 工具（Claude Code、Gemini CLI、OpenAI Codex、Qwen Code）包装并暴露为标准的 OpenAI 兼容 API 接口。

**核心价值**: 让用户可以使用订阅账户（如 Claude Pro/Max、Gemini Advanced）的额度来调用 API，而无需单独购买 API credits。

## 参考项目

- [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) - 原始参考项目
