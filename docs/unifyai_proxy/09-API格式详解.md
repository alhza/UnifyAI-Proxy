# UnifyAI Proxy API 格式详解

本文档详细分析 OpenAI、Claude、Gemini 三大 API 的请求/响应格式差异，为协议转换实现提供精确参考。

---

## 一、端点对比

| Provider | 端点 | 方法 |
|----------|------|------|
| OpenAI | `https://api.openai.com/v1/chat/completions` | POST |
| Claude | `https://api.anthropic.com/v1/messages` | POST |
| Gemini | `https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent` | POST |
| Gemini (流式) | `https://generativelanguage.googleapis.com/v1beta/models/{model}:streamGenerateContent` | POST |

---

## 二、认证头对比

### OpenAI
```http
Authorization: Bearer sk-xxxxxxxxxxxxxxxx
Content-Type: application/json
```

### Claude
```http
x-api-key: sk-ant-api03-xxxxxxxx
anthropic-version: 2023-06-01
Content-Type: application/json
```

OAuth 模式（CLI 工具）：
```http
Authorization: Bearer sk-ant-oat01-xxxxxxxx
anthropic-version: 2023-06-01
anthropic-beta: messages-2024-01-01
User-Agent: claude-code/1.0.0
X-Client-Type: cli
```

### Gemini
```http
x-goog-api-key: AIzaSyxxxxxxxxxxxxxxxxx
Content-Type: application/json
```

---

## 三、请求体格式详解

### 3.1 OpenAI 请求格式

```json
{
  "model": "gpt-4o",
  "messages": [
    {
      "role": "system",
      "content": "You are a helpful assistant."
    },
    {
      "role": "user", 
      "content": "Hello, how are you?"
    },
    {
      "role": "assistant",
      "content": "I'm doing well, thank you!"
    },
    {
      "role": "user",
      "content": "What's the weather like?"
    }
  ],
  "temperature": 0.7,
  "max_tokens": 1000,
  "top_p": 1.0,
  "frequency_penalty": 0,
  "presence_penalty": 0,
  "stream": true,
  "stop": ["\n\n"]
}
```

**关键特点：**
- `messages` 数组包含完整对话历史
- `system` 角色消息在 messages 数组内
- 支持三种角色：`system`、`user`、`assistant`
- `stream: true` 启用 SSE 流式响应

### 3.2 Claude 请求格式

```json
{
  "model": "claude-sonnet-4-5-20250929",
  "system": "You are a helpful assistant.",
  "messages": [
    {
      "role": "user",
      "content": "Hello, how are you?"
    },
    {
      "role": "assistant", 
      "content": "I'm doing well, thank you!"
    },
    {
      "role": "user",
      "content": "What's the weather like?"
    }
  ],
  "max_tokens": 1024,
  "temperature": 0.7,
  "top_p": 1.0,
  "stream": true,
  "stop_sequences": ["\n\n"]
}
```

**关键特点：**
- `system` 是独立的顶级字段，不在 messages 数组中
- messages 只包含 `user` 和 `assistant` 角色
- `max_tokens` 是必需参数
- 使用 `stop_sequences` 而非 `stop`

### 3.3 Gemini 请求格式

```json
{
  "contents": [
    {
      "role": "user",
      "parts": [
        {
          "text": "Hello, how are you?"
        }
      ]
    },
    {
      "role": "model",
      "parts": [
        {
          "text": "I'm doing well, thank you!"
        }
      ]
    },
    {
      "role": "user",
      "parts": [
        {
          "text": "What's the weather like?"
        }
      ]
    }
  ],
  "systemInstruction": {
    "parts": [
      {
        "text": "You are a helpful assistant."
      }
    ]
  },
  "generationConfig": {
    "temperature": 0.7,
    "topP": 1.0,
    "maxOutputTokens": 1024,
    "stopSequences": ["\n\n"]
  }
}
```

**关键特点：**
- 使用 `contents` 数组而非 `messages`
- 每条消息使用 `parts` 数组包装内容
- 角色使用 `model` 而非 `assistant`
- System prompt 使用 `systemInstruction` 字段
- 生成参数在 `generationConfig` 对象中
- 参数名使用 camelCase（如 `maxOutputTokens`）

