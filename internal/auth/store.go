package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// Common errors
var (
	ErrTokenNotFound       = errors.New("token not found")
	ErrInvalidToken        = errors.New("invalid token")
	ErrEncryptionRequired  = errors.New("encryption key required for production mode")
	ErrInvalidIdentifier   = errors.New("invalid provider or token identifier")
)

// safeIdentifierPattern matches only safe identifiers (alphanumeric, underscore, hyphen, dot)
var safeIdentifierPattern = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`)

// Token represents an OAuth token
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`
	Scopes       []string  `json:"scopes"`
}

// IsExpired checks if the token has expired
func (t *Token) IsExpired() bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(t.ExpiresAt)
}

// IsValid checks if the token is valid (not empty and not expired)
func (t *Token) IsValid() bool {
	return t.AccessToken != "" && !t.IsExpired()
}

// Store defines the interface for token storage
type Store interface {
	Save(provider, id string, tok *Token) error
	Load(provider, id string) (*Token, error)
	Delete(provider, id string) error
	List(provider string) ([]string, error)
}

// MemoryStore implements Store using in-memory storage
type MemoryStore struct {
	tokens map[string]map[string]*Token // provider -> id -> token
	mu     sync.RWMutex
}

// NewMemoryStore creates a new in-memory token store
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tokens: make(map[string]map[string]*Token),
	}
}

// Save saves a token to memory
func (s *MemoryStore) Save(provider, id string, tok *Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tokens[provider] == nil {
		s.tokens[provider] = make(map[string]*Token)
	}
	// Store a copy to prevent external modification
	tokenCopy := *tok
	s.tokens[provider][id] = &tokenCopy
	return nil
}

// Load loads a token from memory
func (s *MemoryStore) Load(provider, id string) (*Token, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.tokens[provider] == nil {
		return nil, ErrTokenNotFound
	}
	tok, exists := s.tokens[provider][id]
	if !exists {
		return nil, ErrTokenNotFound
	}
	// Return a copy to prevent external modification
	tokenCopy := *tok
	return &tokenCopy, nil
}

// Delete deletes a token from memory
func (s *MemoryStore) Delete(provider, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tokens[provider] != nil {
		delete(s.tokens[provider], id)
	}
	return nil
}

// List lists all token IDs for a provider
func (s *MemoryStore) List(provider string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.tokens[provider] == nil {
		return []string{}, nil
	}

	ids := make([]string, 0, len(s.tokens[provider]))
	for id := range s.tokens[provider] {
		ids = append(ids, id)
	}
	return ids, nil
}

// FileStore implements Store using file-based storage
type FileStore struct {
	baseDir   string
	mu        sync.RWMutex
	encryptor *Encryptor // Optional encryptor for token encryption
}

// FileStoreConfig contains configuration for FileStore
type FileStoreConfig struct {
	BaseDir           string
	EncryptionKey     []byte
	RequireEncryption bool // If true, returns error when no encryption key
	WarnUnencrypted   bool // If true, logs warning when no encryption key
}

// NewFileStore creates a new file-based token store
func NewFileStore(baseDir string) (*FileStore, error) {
	return NewFileStoreWithEncryption(baseDir, nil)
}

// NewFileStoreWithConfig creates a new file-based token store with full configuration
func NewFileStoreWithConfig(cfg FileStoreConfig) (*FileStore, error) {
	// Check encryption requirements
	if cfg.RequireEncryption && len(cfg.EncryptionKey) == 0 {
		return nil, ErrEncryptionRequired
	}

	// Warn if encryption is not enabled but recommended
	if cfg.WarnUnencrypted && len(cfg.EncryptionKey) == 0 {
		slog.Warn("Token storage encryption is DISABLED - tokens will be stored in plaintext",
			"recommendation", "Set 'token-encryption-key' in config for production deployments")
	}

	return NewFileStoreWithEncryption(cfg.BaseDir, cfg.EncryptionKey)
}

