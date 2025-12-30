# UnifyAI Proxy API 参考

## OpenAI 兼容端点

| 端点 | 方法 | 描述 |
|------|------|------|
| `/v1/chat/completions` | POST | 聊天补全（主要端点） |
| `/v1/models` | GET | 列出可用模型 |
| `/v1/embeddings` | POST | 文本嵌入（如支持） |

## 管理端点

| 端点 | 方法 | 描述 |
|------|------|------|
| `/management.html` | GET | Web 管理界面 |
| `/health` | GET | 健康检查 |
| `/metrics` | GET | 性能指标 |

---

## 请求示例

### 基础聊天请求

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

### 响应示例

```json
{
  "id": "chatcmpl-123",
  "object": "chat.completion",
  "created": 1694268190,
  "model": "claude-sonnet-4-5-20250929",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Hello! How can I help you today?"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 10,
    "completion_tokens": 20,
    "total_tokens": 30
  }
}
```

---

### 流式响应请求

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

### 流式响应示例

```
data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1694268190,"model":"claude-sonnet-4-5-20250929","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1694268190,"model":"claude-sonnet-4-5-20250929","choices":[{"index":0,"delta":{"content":"In"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1694268190,"model":"claude-sonnet-4-5-20250929","choices":[{"index":0,"delta":{"content":" the"},"finish_reason":null}]}

data: [DONE]
```

---

### 带 System Prompt 的请求

```bash
curl -X POST http://127.0.0.1:8317/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "claude-sonnet-4-5-20250929",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "What is 2+2?"}
    ]
  }'
```

---

### 多模态请求（带图片）

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

### 多轮对话

```bash
curl -X POST http://127.0.0.1:8317/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "claude-sonnet-4-5-20250929",
    "messages": [
      {"role": "user", "content": "My name is Alice."},
      {"role": "assistant", "content": "Hello Alice! Nice to meet you."},
      {"role": "user", "content": "What is my name?"}
    ]
  }'
```

---

## 请求参数

| 参数 | 类型 | 必需 | 说明 |
|------|------|------|------|
| `model` | string | 是 | 模型名称 |
| `messages` | array | 是 | 消息数组 |
| `stream` | boolean | 否 | 是否流式响应，默认 false |
| `temperature` | number | 否 | 温度参数，0-2 |
| `max_tokens` | integer | 否 | 最大输出 token 数 |
| `top_p` | number | 否 | Top-p 采样 |
| `stop` | string/array | 否 | 停止序列 |

---

## 错误响应

### 401 Unauthorized

```json
{
  "error": {
    "message": "Invalid API key",
    "type": "authentication_error",
    "code": "invalid_api_key"
  }
}
```

### 404 Model Not Found

```json
{
  "error": {
    "message": "Model not found: gpt-5",
    "type": "invalid_request_error",
    "code": "model_not_found"
  }
}
```

### 429 Rate Limited

```json
{
  "error": {
    "message": "Rate limit exceeded",
    "type": "rate_limit_error",
    "code": "rate_limit_exceeded"
  }
}
```

---

## 支持的模型

### Claude 模型

- `claude-sonnet-4-5-20250929`
- `claude-opus-4-20250514`
- `claude-3-5-sonnet-20241022`
- `claude-3-5-haiku-20241022`

### Gemini 模型

- `gemini-2.5-pro`
- `gemini-2.5-flash`
- `gemini-2.0-flash`

### OpenAI Codex 模型

- `gpt-5`
- `gpt-5-codex`
- `gpt-4o`

### 模型别名

通过 `model-mapping` 配置，可以使用自定义别名：

```yaml
model-mapping:
  gpt-4: claude-sonnet-4-5-20250929
```