---

## 四、请求格式转换矩阵

| 字段 | OpenAI | Claude | Gemini |
|------|--------|--------|--------|
| 消息数组 | `messages` | `messages` | `contents` |
| 消息内容 | `content` (string/array) | `content` (string/array) | `parts` (array) |
| System Prompt | `messages[role=system]` | `system` (顶级字段) | `systemInstruction` |
| 助手角色 | `assistant` | `assistant` | `model` |
| 最大 Token | `max_tokens` | `max_tokens` (必需) | `generationConfig.maxOutputTokens` |
| 温度 | `temperature` | `temperature` | `generationConfig.temperature` |
| Top P | `top_p` | `top_p` | `generationConfig.topP` |
| 停止序列 | `stop` | `stop_sequences` | `generationConfig.stopSequences` |
| 流式开关 | `stream: true` | `stream: true` | 使用不同端点 |

---

## 五、多模态内容格式

### 5.1 OpenAI 图片格式

```json
{
  "role": "user",
  "content": [
    {
      "type": "text",
      "text": "What's in this image?"
    },
    {
      "type": "image_url",
      "image_url": {
        "url": "data:image/jpeg;base64,/9j/4AAQSkZJRg...",
        "detail": "high"
      }
    }
  ]
}
```

### 5.2 Claude 图片格式

```json
{
  "role": "user",
  "content": [
    {
      "type": "text",
      "text": "What's in this image?"
    },
    {
      "type": "image",
      "source": {
        "type": "base64",
        "media_type": "image/jpeg",
        "data": "/9j/4AAQSkZJRg..."
      }
    }
  ]
}
```

### 5.3 Gemini 图片格式

```json
{
  "role": "user",
  "parts": [
    {
      "text": "What's in this image?"
    },
    {
      "inline_data": {
        "mime_type": "image/jpeg",
        "data": "/9j/4AAQSkZJRg..."
      }
    }
  ]
}
```

### 5.4 图片格式转换对照

| 字段 | OpenAI | Claude | Gemini |
|------|--------|--------|--------|
| 类型标识 | `type: "image_url"` | `type: "image"` | 无（直接用 `inline_data`） |
| 数据容器 | `image_url.url` | `source` | `inline_data` |
| Base64 前缀 | `data:image/jpeg;base64,` | 无 | 无 |
| MIME 类型 | URL 中包含 | `media_type` | `mime_type` |
| 原始数据 | URL 后半部分 | `data` | `data` |

---

## 六、响应格式详解

### 6.1 OpenAI 非流式响应

```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion",
  "created": 1694268190,
  "model": "gpt-4o",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Hello! I'm doing well, thank you for asking."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 25,
    "completion_tokens": 12,
    "total_tokens": 37
  }
}
```

### 6.2 Claude 非流式响应

```json
{
  "id": "msg_01XFDUDYJgAACzvnptvVoYEL",
  "type": "message",
  "role": "assistant",
  "content": [
    {
      "type": "text",
      "text": "Hello! I'm doing well, thank you for asking."
    }
  ],
  "model": "claude-sonnet-4-5-20250929",
  "stop_reason": "end_turn",
  "stop_sequence": null,
  "usage": {
    "input_tokens": 25,
    "output_tokens": 12
  }
}
```

### 6.3 Gemini 非流式响应

```json
{
  "candidates": [
    {
      "content": {
        "parts": [
          {
            "text": "Hello! I'm doing well, thank you for asking."
          }
        ],
        "role": "model"
      },
      "finishReason": "STOP",
      "index": 0
    }
  ],
  "usageMetadata": {
    "promptTokenCount": 25,
    "candidatesTokenCount": 12,
    "totalTokenCount": 37
  },
  "modelVersion": "gemini-2.5-flash"
}
```

### 6.4 响应格式转换对照

