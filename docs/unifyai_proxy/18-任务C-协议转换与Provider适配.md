# 任务 C：协议转换、Provider 适配与流式

前置：完成 `15-任务拆解-基础设计.md`（冲突以 15 为准）；依赖任务 A 的路由挂载/日志接口、任务 B 的选路与 token 获取接口。

## 目标
- 实现 OpenAI ↔ Provider 的请求/响应/SSE/Tool Calling 转换，Provider 适配与错误映射。

## 范围
- 模型路由：模型别名/黑名单/force-model-prefix、能力校验（多模态/Tool Calling/上下文长度）；预览回退逻辑。
- 请求转换：OpenAI → Claude/Gemini/Codex/Qwen，system 处理、多模态（text+image）、Tool Calling 定义与 tool_choice 映射。
- 响应转换：Provider → OpenAI，usage/错误统一。
- SSE：事件映射到 OpenAI delta，`[DONE]` 终止；错误事件中断；keepalive/首包重试。
- Tool Calling 流式：调用 ID 生成/保持、增量参数累积、并行处理。
- Provider 适配：HTTP 客户端（超时/代理）、头注入（User-Agent 伪装、版本/必需 headers）、错误分类。
- 扩展：OpenAI-compatible upstream、Vertex/API-key 直连、Amp 路由入口对齐配置。
- 观测：为日志/metrics 提供 hook，标注 provider/model/account（脱敏）。

## 交付物
- 包/文件：`internal/transform`（请求/响应/SSE/Tool Calling）、`internal/provider`（claude/gemini/codex/qwen/openai-compat 等）、错误类型映射。
- 公共接口：Transformer 工厂、Provider 接口实现，供任务 A 的路由注册。
- 自测：非流式/流式转换用例，多模态/工具调用用例，SSE 事件序列转换。
- 验收标准：
  - **模型路由**: 别名展开正确（支持多级，限制5级深度）；黑名单优先级高于别名；force-model-prefix 生效；能力校验拒绝不满足请求；preview 回退生效。
  - **协议转换**: OpenAI ↔ Claude/Gemini/Codex/Qwen 请求/响应正确转换；system 消息处理符合各 Provider 规范；多模态（text+image）正确映射；Tool Calling 定义与 tool_choice 转换符合 09/10。
  - **错误映射**（遵循任务 A 格式，补充 Provider 特定错误）：
    - Provider 401/403 → 401 authentication_error
    - Provider 400/422 → 400 invalid_request_error
    - Provider 429 → 429 rate_limit_error（含 Retry-After 头）
    - Provider 5xx → 502/503/504 server_error（根据具体情况）
    - 网络超时/连接失败 → 504 server_error
    - **错误响应示例**（能力不满足）：
      ```json
      {
        "error": {
          "message": "Model claude-sonnet-4-5-20250929 does not support tool_calling capability",
          "type": "invalid_request_error",
          "code": "capability_not_supported"
        }
      }
      ```
  - **SSE 流式**: 事件正确映射为 OpenAI delta 格式；流结束发送 `data: [DONE]`；错误事件立即中断流（不发送 [DONE]）；keepalive 心跳按配置触发；bootstrap 重试按配置执行。
  - **Tool Calling 流式**: 调用 ID 稳定生成（call_<uuid>）；增量参数正确累积；并行调用使用独立 CallState；流终止前验证 JSON 完整性。
  - **Provider 适配**: HTTP 客户端正确配置超时/代理；User-Agent 伪装生效；必需 headers 注入；错误正确分类（QuotaExceeded/RateLimited/AuthError/NetworkError/ServerError）。
  - **扩展 Provider**: openai-compatibility/vertex/amp 配置解析并生效（至少占位实现）。
  - **观测性指标**（补充任务 A/B 基础指标）：
    - `provider_requests_total{provider,model,status}` - Provider 请求计数
    - `provider_request_duration_seconds{provider,model}` - Provider 请求延迟
    - `streaming_duration_seconds{provider,model}` - SSE 流总时长
    - `streaming_events_total{provider,model,event_type}` - SSE 事件计数
    - `tool_calling_total{provider,model,status}` - Tool Calling 调用计数
    - `protocol_transform_errors_total{provider,direction}` - 协议转换错误计数

## 依赖/接口契约
- 必须消费的配置字段（见 15/04）：模型映射/黑名单/force-model-prefix、能力声明/预览回退、`openai-compatibility`、`vertex-api-key`、`ampcode`、`proxy-url`、`streaming`、`payload` 注入规则。
- 调用任务 B 的 AccountSelector/Token 获取；使用任务 A 的日志/metrics hook。
