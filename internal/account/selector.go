package account

import (
	"errors"
	"sync"
	"time"
)

// ErrorType classifies different types of errors for state transitions
// Reference: docs/unifyai_proxy/15-任务拆解-基础设计.md
type ErrorType int

const (
	// ErrorTypeNone indicates no error
	ErrorTypeNone ErrorType = iota
	// ErrorTypeQuotaExceeded indicates quota has been exceeded
	ErrorTypeQuotaExceeded
	// ErrorTypeRateLimited indicates rate limit hit (429)
	ErrorTypeRateLimited
	// ErrorTypeAuthError indicates authentication error (401/403)
	ErrorTypeAuthError
	// ErrorTypeNetworkError indicates network connectivity error
	ErrorTypeNetworkError
	// ErrorTypeServerError indicates server error (5xx)
	ErrorTypeServerError
)

// String returns the string representation of ErrorType
func (e ErrorType) String() string {
	switch e {
	case ErrorTypeNone:
		return "none"
	case ErrorTypeQuotaExceeded:
		return "quota_exceeded"
	case ErrorTypeRateLimited:
		return "rate_limited"
	case ErrorTypeAuthError:
		return "auth_error"
	case ErrorTypeNetworkError:
		return "network_error"
	case ErrorTypeServerError:
		return "server_error"
	default:
		return "unknown"
	}
}

// Result encapsulates request result for state machine updates
type Result struct {
	Success       bool          // Whether the request succeeded
	ErrorType     ErrorType     // Type of error if failed
	ErrorCode     int           // HTTP status code (403/429/503 etc.)
	TokensUsed    int64         // Number of tokens used
	ResponseTime  time.Duration // Response latency
	ShouldRetry   bool          // Whether the request should be retried
	ShouldSwitch  bool          // Whether to switch to another account
	Error         error         // The actual error if any
}

// Common errors
var (
	ErrNoAvailableAccount    = errors.New("no available account")
	ErrAllAccountsExhausted  = errors.New("all accounts exhausted (quota exceeded)")
	ErrAllAccountsErrored    = errors.New("all accounts in error state")
	ErrAccountNotFound       = errors.New("account not found")
	ErrModelNotSupported     = errors.New("model not supported by any provider")
)

// AccountSelector selects accounts for requests
type AccountSelector interface {
	// Pick selects an available account for the given model and provider
	Pick(model, provider string) (*Account, error)
	// MarkResult updates account state based on request result
	MarkResult(acc *Account, result Result)
}

// DefaultAccountSelector implements round-robin account selection with state management
type DefaultAccountSelector struct {
	accounts      map[string][]*Account // provider -> accounts
	currentIndex  map[string]int        // provider -> current index
	mu            sync.RWMutex
	maxRetryInterval time.Duration
}

// NewDefaultAccountSelector creates a new account selector
func NewDefaultAccountSelector(maxRetryInterval time.Duration) *DefaultAccountSelector {
	return &DefaultAccountSelector{
		accounts:         make(map[string][]*Account),
		currentIndex:     make(map[string]int),
		maxRetryInterval: maxRetryInterval,
	}
}

// AddAccount adds an account to the selector
func (s *DefaultAccountSelector) AddAccount(acc *Account) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.accounts[acc.Provider] == nil {
		s.accounts[acc.Provider] = make([]*Account, 0)
	}
	s.accounts[acc.Provider] = append(s.accounts[acc.Provider], acc)
}

// RemoveAccount removes an account from the selector
func (s *DefaultAccountSelector) RemoveAccount(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for provider, accounts := range s.accounts {
		for i, acc := range accounts {
			if acc.ID == id {
				s.accounts[provider] = append(accounts[:i], accounts[i+1:]...)
				return
			}
		}
	}
}

