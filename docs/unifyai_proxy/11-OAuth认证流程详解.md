# UnifyAI Proxy OAuth 认证流程详解

本文档深入分析 Claude Code、Gemini CLI、OpenAI Codex 的 OAuth 认证实现细节。

---

## 一、OAuth 2.0 基础概念

### 1.1 授权码流程 (Authorization Code Flow)

```
┌──────────┐                               ┌──────────────┐
│          │  1. 授权请求 (浏览器)          │              │
│   用户   │ ─────────────────────────────▶│   授权服务器  │
│          │                               │              │
│          │  2. 用户登录并授权             │              │
│          │ ◀─────────────────────────────│              │
└──────────┘                               └──────────────┘
     │                                            │
     │ 3. 重定向到回调 URL (带 code)               │
     ▼                                            │
┌──────────┐                                      │
│          │  4. 用 code 换 token                 │
│ CLI 工具 │ ─────────────────────────────────────┘
│          │
│          │  5. 返回 access_token + refresh_token
│          │ ◀─────────────────────────────────────
└──────────┘
```

### 1.2 PKCE 扩展 (Proof Key for Code Exchange)

公共客户端（如 CLI 工具）使用 PKCE 防止授权码拦截攻击：

```
1. 生成 code_verifier (随机字符串)
2. 计算 code_challenge = BASE64URL(SHA256(code_verifier))
3. 授权请求携带 code_challenge
4. Token 请求携带 code_verifier
5. 服务器验证 SHA256(code_verifier) == code_challenge
```

---

## 二、Claude Code OAuth 详解

### 2.1 OAuth 端点

| 端点 | URL |
|------|-----|
| 授权端点 | `https://claude.ai/oauth/authorize` |
| Token 端点 | `https://claude.ai/oauth/token` |
| 回调地址 | `http://localhost:54545/callback` |

### 2.2 客户端配置

```
Client ID: claude-code-cli
Redirect URI: http://localhost:54545/callback
Scopes: user:inference user:profile
```

### 2.3 授权请求参数

```http
GET https://claude.ai/oauth/authorize?
  client_id=claude-code-cli&
  redirect_uri=http://localhost:54545/callback&
  response_type=code&
  scope=user:inference%20user:profile&
  state=<random_state>&
  code_challenge=<PKCE_challenge>&
  code_challenge_method=S256
```

### 2.4 Token 请求

```http
POST https://claude.ai/oauth/token
Content-Type: application/x-www-form-urlencoded

grant_type=authorization_code&
code=<authorization_code>&
redirect_uri=http://localhost:54545/callback&
client_id=claude-code-cli&
code_verifier=<PKCE_verifier>
```

### 2.5 Token 响应

```json
{
  "access_token": "sk-ant-oat01-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "refresh_token": "sk-ant-ort01-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "token_type": "Bearer",
  "expires_in": 43200,
  "scope": "user:inference user:profile"
}
```

### 2.6 Token 刷新

```http
POST https://claude.ai/oauth/token
Content-Type: application/x-www-form-urlencoded

grant_type=refresh_token&
refresh_token=sk-ant-ort01-xxxxxxxx&
client_id=claude-code-cli
```

### 2.7 凭证存储

**文件位置：** `~/.claude/.credentials.json`

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

### 2.8 API 请求认证

```http
POST https://api.anthropic.com/v1/messages
Authorization: Bearer sk-ant-oat01-xxxxxxxx
anthropic-version: 2023-06-01
anthropic-beta: messages-2024-01-01
User-Agent: claude-code/1.0.0
X-Client-Type: cli
Content-Type: application/json
```

---

## 三、Gemini CLI OAuth 详解

### 3.1 OAuth 端点 (Google OAuth 2.0)

| 端点 | URL |
|------|-----|
| 授权端点 | `https://accounts.google.com/o/oauth2/v2/auth` |
| Token 端点 | `https://oauth2.googleapis.com/token` |
| 回调地址 | `http://localhost:PORT/callback` (动态端口) |

### 3.2 客户端配置

```
Client ID: <google_cloud_client_id>.apps.googleusercontent.com
Redirect URI: http://localhost:{dynamic_port}/callback
Scopes: 
  - https://www.googleapis.com/auth/generative-language
  - https://www.googleapis.com/auth/userinfo.email
  - openid
```

### 3.3 授权请求参数

```http
GET https://accounts.google.com/o/oauth2/v2/auth?
  client_id=<client_id>.apps.googleusercontent.com&
  redirect_uri=http://localhost:8085/callback&
  response_type=code&
  scope=https://www.googleapis.com/auth/generative-language%20openid%20email&
  state=<random_state>&
  code_challenge=<PKCE_challenge>&
  code_challenge_method=S256&
  access_type=offline&
  prompt=consent
```

