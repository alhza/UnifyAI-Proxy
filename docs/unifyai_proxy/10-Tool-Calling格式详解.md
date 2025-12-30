# UnifyAI Proxy Tool/Function Calling 格式详解

本文档详细分析 OpenAI、Claude、Gemini 三大 API 的 Tool/Function Calling 格式差异。

---

## 一、概念对照

| 概念 | OpenAI | Claude | Gemini |
|------|--------|--------|--------|
| 工具定义 | `tools` | `tools` | `tools` |
| 单个工具 | `function` | `custom` (type) | `functionDeclarations` |
| 工具调用 | `tool_calls` | `tool_use` (content block) | `functionCall` |
| 工具结果 | `tool` (role) | `tool_result` (content block) | `functionResponse` |

---

## 二、工具定义格式

### 2.1 OpenAI 工具定义

```json
{
  "model": "gpt-4o",
  "messages": [...],
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get the current weather in a given location",
        "parameters": {
          "type": "object",
          "properties": {
            "location": {
              "type": "string",
              "description": "The city and state, e.g. San Francisco, CA"
            },
            "unit": {
              "type": "string",
              "enum": ["celsius", "fahrenheit"]
            }
          },
          "required": ["location"]
        }
      }
    }
  ],
  "tool_choice": "auto"
}
```

### 2.2 Claude 工具定义

```json
{
  "model": "claude-sonnet-4-5-20250929",
  "messages": [...],
  "tools": [
    {
      "name": "get_weather",
      "description": "Get the current weather in a given location",
      "input_schema": {
        "type": "object",
        "properties": {
          "location": {
            "type": "string",
            "description": "The city and state, e.g. San Francisco, CA"
          },
          "unit": {
            "type": "string",
            "enum": ["celsius", "fahrenheit"]
          }
        },
        "required": ["location"]
      }
    }
  ],
  "tool_choice": {"type": "auto"}
}
```

### 2.3 Gemini 工具定义

```json
{
  "contents": [...],
  "tools": [
    {
      "functionDeclarations": [
        {
          "name": "get_weather",
          "description": "Get the current weather in a given location",
          "parameters": {
            "type": "object",
            "properties": {
              "location": {
                "type": "string",
                "description": "The city and state, e.g. San Francisco, CA"
              },
              "unit": {
                "type": "string",
                "enum": ["celsius", "fahrenheit"]
              }
            },
            "required": ["location"]
          }
        }
      ]
    }
  ],
  "toolConfig": {
    "functionCallingConfig": {
      "mode": "AUTO"
    }
  }
}
```

### 2.4 工具定义转换对照

| 字段 | OpenAI | Claude | Gemini |
|------|--------|--------|--------|
| 工具数组 | `tools` | `tools` | `tools` |
| 工具类型 | `type: "function"` | 无（隐式） | 无（用 `functionDeclarations`） |
| 函数容器 | `function` | 直接在工具对象中 | `functionDeclarations[]` |
| 函数名 | `function.name` | `name` | `name` |
| 描述 | `function.description` | `description` | `description` |
| 参数 Schema | `function.parameters` | `input_schema` | `parameters` |
| 工具选择 | `tool_choice` | `tool_choice` | `toolConfig.functionCallingConfig` |
| 自动模式 | `"auto"` | `{"type": "auto"}` | `{"mode": "AUTO"}` |
| 强制调用 | `{"type": "function", "function": {"name": "xxx"}}` | `{"type": "tool", "name": "xxx"}` | `{"mode": "ANY"}` |
| 禁用工具 | `"none"` | `{"type": "none"}` | `{"mode": "NONE"}` |

---

## 三、工具调用响应格式

### 3.1 OpenAI 工具调用响应

```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": null,
        "tool_calls": [
          {
            "id": "call_abc123",
            "type": "function",
            "function": {
              "name": "get_weather",
              "arguments": "{\"location\": \"San Francisco, CA\", \"unit\": \"celsius\"}"
            }
          }
        ]
      },
      "finish_reason": "tool_calls"
    }
  ]
}
```

### 3.2 Claude 工具调用响应

```json
{
  "id": "msg_01XFDUDYJgAACzvnptvVoYEL",
  "type": "message",
  "role": "assistant",
  "content": [
    {
      "type": "text",
      "text": "I'll check the weather for you."
    },
    {
      "type": "tool_use",
      "id": "toolu_01A09q90qw90lq917835lgs",
      "name": "get_weather",
      "input": {
        "location": "San Francisco, CA",
        "unit": "celsius"
      }
    }
  ],
  "stop_reason": "tool_use"
}
```