// NewFileStoreWithEncryption creates a new file-based token store with optional encryption
// If encryptionKey is provided, tokens will be encrypted using AES-256-GCM
func NewFileStoreWithEncryption(baseDir string, encryptionKey []byte) (*FileStore, error) {
	// Expand ~ to home directory
	if len(baseDir) > 0 && baseDir[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		// Strip the ~ and any leading path separator from the rest
		rest := baseDir[1:]
		if len(rest) > 0 && (rest[0] == '/' || rest[0] == filepath.Separator) {
			rest = rest[1:]
		}
		baseDir = filepath.Join(home, rest)
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, err
	}

	store := &FileStore{baseDir: baseDir}

	// Setup encryption if key provided
	if len(encryptionKey) > 0 {
		// Try to load existing salt from file
		saltPath := filepath.Join(baseDir, ".salt")
		var salt []byte
		if data, err := os.ReadFile(saltPath); err == nil && len(data) == 16 {
			salt = data
		}

		config := CryptoConfig{
			Key:  encryptionKey,
			Salt: salt,
		}
		encryptor, err := NewEncryptor(config)
		if err != nil {
			return nil, err
		}
		store.encryptor = encryptor

		// Save salt for consistent key derivation
		if len(salt) == 0 {
			if err := os.WriteFile(saltPath, encryptor.Salt(), 0600); err != nil {
				return nil, err
			}
		}
	}

	return store, nil
}


// validateIdentifier checks if provider and id are safe (no path traversal)
func validateIdentifier(provider, id string) error {
	if !safeIdentifierPattern.MatchString(provider) {
		return ErrInvalidIdentifier
	}
	if !safeIdentifierPattern.MatchString(id) {
		return ErrInvalidIdentifier
	}
	return nil
}

// tokenPath returns the file path for a token
func (s *FileStore) tokenPath(provider, id string) string {
	ext := ".json"
	if s.encryptor != nil {
		ext = ".enc" // Use different extension for encrypted files
	}
	return filepath.Join(s.baseDir, provider, id+ext)
}

// Save saves a token to file (encrypted if encryptor is configured)
func (s *FileStore) Save(provider, id string, tok *Token) error {
	// Validate identifiers to prevent path traversal
	if err := validateIdentifier(provider, id); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Create provider directory
	providerDir := filepath.Join(s.baseDir, provider)
	if err := os.MkdirAll(providerDir, 0700); err != nil {
		return err
	}

	// Marshal token to JSON
	data, err := json.Marshal(tok)
	if err != nil {
		return err
	}

	// Encrypt if encryptor is configured
	if s.encryptor != nil {
		encrypted, err := s.encryptor.Encrypt(data)
		if err != nil {
			return err
		}
		data = []byte(encrypted)
	}

	// Write to file with restricted permissions
	return os.WriteFile(s.tokenPath(provider, id), data, 0600)
}

// Load loads a token from file (decrypts if encryptor is configured)
func (s *FileStore) Load(provider, id string) (*Token, error) {
	// Validate identifiers to prevent path traversal
	if err := validateIdentifier(provider, id); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.tokenPath(provider, id))
	if err != nil {
		if os.IsNotExist(err) {
			// Try legacy .json extension if using encryption
			if s.encryptor != nil {
				legacyPath := filepath.Join(s.baseDir, provider, id+".json")
				data, err = os.ReadFile(legacyPath)
				if err != nil {
					if os.IsNotExist(err) {
						return nil, ErrTokenNotFound
					}
					return nil, err
				}
				// Legacy unencrypted file - parse and migrate
				var tok Token
				if err := json.Unmarshal(data, &tok); err != nil {
					return nil, err
				}
				return &tok, nil
			}
			return nil, ErrTokenNotFound
		}
		return nil, err
	}

	// Decrypt if encryptor is configured
	if s.encryptor != nil {
		decrypted, err := s.encryptor.Decrypt(string(data))
		if err != nil {
			// Try parsing as unencrypted JSON (migration case)
			var tok Token
			if jsonErr := json.Unmarshal(data, &tok); jsonErr == nil {
				return &tok, nil
			}
			return nil, err
		}
		data = decrypted
	}

	var tok Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, err
	}

	return &tok, nil
}

// Delete deletes a token file
func (s *FileStore) Delete(provider, id string) error {
	// Validate identifiers to prevent path traversal
	if err := validateIdentifier(provider, id); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	err := os.Remove(s.tokenPath(provider, id))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// List lists all token IDs for a provider
func (s *FileStore) List(provider string) ([]string, error) {
	// Validate provider to prevent path traversal
	if !safeIdentifierPattern.MatchString(provider) {
		return nil, ErrInvalidIdentifier
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	providerDir := filepath.Join(s.baseDir, provider)
	entries, err := os.ReadDir(providerDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		// Support both .json and .enc extensions
		if ext == ".json" || ext == ".enc" {
			id := name[:len(name)-len(ext)]
			seen[id] = true
		}
	}

	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids, nil
}