**关键参数：**
- `access_type=offline`: 请求 refresh_token
- `prompt=consent`: 强制显示同意页面（确保获得 refresh_token）

### 3.4 Token 请求

```http
POST https://oauth2.googleapis.com/token
Content-Type: application/x-www-form-urlencoded

grant_type=authorization_code&
code=<authorization_code>&
redirect_uri=http://localhost:8085/callback&
client_id=<client_id>.apps.googleusercontent.com&
code_verifier=<PKCE_verifier>
```

### 3.5 Token 响应

```json
{
  "access_token": "ya29.a0AfH6SMBxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "refresh_token": "1//0exxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "token_type": "Bearer",
  "expires_in": 3599,
  "scope": "https://www.googleapis.com/auth/generative-language openid email",
  "id_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

### 3.6 凭证存储

**可能的位置：**
- `~/.gemini/credentials.json`
- `~/.config/gemini/credentials.json`
- 系统 Keychain (macOS)

```json
{
  "access_token": "ya29.a0AfH6SMBxxxxxxxx",
  "refresh_token": "1//0exxxxxxxx",
  "token_type": "Bearer",
  "expiry": "2024-01-15T10:30:00Z"
}
```

### 3.7 API 请求认证

**方式一：OAuth Token**
```http
POST https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent
Authorization: Bearer ya29.a0AfH6SMBxxxxxxxx
Content-Type: application/json
```

**方式二：API Key**
```http
POST https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent
x-goog-api-key: AIzaSyxxxxxxxxxxxxxxxxxxxxxxxxx
Content-Type: application/json
```

---

## 四、OpenAI Codex CLI OAuth 详解

### 4.1 OAuth 端点

| 端点 | URL |
|------|-----|
| 授权端点 | `https://auth0.openai.com/authorize` 或 `https://auth.openai.com/authorize` |
| Token 端点 | `https://auth0.openai.com/oauth/token` |
| 回调地址 | `http://localhost:1455/callback` |

### 4.2 客户端配置

```
Client ID: <openai_cli_client_id>
Redirect URI: http://localhost:1455/callback
Scopes: openid profile email offline_access
```

### 4.3 授权请求参数

```http
GET https://auth0.openai.com/authorize?
  client_id=<client_id>&
  redirect_uri=http://localhost:1455/callback&
  response_type=code&
  scope=openid%20profile%20email%20offline_access&
  state=<random_state>&
  code_challenge=<PKCE_challenge>&
  code_challenge_method=S256&
  audience=https://api.openai.com/v1
```

### 4.4 Token 响应

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "v1.xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "token_type": "Bearer",
  "expires_in": 86400,
  "id_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

### 4.5 凭证存储

**文件位置：** `~/.codex/auth.json`

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "refresh_token": "v1.xxxxxxxx",
  "expires_at": 1735689600,
  "token_type": "Bearer"
}
```

### 4.6 API 请求认证

```http
POST https://api.openai.com/v1/chat/completions
Authorization: Bearer eyJhbGciOiJSUzI1NiIs...
Content-Type: application/json
```

---

## 五、OAuth 实现代码

### 5.1 PKCE 生成

```go
import (
    "crypto/rand"
    "crypto/sha256"
    "encoding/base64"
)

func GeneratePKCE() (verifier, challenge string) {
    // 生成 43-128 字符的随机字符串
    b := make([]byte, 32)
    rand.Read(b)
    verifier = base64.RawURLEncoding.EncodeToString(b)
    
    // 计算 SHA256 哈希
    h := sha256.Sum256([]byte(verifier))
    challenge = base64.RawURLEncoding.EncodeToString(h[:])
    
    return verifier, challenge
}
```

### 5.2 本地回调服务器

```go
func StartCallbackServer(port int) (codeChan chan string, stateChan chan string, shutdown func()) {
    codeChan = make(chan string, 1)
    stateChan = make(chan string, 1)
    
    mux := http.NewServeMux()
    server := &http.Server{
        Addr:    fmt.Sprintf(":%d", port),
        Handler: mux,
    }
    
    mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
        code := r.URL.Query().Get("code")
        state := r.URL.Query().Get("state")
        
        if code != "" {
            codeChan <- code
            stateChan <- state
            w.Header().Set("Content-Type", "text/html")
            fmt.Fprintf(w, `
                <html>
                <body>
                    <h1>登录成功！</h1>
                    <p>您可以关闭此窗口。</p>
                    <script>window.close();</script>
                </body>
                </html>
            `)
        } else {
            errorMsg := r.URL.Query().Get("error")
            http.Error(w, "授权失败: "+errorMsg, http.StatusBadRequest)
        }
    })
    
    go server.ListenAndServe()
    
    shutdown = func() {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        server.Shutdown(ctx)
    }
    
    return codeChan, stateChan, shutdown
}
```

### 5.3 完整 OAuth 登录流程

```go
type OAuthConfig struct {
    AuthURL      string
    TokenURL     string
    ClientID     string
    RedirectURI  string
    Scopes       []string
    CallbackPort int
}

