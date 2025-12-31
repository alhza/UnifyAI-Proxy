// Package auth provides authentication and token management.
package auth

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// AutoRefreshService automatically refreshes OAuth tokens before they expire
type AutoRefreshService struct {
	manager    *TokenManager
	httpClient *http.Client

	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	started    bool
	mu         sync.Mutex

	// Configuration
	checkInterval  time.Duration
	refreshBefore  time.Duration // Refresh tokens this long before expiry
}

// NewAutoRefreshService creates a new auto-refresh service
func NewAutoRefreshService(manager *TokenManager, httpClient *http.Client) *AutoRefreshService {
	return &AutoRefreshService{
		manager:       manager,
		httpClient:    httpClient,
		checkInterval: 5 * time.Minute,
		refreshBefore: 10 * time.Minute,
	}
}

// Start begins the auto-refresh background service
func (s *AutoRefreshService) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return
	}

	s.ctx, s.cancel = context.WithCancel(ctx)
	s.started = true

	s.wg.Add(1)
	go s.refreshLoop()
}

// Stop stops the auto-refresh service
func (s *AutoRefreshService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return
	}

	s.cancel()
	s.wg.Wait()
	s.started = false
	slog.Info("Auto-refresh service stopped")
}

// refreshLoop periodically checks and refreshes tokens
func (s *AutoRefreshService) refreshLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	// Do an initial check immediately
	s.checkAndRefreshTokens()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.checkAndRefreshTokens()
		}
	}
}

// checkAndRefreshTokens checks all tokens and refreshes those close to expiry
func (s *AutoRefreshService) checkAndRefreshTokens() {
	accounts := s.manager.GetAllAccounts()

	for _, account := range accounts {
		if s.shouldRefresh(account.Token) {
			slog.Debug("Token needs refresh",
				"provider", account.Provider,
				"account_id", account.ID,
				"expires_at", account.Token.ExpiresAt)

			if err := s.refreshToken(account); err != nil {
				slog.Warn("Failed to refresh token",
					"provider", account.Provider,
					"account_id", account.ID,
					"error", err)
			} else {
				slog.Info("Token refreshed successfully",
					"provider", account.Provider,
					"account_id", account.ID)
			}
		}
	}
}

// shouldRefresh returns true if the token should be refreshed
func (s *AutoRefreshService) shouldRefresh(token *Token) bool {
	if token == nil || token.RefreshToken == "" {
		return false
	}

	// Check if token expires within refreshBefore duration
	return time.Until(token.ExpiresAt) < s.refreshBefore
}

// refreshToken refreshes a single token
func (s *AutoRefreshService) refreshToken(account *Account) error {
	// Use the token manager's refresh mechanism
	return s.manager.RefreshToken(s.ctx, account.Provider, account.ID, s.httpClient)
}