### 3.3 Gemini 工具调用响应

```json
{
  "candidates": [
    {
      "content": {
        "parts": [
          {
            "functionCall": {
              "name": "get_weather",
              "args": {
                "location": "San Francisco, CA",
                "unit": "celsius"
              }
            }
          }
        ],
        "role": "model"
      },
      "finishReason": "TOOL_CALL"
    }
  ]
}
```

### 3.4 工具调用响应转换对照

| 字段 | OpenAI | Claude | Gemini |
|------|--------|--------|--------|
| 调用容器 | `message.tool_calls[]` | `content[]` (type=tool_use) | `content.parts[].functionCall` |
| 调用 ID | `tool_calls[].id` | `content[].id` | 无（需自行生成） |
| 函数名 | `tool_calls[].function.name` | `content[].name` | `functionCall.name` |
| 参数 | `tool_calls[].function.arguments` (JSON string) | `content[].input` (object) | `functionCall.args` (object) |
| 完成原因 | `"tool_calls"` | `"tool_use"` | `"TOOL_CALL"` |

**关键差异：**
- OpenAI 的 `arguments` 是 JSON 字符串，需要 `JSON.parse()`
- Claude 和 Gemini 的参数直接是对象
- Gemini 没有调用 ID，转换时需要生成

---

## 四、工具结果提交格式

### 4.1 OpenAI 工具结果

```json
{
  "model": "gpt-4o",
  "messages": [
    {"role": "user", "content": "What's the weather in SF?"},
    {
      "role": "assistant",
      "content": null,
      "tool_calls": [
        {
          "id": "call_abc123",
          "type": "function",
          "function": {
            "name": "get_weather",
            "arguments": "{\"location\": \"San Francisco, CA\"}"
          }
        }
      ]
    },
    {
      "role": "tool",
      "tool_call_id": "call_abc123",
      "content": "{\"temperature\": 72, \"unit\": \"fahrenheit\", \"condition\": \"sunny\"}"
    }
  ]
}
```

### 4.2 Claude 工具结果

```json
{
  "model": "claude-sonnet-4-5-20250929",
  "messages": [
    {"role": "user", "content": "What's the weather in SF?"},
    {
      "role": "assistant",
      "content": [
        {"type": "text", "text": "I'll check the weather for you."},
        {
          "type": "tool_use",
          "id": "toolu_01A09q90qw90lq917835lgs",
          "name": "get_weather",
          "input": {"location": "San Francisco, CA"}
        }
      ]
    },
    {
      "role": "user",
      "content": [
        {
          "type": "tool_result",
          "tool_use_id": "toolu_01A09q90qw90lq917835lgs",
          "content": "{\"temperature\": 72, \"unit\": \"fahrenheit\", \"condition\": \"sunny\"}"
        }
      ]
    }
  ]
}
```

### 4.3 Gemini 工具结果

```json
{
  "contents": [
    {"role": "user", "parts": [{"text": "What's the weather in SF?"}]},
    {
      "role": "model",
      "parts": [
        {
          "functionCall": {
            "name": "get_weather",
            "args": {"location": "San Francisco, CA"}
          }
        }
      ]
    },
    {
      "role": "user",
      "parts": [
        {
          "functionResponse": {
            "name": "get_weather",
            "response": {
              "temperature": 72,
              "unit": "fahrenheit",
              "condition": "sunny"
            }
          }
        }
      ]
    }
  ]
}
```

### 4.4 工具结果转换对照

| 字段 | OpenAI | Claude | Gemini |
|------|--------|--------|--------|
| 结果角色 | `role: "tool"` | `role: "user"` | `role: "user"` |
| 结果类型 | 无（角色即类型） | `type: "tool_result"` | 无（用 `functionResponse`） |
| 关联 ID | `tool_call_id` | `tool_use_id` | 无（用 `name` 关联） |
| 结果内容 | `content` (string) | `content` (string/array) | `response` (object) |

**关键差异：**
- OpenAI 使用专门的 `tool` 角色
- Claude 和 Gemini 使用 `user` 角色包装工具结果
- Gemini 通过函数名关联，无需 ID

---

## 五、工具调用转换实现

### 5.1 OpenAI → Claude 工具定义转换