func PerformOAuthLogin(config *OAuthConfig) (*Token, error) {
    // 1. 生成 PKCE
    verifier, challenge := GeneratePKCE()
    state := generateRandomState()
    
    // 2. 启动回调服务器
    codeChan, stateChan, shutdown := StartCallbackServer(config.CallbackPort)
    defer shutdown()
    
    // 3. 构建授权 URL
    authURL := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s&code_challenge=%s&code_challenge_method=S256",
        config.AuthURL,
        url.QueryEscape(config.ClientID),
        url.QueryEscape(config.RedirectURI),
        url.QueryEscape(strings.Join(config.Scopes, " ")),
        state,
        challenge,
    )
    
    // 4. 打开浏览器
    fmt.Printf("请在浏览器中完成登录...\n")
    openBrowser(authURL)
    
    // 5. 等待回调
    select {
    case code := <-codeChan:
        receivedState := <-stateChan
        if receivedState != state {
            return nil, errors.New("state mismatch")
        }
        
        // 6. 用 code 换 token
        return exchangeCodeForToken(config, code, verifier)
        
    case <-time.After(5 * time.Minute):
        return nil, errors.New("login timeout")
    }
}

func exchangeCodeForToken(config *OAuthConfig, code, verifier string) (*Token, error) {
    data := url.Values{
        "grant_type":    {"authorization_code"},
        "code":          {code},
        "redirect_uri":  {config.RedirectURI},
        "client_id":     {config.ClientID},
        "code_verifier": {verifier},
    }
    
    resp, err := http.PostForm(config.TokenURL, data)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    var tokenResp struct {
        AccessToken  string `json:"access_token"`
        RefreshToken string `json:"refresh_token"`
        ExpiresIn    int    `json:"expires_in"`
        TokenType    string `json:"token_type"`
    }
    
    if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
        return nil, err
    }
    
    return &Token{
        AccessToken:  tokenResp.AccessToken,
        RefreshToken: tokenResp.RefreshToken,
        ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
        TokenType:    tokenResp.TokenType,
    }, nil
}
```

### 5.4 Token 刷新实现

```go
func RefreshToken(config *OAuthConfig, refreshToken string) (*Token, error) {
    data := url.Values{
        "grant_type":    {"refresh_token"},
        "refresh_token": {refreshToken},
        "client_id":     {config.ClientID},
    }
    
    resp, err := http.PostForm(config.TokenURL, data)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("token refresh failed: %s", string(body))
    }
    
    var tokenResp struct {
        AccessToken  string `json:"access_token"`
        RefreshToken string `json:"refresh_token,omitempty"`
        ExpiresIn    int    `json:"expires_in"`
    }
    
    if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
        return nil, err
    }
    
    newToken := &Token{
        AccessToken: tokenResp.AccessToken,
        ExpiresAt:   time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
    }
    
    // 有些 Provider 会返回新的 refresh_token
    if tokenResp.RefreshToken != "" {
        newToken.RefreshToken = tokenResp.RefreshToken
    } else {
        newToken.RefreshToken = refreshToken
    }
    
    return newToken, nil
}
```

### 5.5 Token 管理器

```go
type TokenManager struct {
    mu       sync.RWMutex
    tokens   map[string]*Token
    configs  map[string]*OAuthConfig
    storage  TokenStorage
}