| 字段 | OpenAI | Claude | Gemini |
|------|--------|--------|--------|
| 响应 ID | `id` | `id` | `responseId` (流式) |
| 内容容器 | `choices[].message` | `content[]` | `candidates[].content` |
| 文本内容 | `message.content` | `content[].text` | `content.parts[].text` |
| 完成原因 | `finish_reason` | `stop_reason` | `finishReason` |
| 停止值 | `"stop"` | `"end_turn"` | `"STOP"` |
| 输入 Token | `usage.prompt_tokens` | `usage.input_tokens` | `usageMetadata.promptTokenCount` |
| 输出 Token | `usage.completion_tokens` | `usage.output_tokens` | `usageMetadata.candidatesTokenCount` |

---

## 七、SSE 流式响应格式详解

### 7.1 OpenAI SSE 格式

**事件格式：** 纯 `data:` 行，无 `event:` 类型

```
data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1694268190,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1694268190,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1694268190,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1694268190,"model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

**关键特点：**
- 使用 `delta` 对象传递增量内容
- 首个 chunk 包含 `role`
- 后续 chunk 只有 `content`
- 最后一个 chunk 的 `delta` 为空，`finish_reason` 为 `"stop"`
- 以 `data: [DONE]` 结束

### 7.2 Claude SSE 格式

**事件格式：** 使用 `event:` + `data:` 组合

```
event: message_start
data: {"type":"message_start","message":{"id":"msg_01XFDUDYJgAACzvnptvVoYEL","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-5-20250929","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":25,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: ping
data: {"type":"ping"}

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

**事件类型说明：**

| 事件类型 | 说明 | 包含数据 |
|----------|------|----------|
| `message_start` | 消息开始 | 完整 Message 对象（content 为空） |
| `content_block_start` | 内容块开始 | index, content_block 类型 |
| `ping` | 心跳保活 | 无实际内容 |
| `content_block_delta` | 内容增量 | index, delta.text |
| `content_block_stop` | 内容块结束 | index |
| `message_delta` | 消息元数据更新 | stop_reason, usage |
| `message_stop` | 消息结束 | 无 |

### 7.3 Gemini SSE 格式

**事件格式：** 返回 JSON 数组或多个 JSON 对象

```
data: {"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"},"index":0}],"usageMetadata":{"promptTokenCount":25},"modelVersion":"gemini-2.5-flash","responseId":"abc123"}

data: {"candidates":[{"content":{"parts":[{"text":"!"}],"role":"model"},"index":0}],"modelVersion":"gemini-2.5-flash","responseId":"abc123"}

data: {"candidates":[{"content":{"parts":[{"text":" How can I help you?"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":25,"candidatesTokenCount":8,"totalTokenCount":33},"modelVersion":"gemini-2.5-flash","responseId":"abc123"}
```

**关键特点：**
- 每个 chunk 是完整的响应结构
- 使用 `responseId` 关联同一响应的所有 chunk
- `finishReason` 在最后一个 chunk 中出现
- 无特殊结束标记

---

## 八、SSE 转换实现要点

### 8.1 Claude → OpenAI SSE 转换

```go
func convertClaudeSSEToOpenAI(event *ClaudeSSEEvent, msgID string) *OpenAISSEChunk {
    switch event.Type {
    case "message_start":
        // 生成首个 chunk，包含 role
        return &OpenAISSEChunk{
            ID:      msgID,
            Object:  "chat.completion.chunk",
            Created: time.Now().Unix(),
            Model:   event.Message.Model,
            Choices: []ChunkChoice{{
                Index: 0,
                Delta: Delta{Role: "assistant", Content: ""},
                FinishReason: nil,
            }},
        }
    
    case "content_block_delta":
        // 转换内容增量
        return &OpenAISSEChunk{
            ID:      msgID,
            Object:  "chat.completion.chunk",
            Created: time.Now().Unix(),
            Choices: []ChunkChoice{{
                Index: 0,
                Delta: Delta{Content: event.Delta.Text},
                FinishReason: nil,
            }},
        }
    
    case "message_delta":
        // 转换完成状态
        finishReason := "stop"
        if event.Delta.StopReason == "max_tokens" {
            finishReason = "length"
        }
        return &OpenAISSEChunk{
            ID:      msgID,
            Object:  "chat.completion.chunk",
            Created: time.Now().Unix(),
            Choices: []ChunkChoice{{
                Index: 0,
                Delta: Delta{},
                FinishReason: &finishReason,
            }},
        }
    
    case "ping", "content_block_start", "content_block_stop", "message_stop":
        // 忽略这些事件
        return nil
    }
    return nil
}
```

### 8.2 Gemini → OpenAI SSE 转换

```go
func convertGeminiSSEToOpenAI(chunk *GeminiResponse, msgID string, isFirst bool) *OpenAISSEChunk {
    if len(chunk.Candidates) == 0 {
        return nil
    }
    
    candidate := chunk.Candidates[0]
    text := ""
    if len(candidate.Content.Parts) > 0 {
        text = candidate.Content.Parts[0].Text
    }
    
    result := &OpenAISSEChunk{
        ID:      msgID,
        Object:  "chat.completion.chunk",
        Created: time.Now().Unix(),
        Model:   chunk.ModelVersion,
        Choices: []ChunkChoice{{
            Index: 0,
            Delta: Delta{Content: text},
        }},
    }
    
    // 首个 chunk 添加 role
    if isFirst {
        result.Choices[0].Delta.Role = "assistant"
    }
    
    // 检查完成状态
    if candidate.FinishReason == "STOP" {
        finishReason := "stop"
        result.Choices[0].FinishReason = &finishReason
    } else if candidate.FinishReason == "MAX_TOKENS" {
        finishReason := "length"
        result.Choices[0].FinishReason = &finishReason
    }
    
    return result
}
```

### 8.3 完成原因映射

| OpenAI | Claude | Gemini |
|--------|--------|--------|
| `stop` | `end_turn` | `STOP` |
| `length` | `max_tokens` | `MAX_TOKENS` |
| `content_filter` | - | `SAFETY` |
| `tool_calls` | `tool_use` | `TOOL_CALL` |

---

## 九、请求转换完整实现

### 9.1 OpenAI → Claude 转换

```go
type OpenAIRequest struct {
    Model       string          `json:"model"`
    Messages    []OpenAIMessage `json:"messages"`
    MaxTokens   int             `json:"max_tokens,omitempty"`
    Temperature float64         `json:"temperature,omitempty"`
    TopP        float64         `json:"top_p,omitempty"`
    Stream      bool            `json:"stream,omitempty"`
    Stop        []string        `json:"stop,omitempty"`
}

type ClaudeRequest struct {
    Model         string          `json:"model"`
    System        string          `json:"system,omitempty"`
    Messages      []ClaudeMessage `json:"messages"`
    MaxTokens     int             `json:"max_tokens"`
    Temperature   float64         `json:"temperature,omitempty"`
    TopP          float64         `json:"top_p,omitempty"`
    Stream        bool            `json:"stream,omitempty"`
    StopSequences []string        `json:"stop_sequences,omitempty"`
}

func ConvertOpenAIToClaude(req *OpenAIRequest) *ClaudeRequest {
    claudeReq := &ClaudeRequest{
        Model:         mapModelToClaude(req.Model),
        MaxTokens:     req.MaxTokens,
        Temperature:   req.Temperature,
        TopP:          req.TopP,
        Stream:        req.Stream,
        StopSequences: req.Stop,
    }
    
    // 默认 max_tokens（Claude 必需）
    if claudeReq.MaxTokens == 0 {
        claudeReq.MaxTokens = 4096
    }
    
    // 提取 system message，转换其他消息
    for _, msg := range req.Messages {
        if msg.Role == "system" {
            claudeReq.System = extractTextContent(msg.Content)
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

### 9.2 OpenAI → Gemini 转换

```go
type GeminiRequest struct {
    Contents          []GeminiContent    `json:"contents"`
    SystemInstruction *GeminiContent     `json:"systemInstruction,omitempty"`
    GenerationConfig  *GenerationConfig  `json:"generationConfig,omitempty"`
}

type GeminiContent struct {
    Role  string       `json:"role,omitempty"`
    Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
    Text       string      `json:"text,omitempty"`
    InlineData *InlineData `json:"inline_data,omitempty"`
}

type GenerationConfig struct {
    Temperature     float64  `json:"temperature,omitempty"`
    TopP            float64  `json:"topP,omitempty"`
    MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
    StopSequences   []string `json:"stopSequences,omitempty"`
}

func ConvertOpenAIToGemini(req *OpenAIRequest) *GeminiRequest {
    geminiReq := &GeminiRequest{
        GenerationConfig: &GenerationConfig{
            Temperature:     req.Temperature,
            TopP:            req.TopP,
            MaxOutputTokens: req.MaxTokens,
            StopSequences:   req.Stop,
        },
    }
    
    for _, msg := range req.Messages {
        if msg.Role == "system" {
            // System prompt 转为 systemInstruction
            geminiReq.SystemInstruction = &GeminiContent{
                Parts: []GeminiPart{{Text: extractTextContent(msg.Content)}},
            }
        } else {
            // 转换角色名
            role := msg.Role
            if role == "assistant" {
                role = "model"
            }
            
            geminiReq.Contents = append(geminiReq.Contents, GeminiContent{
                Role:  role,
                Parts: convertToGeminiParts(msg.Content),
            })
        }
    }
    
    return geminiReq
}

func convertToGeminiParts(content interface{}) []GeminiPart {
    switch c := content.(type) {
    case string:
        return []GeminiPart{{Text: c}}
    case []interface{}:
        var parts []GeminiPart
        for _, item := range c {
            if m, ok := item.(map[string]interface{}); ok {
                if m["type"] == "text" {
                    parts = append(parts, GeminiPart{Text: m["text"].(string)})
                } else if m["type"] == "image_url" {
                    // 转换图片格式
                    imageURL := m["image_url"].(map[string]interface{})
                    url := imageURL["url"].(string)
                    mimeType, data := parseDataURL(url)
                    parts = append(parts, GeminiPart{
                        InlineData: &InlineData{
                            MimeType: mimeType,
                            Data:     data,
                        },
                    })
                }
            }
        }
        return parts
    }
    return nil
}
```

---

## 十、错误响应格式

### 10.1 OpenAI 错误格式

```json
{
  "error": {
    "message": "Invalid API key provided",
    "type": "invalid_request_error",
    "param": null,
    "code": "invalid_api_key"
  }
}
```

### 10.2 Claude 错误格式

```json
{
  "type": "error",
  "error": {
    "type": "authentication_error",
    "message": "Invalid API key"
  }
}
```

### 10.3 Gemini 错误格式

```json
{
  "error": {
    "code": 401,
    "message": "API key not valid. Please pass a valid API key.",
    "status": "UNAUTHENTICATED"
  }
}
```

### 10.4 错误码映射

| HTTP 状态码 | OpenAI type | Claude type | Gemini status |
|-------------|-------------|-------------|---------------|
| 400 | `invalid_request_error` | `invalid_request_error` | `INVALID_ARGUMENT` |
| 401 | `invalid_request_error` | `authentication_error` | `UNAUTHENTICATED` |
| 403 | `invalid_request_error` | `permission_error` | `PERMISSION_DENIED` |
| 404 | `invalid_request_error` | `not_found_error` | `NOT_FOUND` |
| 429 | `rate_limit_error` | `rate_limit_error` | `RESOURCE_EXHAUSTED` |
| 500 | `server_error` | `api_error` | `INTERNAL` |
| 529 | `overloaded_error` | `overloaded_error` | `UNAVAILABLE` |

---

## 十一、实现检查清单

### 请求转换
- [ ] System prompt 位置转换（messages 内 ↔ 顶级字段）
- [ ] 角色名映射（assistant ↔ model）
- [ ] 参数名转换（snake_case ↔ camelCase）
- [ ] 多模态内容格式转换
- [ ] 必需参数默认值填充（如 Claude 的 max_tokens）

### 响应转换
- [ ] 响应结构映射（choices ↔ candidates）
- [ ] 完成原因映射
- [ ] Token 使用量字段映射
- [ ] 错误响应格式统一

### SSE 流式转换
- [ ] 事件类型过滤（忽略 ping 等）
- [ ] 增量内容提取
- [ ] 首个 chunk 添加 role
- [ ] 结束标记处理（[DONE]）
- [ ] 完成原因转换

### 认证头
- [ ] API Key 格式转换
- [ ] 必需 Header 注入（anthropic-version 等）
- [ ] OAuth Token 注入
- [ ] User-Agent 伪装