```go
func ConvertOpenAIToolsToClaude(tools []OpenAITool) []ClaudeTool {
    var claudeTools []ClaudeTool
    for _, tool := range tools {
        if tool.Type == "function" {
            claudeTools = append(claudeTools, ClaudeTool{
                Name:        tool.Function.Name,
                Description: tool.Function.Description,
                InputSchema: tool.Function.Parameters,
            })
        }
    }
    return claudeTools
}

func ConvertOpenAIToolChoiceToClaude(choice interface{}) *ClaudeToolChoice {
    switch c := choice.(type) {
    case string:
        if c == "auto" {
            return &ClaudeToolChoice{Type: "auto"}
        } else if c == "none" {
            return &ClaudeToolChoice{Type: "none"}
        } else if c == "required" {
            return &ClaudeToolChoice{Type: "any"}
        }
    case map[string]interface{}:
        if c["type"] == "function" {
            fn := c["function"].(map[string]interface{})
            return &ClaudeToolChoice{
                Type: "tool",
                Name: fn["name"].(string),
            }
        }
    }
    return &ClaudeToolChoice{Type: "auto"}
}
```

### 5.2 Claude → OpenAI 工具调用响应转换

```go
func ConvertClaudeToolCallToOpenAI(content []ClaudeContentBlock) []OpenAIToolCall {
    var toolCalls []OpenAIToolCall
    for _, block := range content {
        if block.Type == "tool_use" {
            // Claude 的 input 是对象，需要序列化为 JSON 字符串
            argsJSON, _ := json.Marshal(block.Input)
            toolCalls = append(toolCalls, OpenAIToolCall{
                ID:   block.ID,
                Type: "function",
                Function: OpenAIFunctionCall{
                    Name:      block.Name,
                    Arguments: string(argsJSON),
                },
            })
        }
    }
    return toolCalls
}
```

### 5.3 OpenAI → Gemini 工具定义转换

```go
func ConvertOpenAIToolsToGemini(tools []OpenAITool) []GeminiTool {
    var declarations []GeminiFunctionDeclaration
    for _, tool := range tools {
        if tool.Type == "function" {
            declarations = append(declarations, GeminiFunctionDeclaration{
                Name:        tool.Function.Name,
                Description: tool.Function.Description,
                Parameters:  tool.Function.Parameters,
            })
        }
    }
    return []GeminiTool{{FunctionDeclarations: declarations}}
}

func ConvertOpenAIToolChoiceToGemini(choice interface{}) *GeminiToolConfig {
    config := &GeminiToolConfig{
        FunctionCallingConfig: &FunctionCallingConfig{},
    }
    switch c := choice.(type) {
    case string:
        switch c {
        case "auto":
            config.FunctionCallingConfig.Mode = "AUTO"
        case "none":
            config.FunctionCallingConfig.Mode = "NONE"
        case "required":
            config.FunctionCallingConfig.Mode = "ANY"
        }
    case map[string]interface{}:
        // 强制调用特定函数
        config.FunctionCallingConfig.Mode = "ANY"
        if c["type"] == "function" {
            fn := c["function"].(map[string]interface{})
            config.FunctionCallingConfig.AllowedFunctionNames = []string{fn["name"].(string)}
        }
    }
    return config
}
```

### 5.4 Gemini → OpenAI 工具调用响应转换

```go
func ConvertGeminiToolCallToOpenAI(parts []GeminiPart) []OpenAIToolCall {
    var toolCalls []OpenAIToolCall
    for i, part := range parts {
        if part.FunctionCall != nil {
            // Gemini 没有调用 ID，需要生成
            callID := fmt.Sprintf("call_%s", generateRandomID())
            argsJSON, _ := json.Marshal(part.FunctionCall.Args)
            toolCalls = append(toolCalls, OpenAIToolCall{
                ID:   callID,
                Type: "function",
                Function: OpenAIFunctionCall{
                    Name:      part.FunctionCall.Name,
                    Arguments: string(argsJSON),
                },
            })
        }
    }
    return toolCalls
}
```

### 5.5 OpenAI Tool 消息 → Claude/Gemini 转换

