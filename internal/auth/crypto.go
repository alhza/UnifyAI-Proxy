package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

// CryptoConfig holds encryption configuration
type CryptoConfig struct {
	// Key is the encryption key (will be derived using PBKDF2 if < 32 bytes)
	Key []byte
	// Salt for key derivation (auto-generated if empty)
	Salt []byte
	// Iterations for PBKDF2 (default: 100000)
	Iterations int
}

// DefaultCryptoConfig returns default crypto configuration
func DefaultCryptoConfig() CryptoConfig {
	return CryptoConfig{
		Iterations: 100000,
	}
}

// Encryptor handles AES-256-GCM encryption/decryption
type Encryptor struct {
	gcm  cipher.AEAD
	salt []byte
}

// NewEncryptor creates a new encryptor with the given key
// If the key is less than 32 bytes, it will be derived using PBKDF2
func NewEncryptor(config CryptoConfig) (*Encryptor, error) {
	if len(config.Key) == 0 {
		return nil, errors.New("encryption key is required")
	}

	// Generate salt if not provided
	salt := config.Salt
	if len(salt) == 0 {
		salt = make([]byte, 16)
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return nil, fmt.Errorf("failed to generate salt: %w", err)
		}
	}

	// Derive key using PBKDF2 if needed
	iterations := config.Iterations
	if iterations == 0 {
		iterations = 100000
	}

	derivedKey := pbkdf2.Key(config.Key, salt, iterations, 32, sha256.New)

	// Create AES cipher
	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &Encryptor{
		gcm:  gcm,
		salt: salt,
	}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM
// Returns base64-encoded ciphertext with prepended nonce and salt
func (e *Encryptor) Encrypt(plaintext []byte) (string, error) {
	// Generate random nonce
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt
	ciphertext := e.gcm.Seal(nil, nonce, plaintext, nil)

	// Combine: salt (16) + nonce (12) + ciphertext
	combined := make([]byte, 0, len(e.salt)+len(nonce)+len(ciphertext))
	combined = append(combined, e.salt...)
	combined = append(combined, nonce...)
	combined = append(combined, ciphertext...)

	return base64.StdEncoding.EncodeToString(combined), nil
}

// Decrypt decrypts base64-encoded ciphertext
func (e *Encryptor) Decrypt(encoded string) ([]byte, error) {
	combined, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	nonceSize := e.gcm.NonceSize()
	saltSize := 16

	if len(combined) < saltSize+nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	// Extract components: salt (16) + nonce (12) + ciphertext
	// salt := combined[:saltSize] // Salt is already used in key derivation
	nonce := combined[saltSize : saltSize+nonceSize]
	ciphertext := combined[saltSize+nonceSize:]

	// Decrypt
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}

// Salt returns the salt used for key derivation
func (e *Encryptor) Salt() []byte {
	return e.salt
}

