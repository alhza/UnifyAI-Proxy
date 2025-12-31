package account

import (
	"sync"
	"time"

	"github.com/unifyai-proxy/unifyai-proxy/internal/util"
)

// AccountState represents the current state of an account
// Reference: docs/unifyai_proxy/15-任务拆解-基础设计.md
type AccountState int

const (
	// StateAvailable indicates the account is available for requests
	StateAvailable AccountState = iota
	// StateCoolingDown indicates the account is cooling down after a failure
	StateCoolingDown
	// StateQuotaExceeded indicates the account has exceeded its quota
	StateQuotaExceeded
	// StateErrored indicates the account has encountered a fatal error
	StateErrored
)

// String returns the string representation of AccountState
func (s AccountState) String() string {
	switch s {
	case StateAvailable:
		return "available"
	case StateCoolingDown:
		return "cooling_down"
	case StateQuotaExceeded:
		return "quota_exceeded"
	case StateErrored:
		return "errored"
	default:
		return "unknown"
	}
}

// UsageCounter tracks account usage metrics
type UsageCounter struct {
	Requests    int64     // Total requests today
	Tokens      int64     // Total tokens today
	RPMRequests int64     // Requests in current minute window
	RPMWindowStart time.Time // Start of current RPM window
}

// Account represents a provider account with state machine
type Account struct {
	ID               string
	Provider         string
	TokenID          string
	State            AccountState
	UnavailableUntil time.Time   // When CoolingDown state ends
	ResetAt          time.Time   // When QuotaExceeded state ends (daily reset)
	Usage            UsageCounter
	LastError        error
	LastErrorTime    time.Time
	LastRequestTime  time.Time

	// Limits configured for this account
	DailyRequestLimit int64
	DailyTokenLimit   int64
	RPMLimit          int

	// Reservation counter for concurrent access control
	activeRequests int32

	// SupportedModels is the list of models this account supports (empty means all)
	SupportedModels []string
	// ExcludedModels is the list of models this account does not support
	ExcludedModels []string

	mu sync.RWMutex
}

// NewAccount creates a new account with initial state
func NewAccount(id, provider, tokenID string) *Account {
	return &Account{
		ID:       id,
		Provider: provider,
		TokenID:  tokenID,
		State:    StateAvailable,
		Usage:    UsageCounter{},
	}
}

// GetState returns the current state with read lock
func (a *Account) GetState() AccountState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.State
}

// IsAvailable checks if the account is available for requests
func (a *Account) IsAvailable() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.isAvailableUnsafe()
}

// isAvailableUnsafe checks availability without lock (caller must hold lock)
func (a *Account) isAvailableUnsafe() bool {
	now := time.Now()

	switch a.State {
	case StateAvailable:
		// Also check RPM limit for available accounts
		if a.isRPMLimitExceededUnsafe() {
			return false
		}
		return true
	case StateCoolingDown:
		// Check if cooling period has ended
		if now.After(a.UnavailableUntil) {
			return true
		}
		return false
	case StateQuotaExceeded:
		// Check if reset time has passed
		if now.After(a.ResetAt) {
			return true
		}
		return false
	case StateErrored:
		return false
	default:
		return false
	}
}

// CheckAndRecover attempts to recover from non-available states
// Returns true if the account is now available
func (a *Account) CheckAndRecover() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()

	switch a.State {
	case StateCoolingDown:
		if now.After(a.UnavailableUntil) {
			// Check if quota is still OK
			if !a.isQuotaExceededUnsafe() {
				a.State = StateAvailable
				return true
			}
			// Transition to QuotaExceeded
			a.State = StateQuotaExceeded
			return false
		}
	case StateQuotaExceeded:
		if now.After(a.ResetAt) {
			// Reset usage counters
			a.Usage = UsageCounter{}
			a.State = StateAvailable
			return true
		}
	}

	return a.State == StateAvailable
}

// isQuotaExceededUnsafe checks if quota is exceeded without lock
func (a *Account) isQuotaExceededUnsafe() bool {
	if a.DailyRequestLimit > 0 && a.Usage.Requests >= a.DailyRequestLimit {
		return true
	}
	if a.DailyTokenLimit > 0 && a.Usage.Tokens >= a.DailyTokenLimit {
		return true
	}
	return false
}

// isRPMLimitExceededUnsafe checks if RPM limit is exceeded without lock
func (a *Account) isRPMLimitExceededUnsafe() bool {
	if a.RPMLimit <= 0 {
		return false
	}

	now := time.Now()
	// Check if we're in a new minute window
	if now.Sub(a.Usage.RPMWindowStart) >= time.Minute {
		return false // New window, not exceeded
	}

	return a.Usage.RPMRequests >= int64(a.RPMLimit)
}


// SetLimits configures the account's quota limits
func (a *Account) SetLimits(dailyRequests, dailyTokens int64, rpm int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.DailyRequestLimit = dailyRequests
	a.DailyTokenLimit = dailyTokens
	a.RPMLimit = rpm
}

