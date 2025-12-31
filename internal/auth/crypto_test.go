package auth

import (
	"testing"
)

func TestNewEncryptor(t *testing.T) {
	tests := []struct {
		name      string
		config    CryptoConfig
		shouldErr bool
	}{
		{
			name:      "empty key should fail",
			config:    CryptoConfig{},
			shouldErr: true,
		},
		{
			name: "valid key should work",
			config: CryptoConfig{
				Key: []byte("test-encryption-key"),
			},
			shouldErr: false,
		},
		{
			name: "with custom salt",
			config: CryptoConfig{
				Key:  []byte("test-key"),
				Salt: []byte("1234567890123456"),
			},
			shouldErr: false,
		},
		{
			name: "with custom iterations",
			config: CryptoConfig{
				Key:        []byte("test-key"),
				Iterations: 10000,
			},
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, err := NewEncryptor(tt.config)
			if tt.shouldErr {
				if err == nil {
					t.Error("NewEncryptor() should have failed")
				}
				return
			}
			if err != nil {
				t.Fatalf("NewEncryptor() error = %v", err)
			}
			if enc == nil {
				t.Error("NewEncryptor() returned nil encryptor")
			}
		})
	}
}

func TestEncryptDecrypt(t *testing.T) {
	enc, err := NewEncryptor(CryptoConfig{
		Key:  []byte("test-encryption-key"),
		Salt: []byte("1234567890123456"),
	})
	if err != nil {
		t.Fatalf("NewEncryptor() error = %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
	}{
		{"empty string", ""},
		{"short text", "hello"},
		{"API key format", "sk-test123456789abcdef"},
		{"long text", "this is a much longer text that should still work correctly with encryption"},
		{"unicode text", "测试中文和emoji 🔐"},
		{"special chars", "!@#$%^&*()_+-=[]{}|;':\",./<>?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := enc.Encrypt([]byte(tt.plaintext))
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}

			// Encrypted should be different from plaintext
			if tt.plaintext != "" && encrypted == tt.plaintext {
				t.Error("Encrypt() did not change the data")
			}

			decrypted, err := enc.Decrypt(encrypted)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}

			if string(decrypted) != tt.plaintext {
				t.Errorf("Decrypt() = %q, want %q", decrypted, tt.plaintext)
			}
		})
	}
}

func TestDecryptWithDifferentEncryptor(t *testing.T) {
	// Create two encryptors with same key and salt
	config := CryptoConfig{
		Key:  []byte("shared-key"),
		Salt: []byte("1234567890123456"),
	}

	enc1, _ := NewEncryptor(config)
	enc2, _ := NewEncryptor(config)

	plaintext := "secret data"
	encrypted, _ := enc1.Encrypt([]byte(plaintext))

	// Should be able to decrypt with another encryptor using same key/salt
	decrypted, err := enc2.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt() with same config should work: %v", err)
	}
	if string(decrypted) != plaintext {
		t.Errorf("Decrypt() = %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	enc1, _ := NewEncryptor(CryptoConfig{
		Key:  []byte("correct-key"),
		Salt: []byte("1234567890123456"),
	})

	enc2, _ := NewEncryptor(CryptoConfig{
		Key:  []byte("wrong-key"),
		Salt: []byte("1234567890123456"),
	})

	encrypted, _ := enc1.Encrypt([]byte("secret"))

	_, err := enc2.Decrypt(encrypted)
	if err == nil {
		t.Error("Decrypt() with wrong key should fail")
	}
}

func TestEncryptProducesUniqueOutput(t *testing.T) {
	enc, _ := NewEncryptor(CryptoConfig{
		Key:  []byte("test-key"),
		Salt: []byte("1234567890123456"),
	})

	plaintext := "same plaintext"

	encrypted1, _ := enc.Encrypt([]byte(plaintext))
	encrypted2, _ := enc.Encrypt([]byte(plaintext))

	// Each encryption should produce different ciphertext due to random nonce
	if encrypted1 == encrypted2 {
		t.Error("Encrypt() should produce unique output each time")
	}
}