```go
// OpenAI tool 消息转 Claude tool_result
func ConvertOpenAIToolMessageToClaude(msg OpenAIMessage) ClaudeContentBlock {
    return ClaudeContentBlock{
        Type:      "tool_result",
        ToolUseID: msg.ToolCallID,
        Content:   msg.Content,
    }
}

// OpenAI tool 消息转 Gemini functionResponse
func ConvertOpenAIToolMessageToGemini(msg OpenAIMessage, toolName string) GeminiPart {
    var response interface{}
    json.Unmarshal([]byte(msg.Content), &response)
    return GeminiPart{
        FunctionResponse: &GeminiFunctionResponse{
            Name:     toolName,
            Response: response,
        },
    }
}
```

---

## 六、流式工具调用处理

### 6.1 OpenAI 流式工具调用

```
data: {"id":"chatcmpl-abc","choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-abc","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"lo"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-abc","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"cation"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-abc","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\": \"SF\"}"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-abc","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]
```

**特点：**
- 首个 chunk 包含 `id`、`type`、`name`
- 后续 chunk 只有 `arguments` 的增量片段
- 需要累积拼接 `arguments`

### 6.2 Claude 流式工具调用

```
event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01A09q90qw90lq917835lgs","name":"get_weather"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"lo"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"cation"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\": \"SF\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}
```

**特点：**
- `content_block_start` 包含 `id` 和 `name`
- `input_json_delta` 传递参数增量
- 需要累积拼接 `partial_json`

### 6.3 流式工具调用转换状态机

```go
type ToolCallAccumulator struct {
    ID        string
    Name      string
    Arguments strings.Builder
}

func (a *ToolCallAccumulator) ProcessClaudeEvent(event *ClaudeSSEEvent) *OpenAISSEChunk {
    switch event.Type {
    case "content_block_start":
        if event.ContentBlock.Type == "tool_use" {
            a.ID = event.ContentBlock.ID
            a.Name = event.ContentBlock.Name
            // 返回首个 tool_calls chunk
            return &OpenAISSEChunk{
                Choices: []ChunkChoice{{
                    Delta: Delta{
                        ToolCalls: []ToolCallDelta{{
                            Index: 0,
                            ID:    a.ID,
                            Type:  "function",
                            Function: FunctionDelta{
                                Name:      a.Name,
                                Arguments: "",
                            },
                        }},
                    },
                }},
            }
        }
    case "content_block_delta":
        if event.Delta.Type == "input_json_delta" {
            a.Arguments.WriteString(event.Delta.PartialJSON)
            // 返回 arguments 增量
            return &OpenAISSEChunk{
                Choices: []ChunkChoice{{
                    Delta: Delta{
                        ToolCalls: []ToolCallDelta{{
                            Index: 0,
                            Function: FunctionDelta{
                                Arguments: event.Delta.PartialJSON,
                            },
                        }},
                    },
                }},
            }
        }
    }
    return nil
}
```

---

## 七、并行工具调用

### 7.1 OpenAI 并行调用

```json
{
  "choices": [{
    "message": {
      "tool_calls": [
        {"id": "call_1", "function": {"name": "get_weather", "arguments": "{\"location\":\"SF\"}"}},
        {"id": "call_2", "function": {"name": "get_weather", "arguments": "{\"location\":\"NYC\"}"}}
      ]
    }
  }]
}
```

### 7.2 Claude 并行调用

```json
{
  "content": [
    {"type": "text", "text": "I'll check both locations."},
    {"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"location": "SF"}},
    {"type": "tool_use", "id": "toolu_2", "name": "get_weather", "input": {"location": "NYC"}}
  ]
}
```

### 7.3 Gemini 并行调用

```json
{
  "candidates": [{
    "content": {
      "parts": [
        {"functionCall": {"name": "get_weather", "args": {"location": "SF"}}},
        {"functionCall": {"name": "get_weather", "args": {"location": "NYC"}}}
      ]
    }
  }]
}
```

---

## 八、实现检查清单

### 工具定义转换
- [ ] `function.parameters` ↔ `input_schema` ↔ `parameters`
- [ ] `tool_choice` 格式转换
- [ ] 嵌套在 `function` 对象 vs 直接属性

### 工具调用响应转换
- [ ] `arguments` JSON 字符串 ↔ 对象
- [ ] 调用 ID 生成（Gemini 无 ID）
- [ ] `finish_reason` 映射

### 工具结果转换
- [ ] `role: "tool"` ↔ `role: "user"` + `tool_result`
- [ ] `tool_call_id` ↔ `tool_use_id` ↔ `name`
- [ ] 结果内容格式（string vs object）

### 流式处理
- [ ] 工具调用增量累积
- [ ] 并行调用索引处理
- [ ] 状态机管理