func (tm *TokenManager) GetValidToken(provider string) (*Token, error) {
    tm.mu.RLock()
    token := tm.tokens[provider]
    tm.mu.RUnlock()
    
    if token == nil {
        return nil, ErrNotLoggedIn
    }
    
    // 检查是否需要刷新（提前 5 分钟）
    if time.Until(token.ExpiresAt) < 5*time.Minute {
        tm.mu.Lock()
        defer tm.mu.Unlock()
        
        // 双重检查
        token = tm.tokens[provider]
        if time.Until(token.ExpiresAt) < 5*time.Minute {
            config := tm.configs[provider]
            newToken, err := RefreshToken(config, token.RefreshToken)
            if err != nil {
                // Refresh token 也过期了，需要重新登录
                if isRefreshTokenExpired(err) {
                    delete(tm.tokens, provider)
                    tm.storage.Delete(provider)
                    return nil, ErrNeedRelogin
                }
                return nil, err
            }
            
            tm.tokens[provider] = newToken
            tm.storage.Save(provider, newToken)
            return newToken, nil
        }
    }
    
    return token, nil
}
```

---

## 六、Provider 特定配置

### 6.1 Claude Code 配置

```go
var ClaudeOAuthConfig = &OAuthConfig{
    AuthURL:      "https://claude.ai/oauth/authorize",
    TokenURL:     "https://claude.ai/oauth/token",
    ClientID:     "claude-code-cli",
    RedirectURI:  "http://localhost:54545/callback",
    Scopes:       []string{"user:inference", "user:profile"},
    CallbackPort: 54545,
}
```

### 6.2 Gemini CLI 配置

```go
var GeminiOAuthConfig = &OAuthConfig{
    AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
    TokenURL:     "https://oauth2.googleapis.com/token",
    ClientID:     "<google_client_id>.apps.googleusercontent.com",
    RedirectURI:  "http://localhost:8085/callback",
    Scopes:       []string{
        "https://www.googleapis.com/auth/generative-language",
        "openid",
        "email",
    },
    CallbackPort: 8085,
    // Google 特有参数
    ExtraParams: map[string]string{
        "access_type": "offline",
        "prompt":      "consent",
    },
}
```

### 6.3 OpenAI Codex 配置

```go
var CodexOAuthConfig = &OAuthConfig{
    AuthURL:      "https://auth0.openai.com/authorize",
    TokenURL:     "https://auth0.openai.com/oauth/token",
    ClientID:     "<openai_cli_client_id>",
    RedirectURI:  "http://localhost:1455/callback",
    Scopes:       []string{"openid", "profile", "email", "offline_access"},
    CallbackPort: 1455,
    ExtraParams: map[string]string{
        "audience": "https://api.openai.com/v1",
    },
}
```

---

## 七、Headless 环境认证

### 7.1 设备授权流程 (Device Flow)

某些 Provider 支持设备授权流程，适用于无浏览器环境：

```
1. CLI 请求设备码
2. 显示用户码和验证 URL
3. 用户在其他设备上访问 URL 并输入用户码
4. CLI 轮询 Token 端点直到授权完成
```

### 7.2 SSH 隧道方案

```bash
# 在本地机器上建立隧道
ssh -L 54545:127.0.0.1:54545 user@remote-server

# 在远程服务器上执行登录
./unifyai-proxy --claude-login

# 浏览器会在本地打开，回调通过隧道传回远程
```

### 7.3 凭证复制方案

```bash
# 在本地完成登录
./unifyai-proxy --claude-login

# 复制凭证到远程服务器
scp ~/.unifyai-proxy/auths/claude/default.json user@remote:~/.unifyai-proxy/auths/claude/
```

---

## 八、安全注意事项

### 8.1 Token 存储安全

```go
// 设置文件权限为 600
func SaveTokenSecurely(path string, token *Token) error {
    data, err := json.Marshal(token)
    if err != nil {
        return err
    }
    
    // 确保目录存在
    dir := filepath.Dir(path)
    if err := os.MkdirAll(dir, 0700); err != nil {
        return err
    }
    
    // 写入文件，权限 600
    return os.WriteFile(path, data, 0600)
}
```

### 8.2 State 参数验证

```go
func validateState(received, expected string) error {
    if received != expected {
        return errors.New("state mismatch: possible CSRF attack")
    }
    return nil
}
```

### 8.3 PKCE 必要性

- 公共客户端（CLI 工具）无法安全存储 client_secret
- PKCE 防止授权码被拦截后被恶意使用
- 所有现代 OAuth 实现都应使用 PKCE

---

## 九、实现检查清单

### OAuth 登录
- [ ] PKCE 生成（code_verifier + code_challenge）
- [ ] State 参数生成和验证
- [ ] 本地回调服务器
- [ ] 浏览器自动打开
- [ ] 超时处理

### Token 管理
- [ ] Token 安全存储（权限 600）
- [ ] 自动刷新（提前 5 分钟）
- [ ] Refresh token 过期处理
- [ ] 多账户支持

### Provider 适配
- [ ] Claude: 端口 54545，scope user:inference
- [ ] Gemini: Google OAuth，access_type=offline
- [ ] Codex: Auth0，audience 参数

### 安全
- [ ] HTTPS 验证
- [ ] State 验证
- [ ] Token 文件权限
- [ ] 敏感信息不打印日志
