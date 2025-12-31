package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// OAuthFlow handles the OAuth authorization flow with PKCE
type OAuthFlow struct {
	ClientID     string
	AuthURL      string
	TokenURL     string
	RedirectURI  string
	Scopes       []string
	CallbackPort int

	// PKCE values
	codeVerifier  string
	codeChallenge string

	// Callback handling
	callbackServer *http.Server
	authCode       string
	authError      error
	done           chan struct{}
	doneOnce       sync.Once // Ensures done channel is closed only once
	mu             sync.Mutex
}

// NewOAuthFlow creates a new OAuth flow instance
func NewOAuthFlow(clientID, authURL, tokenURL string, scopes []string, callbackPort int) *OAuthFlow {
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", callbackPort)
	return &OAuthFlow{
		ClientID:     clientID,
		AuthURL:      authURL,
		TokenURL:     tokenURL,
		RedirectURI:  redirectURI,
		Scopes:       scopes,
		CallbackPort: callbackPort,
		done:         make(chan struct{}),
	}
}

// closeDone safely closes the done channel using sync.Once
func (f *OAuthFlow) closeDone() {
	f.doneOnce.Do(func() {
		close(f.done)
	})
}

// GeneratePKCE generates PKCE code verifier and challenge
func (f *OAuthFlow) GeneratePKCE() error {
	// Generate 32 random bytes for code verifier
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Base64 URL encode without padding
	f.codeVerifier = base64.RawURLEncoding.EncodeToString(verifierBytes)

	// Generate code challenge (SHA256 hash of verifier, base64 URL encoded)
	hash := sha256.Sum256([]byte(f.codeVerifier))
	f.codeChallenge = base64.RawURLEncoding.EncodeToString(hash[:])

	return nil
}

// GetAuthorizationURL returns the authorization URL to open in browser
func (f *OAuthFlow) GetAuthorizationURL(state string) string {
	params := url.Values{}
	params.Set("client_id", f.ClientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", f.RedirectURI)
	params.Set("scope", strings.Join(f.Scopes, " "))
	params.Set("state", state)

	if f.codeChallenge != "" {
		params.Set("code_challenge", f.codeChallenge)
		params.Set("code_challenge_method", "S256")
	}

	return f.AuthURL + "?" + params.Encode()
}

// StartCallbackServer starts the local callback server
func (f *OAuthFlow) StartCallbackServer(ctx context.Context, expectedState string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		f.handleCallback(w, r, expectedState)
	})

	addr := fmt.Sprintf("127.0.0.1:%d", f.CallbackPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start callback server: %w", err)
	}

	f.callbackServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := f.callbackServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			f.mu.Lock()
			f.authError = err
			f.mu.Unlock()
			f.closeDone()
		}
	}()

	// Auto-shutdown on context cancellation
	go func() {
		select {
		case <-ctx.Done():
			f.callbackServer.Shutdown(context.Background())
		case <-f.done:
		}
	}()

	return nil
}

// handleCallback handles the OAuth callback
func (f *OAuthFlow) handleCallback(w http.ResponseWriter, r *http.Request, expectedState string) {
	query := r.URL.Query()

	// Check for error
	if errCode := query.Get("error"); errCode != "" {
		errDesc := query.Get("error_description")
		f.mu.Lock()
		f.authError = fmt.Errorf("oauth error: %s - %s", errCode, errDesc)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "<html><body><h1>Authorization Failed</h1><p>%s</p></body></html>", errDesc)
		f.closeDone()
		return
	}

	// Verify state
	state := query.Get("state")
	if state != expectedState {
		f.mu.Lock()
		f.authError = fmt.Errorf("state mismatch: expected %s, got %s", expectedState, state)
		f.mu.Unlock()
		http.Error(w, "State mismatch", http.StatusBadRequest)
		f.closeDone()
		return
	}

	// Get authorization code
	code := query.Get("code")
	if code == "" {
		f.mu.Lock()
		f.authError = fmt.Errorf("no authorization code received")
		f.mu.Unlock()
		http.Error(w, "No code received", http.StatusBadRequest)
		f.closeDone()
		return
	}

	f.mu.Lock()
	f.authCode = code
	f.mu.Unlock()

	// Success response
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<html><body><h1>Authorization Successful</h1>
		<p>You can close this window and return to the application.</p>
		<script>window.close();</script></body></html>`)
	f.closeDone()
}

// WaitForCallback waits for the OAuth callback with timeout
func (f *OAuthFlow) WaitForCallback(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-f.done:
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.authError != nil {
			return "", f.authError
		}
		return f.authCode, nil
	}
}

// ExchangeCode exchanges the authorization code for tokens
func (f *OAuthFlow) ExchangeCode(ctx context.Context, code string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", f.RedirectURI)
	data.Set("client_id", f.ClientID)

	if f.codeVerifier != "" {
		data.Set("code_verifier", f.codeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", f.TokenURL,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("token exchange failed: %s - %s", errResp.Error, errResp.ErrorDescription)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &tokenResp, nil
}

// TokenResponse represents an OAuth token response
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

// ToToken converts TokenResponse to Token
func (t *TokenResponse) ToToken() *Token {
	return &Token{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(t.ExpiresIn) * time.Second),
		TokenType:    t.TokenType,
		Scopes:       strings.Split(t.Scope, " "),
	}
}

// OpenBrowser opens the default browser with the given URL
func OpenBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default: // linux and others
		cmd = exec.Command("xdg-open", url)
	}

	return cmd.Start()
}

// GenerateState generates a random state string for CSRF protection
func GenerateState() (string, error) {
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(stateBytes), nil
}

// Stop stops the callback server
func (f *OAuthFlow) Stop() {
	if f.callbackServer != nil {
		f.callbackServer.Shutdown(context.Background())
	}
}

// Login performs the complete OAuth login flow
func (f *OAuthFlow) Login(ctx context.Context) (*Token, error) {
	// Generate PKCE
	if err := f.GeneratePKCE(); err != nil {
		return nil, fmt.Errorf("failed to generate PKCE: %w", err)
	}

	// Generate state
	state, err := GenerateState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	// Start callback server
	if err := f.StartCallbackServer(ctx, state); err != nil {
		return nil, fmt.Errorf("failed to start callback server: %w", err)
	}
	defer f.Stop()

	// Get authorization URL and open browser
	authURL := f.GetAuthorizationURL(state)
	if err := OpenBrowser(authURL); err != nil {
		return nil, fmt.Errorf("failed to open browser: %w (URL: %s)", err, authURL)
	}

	// Wait for callback
	code, err := f.WaitForCallback(ctx)
	if err != nil {
		return nil, fmt.Errorf("authorization failed: %w", err)
	}

	// Exchange code for tokens
	tokenResp, err := f.ExchangeCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}

	return tokenResp.ToToken(), nil
}

// refreshOAuthToken refreshes an OAuth token using the refresh token
func refreshOAuthToken(ctx context.Context, httpClient interface{}, tokenURL, clientID, refreshToken string) (*Token, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", clientID)

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// Use the provided HTTP client or create a default one
	var client *http.Client
	if c, ok := httpClient.(*http.Client); ok && c != nil {
		client = c
	} else {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("token refresh failed: %s - %s (status: %d)",
			errResp.Error, errResp.ErrorDescription, resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode refresh response: %w", err)
	}

	// If no new refresh token was provided, keep the old one
	newToken := tokenResp.ToToken()
	if newToken.RefreshToken == "" {
		newToken.RefreshToken = refreshToken
	}

	return newToken, nil
}

