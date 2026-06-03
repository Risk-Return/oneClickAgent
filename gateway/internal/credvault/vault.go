// Package credvault provides AES-256-GCM encryption and decryption for
// browser login credentials. Supports data-key mode (IAGENT_CRED_KEY) and
// KMS envelope mode (IAGENT_CRED_KMS, stub).
//
// Storage format (aligned with 06-data-model §1.16):
//
//	storage_state_enc  — ciphertext (no nonce, no tag)
//	nonce              — 12-byte GCM nonce/IV
//	auth_tag           — 16-byte GCM authentication tag
//
// key_id identifies the key version, stored alongside the encrypted data.
package credvault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/oneClickAgent/gateway/internal/model"
)

var (
	ErrKeyNotConfigured = errors.New("credential vault key not configured")
	ErrDecryptFailed    = errors.New("decryption failed: integrity check failed or wrong key")
	ErrDataTooLarge     = errors.New("credential data exceeds maximum size")
)

const MaxCredentialSize = 10 << 20 // 10 MiB

// Vault encrypts/decrypts browser credentials.
type Vault struct {
	mu      sync.RWMutex
	dataKey []byte   // raw AES-256 key (32 bytes)
	keyID   string   // key identifier
	kmsID   string   // KMS key id if using envelope encryption
}

// NewVault creates a credential vault.
// credKey is a base64-encoded 32-byte AES-256 key.
// kmsKey is an optional KMS key ID for envelope encryption (stub).
func NewVault(credKey, kmsKey string) (*Vault, error) {
	v := &Vault{keyID: "default", kmsID: kmsKey}

	if credKey != "" {
		key, err := base64.StdEncoding.DecodeString(credKey)
		if err != nil {
			return nil, fmt.Errorf("decode IAGENT_CRED_KEY: %w", err)
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("IAGENT_CRED_KEY must be 32 bytes (AES-256), got %d", len(key))
		}
		v.dataKey = key
	}

	return v, nil
}

// IsConfigured returns true if the vault has an encryption key.
func (v *Vault) IsConfigured() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.dataKey) > 0 || v.kmsID != ""
}

// EncryptResult holds the separated AES-256-GCM output fields.
type EncryptResult struct {
	StorageStateEnc []byte
	Nonce           []byte
	AuthTag         []byte
	SHA256          string
}

// Encrypt encrypts plaintext with AES-256-GCM and returns separated fields.
func (v *Vault) Encrypt(plaintext []byte) (EncryptResult, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if len(v.dataKey) == 0 {
		return EncryptResult{}, ErrKeyNotConfigured
	}
	if len(plaintext) > MaxCredentialSize {
		return EncryptResult{}, ErrDataTooLarge
	}

	block, err := aes.NewCipher(v.dataKey)
	if err != nil {
		return EncryptResult{}, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return EncryptResult{}, fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return EncryptResult{}, fmt.Errorf("generate nonce: %w", err)
	}

	// GCM seal (without prepended nonce): ciphertext || tag
	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	tagSize := gcm.Overhead()
	storageStateEnc := sealed[:len(sealed)-tagSize]
	authTag := sealed[len(sealed)-tagSize:]

	h := sha256.Sum256(plaintext)
	sha256sum := hex.EncodeToString(h[:])

	return EncryptResult{
		StorageStateEnc: storageStateEnc,
		Nonce:           nonce,
		AuthTag:         authTag,
		SHA256:          sha256sum,
	}, nil
}

// Decrypt decrypts using separate storage_state_enc, nonce, auth_tag and verifies SHA-256.
func (v *Vault) Decrypt(storageStateEnc, nonce, authTag []byte, expectedSHA256 string) ([]byte, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if len(v.dataKey) == 0 {
		return nil, ErrKeyNotConfigured
	}

	block, err := aes.NewCipher(v.dataKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	if len(nonce) != gcm.NonceSize() {
		return nil, ErrDecryptFailed
	}

	sealed := append(storageStateEnc, authTag...)
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}

	h := sha256.Sum256(plaintext)
	actual := hex.EncodeToString(h[:])
	if expectedSHA256 != "" && actual != expectedSHA256 {
		return nil, fmt.Errorf("SHA-256 mismatch: expected %s, got %s", expectedSHA256, actual)
	}

	return plaintext, nil
}

// GenerateKey generates a random 32-byte AES-256 key and returns it base64-encoded.
func GenerateKey() (string, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// CaptureResult is the result of capturing a credential from a VNC session.
type CaptureResult struct {
	Credential *model.BrowserCredential
}