// Pick selects an available account using round-robin strategy
// The returned account is reserved and must be released after use by calling Release()
func (s *DefaultAccountSelector) Pick(model, provider string) (*Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	accounts, exists := s.accounts[provider]
	if !exists || len(accounts) == 0 {
		return nil, ErrNoAvailableAccount
	}

	startIndex := s.currentIndex[provider]
	n := len(accounts)

	// Track states for error reporting
	var quotaExceededCount, erroredCount, coolingCount, modelMismatchCount int
	var earliestResetAt time.Time

	for i := 0; i < n; i++ {
		idx := (startIndex + i) % n
		acc := accounts[idx]

		// Try to recover the account first
		acc.CheckAndRecover()

		// Check if account supports the requested model
		if model != "" && !acc.SupportsModel(model) {
			modelMismatchCount++
			continue
		}

		// Try to reserve the account atomically
		if acc.Reserve() {
			s.currentIndex[provider] = (idx + 1) % n
			return acc, nil
		}

		// Track unavailable states for better error messages
		state := acc.GetState()
		switch state {
		case StateQuotaExceeded:
			quotaExceededCount++
			info := acc.GetStateInfo()
			if earliestResetAt.IsZero() || info.ResetAt.Before(earliestResetAt) {
				earliestResetAt = info.ResetAt
			}
		case StateErrored:
			erroredCount++
		case StateCoolingDown:
			coolingCount++
		}
	}

	// Determine the appropriate error
	if modelMismatchCount == n {
		return nil, ErrModelNotSupported
	}
	if quotaExceededCount == n-modelMismatchCount && quotaExceededCount > 0 {
		return nil, ErrAllAccountsExhausted
	}
	if erroredCount == n-modelMismatchCount && erroredCount > 0 {
		return nil, ErrAllAccountsErrored
	}

	return nil, ErrNoAvailableAccount
}

// MarkResult updates account state based on request result
func (s *DefaultAccountSelector) MarkResult(acc *Account, result Result) {
	if result.Success {
		// Record successful usage
		acc.RecordUsage(1, result.TokensUsed)
		return
	}

	// Handle error based on type
	switch result.ErrorType {
	case ErrorTypeQuotaExceeded:
		// Transition to quota exceeded, calculate reset time
		resetAt := calculateNextResetTime(time.Now(), "UTC")
		acc.TransitionToQuotaExceeded(resetAt)

	case ErrorTypeRateLimited:
		// Cooling down for rate limit
		cooldown := calculateCooldown(result.ErrorCode, s.maxRetryInterval)
		acc.TransitionToCoolingDown(cooldown, result.Error)

	case ErrorTypeAuthError:
		// Fatal auth error - needs manual intervention
		acc.TransitionToErrored(result.Error)

	case ErrorTypeNetworkError, ErrorTypeServerError:
		// Temporary error - cooling down
		cooldown := calculateCooldown(result.ErrorCode, s.maxRetryInterval)
		acc.TransitionToCoolingDown(cooldown, result.Error)
	}
}

// GetAccountsByProvider returns all accounts for a provider
func (s *DefaultAccountSelector) GetAccountsByProvider(provider string) []*Account {
	s.mu.RLock()
	defer s.mu.RUnlock()

	accounts := s.accounts[provider]
	result := make([]*Account, len(accounts))
	copy(result, accounts)
	return result
}

// GetAllAccounts returns all accounts
func (s *DefaultAccountSelector) GetAllAccounts() []*Account {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Account
	for _, accounts := range s.accounts {
		result = append(result, accounts...)
	}
	return result
}

// GetAvailableAccountCount returns the number of available accounts for a provider
func (s *DefaultAccountSelector) GetAvailableAccountCount(provider string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, acc := range s.accounts[provider] {
		if acc.IsAvailable() {
			count++
		}
	}
	return count
}

// calculateCooldown calculates cooldown duration based on error code
func calculateCooldown(errorCode int, maxInterval time.Duration) time.Duration {
	var cooldown time.Duration

	switch errorCode {
	case 429:
		// Rate limited - start with 60 seconds
		cooldown = 60 * time.Second
	case 503:
		// Service unavailable - start with 30 seconds
		cooldown = 30 * time.Second
	case 500, 502, 504:
		// Server errors - start with 10 seconds
		cooldown = 10 * time.Second
	default:
		// Other errors - start with 5 seconds
		cooldown = 5 * time.Second
	}

	if cooldown > maxInterval {
		cooldown = maxInterval
	}

	return cooldown
}