// SetResetTime sets the next daily reset time
func (a *Account) SetResetTime(resetAt time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ResetAt = resetAt
}

// TransitionToCoolingDown transitions the account to cooling down state
// duration is the cooldown period
func (a *Account) TransitionToCoolingDown(duration time.Duration, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.State == StateErrored {
		return // Don't transition from Errored
	}

	a.State = StateCoolingDown
	a.UnavailableUntil = time.Now().Add(duration)
	a.LastError = err
	a.LastErrorTime = time.Now()
}

// TransitionToQuotaExceeded transitions the account to quota exceeded state
func (a *Account) TransitionToQuotaExceeded(resetAt time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.State = StateQuotaExceeded
	a.ResetAt = resetAt
}

// TransitionToErrored transitions the account to errored state
func (a *Account) TransitionToErrored(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.State = StateErrored
	a.LastError = err
	a.LastErrorTime = time.Now()
}

// TransitionToAvailable transitions the account back to available state
func (a *Account) TransitionToAvailable() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.State = StateAvailable
	a.LastError = nil
}

// RecordUsage adds usage to the account counters
// Returns the new state after recording usage
func (a *Account) RecordUsage(requests, tokens int64) AccountState {
	return a.RecordUsageWithTimezone(requests, tokens, "UTC")
}

// RecordUsageWithTimezone adds usage to the account counters with specified timezone for reset calculation
// Returns the new state after recording usage
func (a *Account) RecordUsageWithTimezone(requests, tokens int64, timezone string) AccountState {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	a.LastRequestTime = now

	// Update daily counters
	a.Usage.Requests += requests
	a.Usage.Tokens += tokens

	// Update RPM counter
	if now.Sub(a.Usage.RPMWindowStart) >= time.Minute {
		// New minute window
		a.Usage.RPMWindowStart = now
		a.Usage.RPMRequests = requests
	} else {
		a.Usage.RPMRequests += requests
	}

	// Check if quota is now exceeded
	if a.isQuotaExceededUnsafe() && a.State == StateAvailable {
		a.State = StateQuotaExceeded
		// Set ResetAt to next midnight in the specified timezone
		a.ResetAt = calculateNextResetTime(now, timezone)
	}

	return a.State
}

// calculateNextResetTime calculates the next daily reset time
func calculateNextResetTime(now time.Time, timezone string) time.Time {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}

	// Get current time in the specified timezone
	localNow := now.In(loc)

	// Calculate next midnight
	nextMidnight := time.Date(
		localNow.Year(), localNow.Month(), localNow.Day()+1,
		0, 0, 0, 0, loc,
	)

	return nextMidnight
}

// GetUsage returns current usage counters
func (a *Account) GetUsage() UsageCounter {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Usage
}

// ResetUsage resets usage counters (called on daily reset)
func (a *Account) ResetUsage() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.Usage = UsageCounter{}
	if a.State == StateQuotaExceeded {
		a.State = StateAvailable
	}
}

// GetStateInfo returns detailed state information
func (a *Account) GetStateInfo() AccountStateInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return AccountStateInfo{
		State:            a.State,
		UnavailableUntil: a.UnavailableUntil,
		ResetAt:          a.ResetAt,
		Usage:            a.Usage,
		LastError:        a.LastError,
		LastErrorTime:    a.LastErrorTime,
		LastRequestTime:  a.LastRequestTime,
	}
}

// AccountStateInfo contains detailed state information
type AccountStateInfo struct {
	State            AccountState
	UnavailableUntil time.Time
	ResetAt          time.Time
	Usage            UsageCounter
	LastError        error
	LastErrorTime    time.Time
	LastRequestTime  time.Time
}

// Reserve atomically reserves the account for a request
// Returns true if the reservation was successful, false if the account is not available
func (a *Account) Reserve() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.isAvailableUnsafe() {
		return false
	}

	a.activeRequests++
	return true
}

// Release releases a reservation on the account
func (a *Account) Release() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.activeRequests > 0 {
		a.activeRequests--
	}
}

// GetActiveRequests returns the number of active requests
func (a *Account) GetActiveRequests() int32 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.activeRequests
}

// SupportsModel checks if this account supports the given model
func (a *Account) SupportsModel(model string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Check if model is in excluded list
	for _, excluded := range a.ExcludedModels {
		if excluded == model {
			return false
		}
		// Support wildcard matching (e.g., "gpt-*")
		if util.MatchWildcard(excluded, model) {
			return false
		}
	}

	// If SupportedModels is empty, all models are supported (except excluded)
	if len(a.SupportedModels) == 0 {
		return true
	}

	// Check if model is in supported list
	for _, supported := range a.SupportedModels {
		if supported == model {
			return true
		}
		// Support wildcard matching
		if util.MatchWildcard(supported, model) {
			return true
		}
	}

	return false
}

// SetModels configures the supported and excluded models for this account
func (a *Account) SetModels(supported, excluded []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.SupportedModels = supported
	a.ExcludedModels = excluded
}
