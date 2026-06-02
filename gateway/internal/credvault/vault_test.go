package credvault

import (
	"encoding/base64"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	v, err := NewVault(key, "")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	if !v.IsConfigured() {
		t.Fatal("vault should be configured")
	}

	plaintext := []byte(`{"cookies":[{"name":"session","value":"abc123"}],"origins":["https://example.com"]}`)

	result, err := v.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if len(result.StorageStateEnc) == 0 {
		t.Fatal("storage_state_enc should not be empty")
	}
	if len(result.Nonce) == 0 {
		t.Fatal("nonce should not be empty")
	}
	if len(result.AuthTag) == 0 {
		t.Fatal("auth_tag should not be empty")
	}
	if result.SHA256 == "" {
		t.Fatal("sha256 should not be empty")
	}

	decrypted, err := v.Decrypt(result.StorageStateEnc, result.Nonce, result.AuthTag, result.SHA256)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", string(decrypted), string(plaintext))
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1, _ := GenerateKey()
	key2, _ := GenerateKey()

	v1, _ := NewVault(key1, "")
	v2, _ := NewVault(key2, "")

	plaintext := []byte("secret data")
	result, _ := v1.Encrypt(plaintext)

	_, err := v2.Decrypt(result.StorageStateEnc, result.Nonce, result.AuthTag, result.SHA256)
	if err == nil {
		t.Error("decryption with wrong key should fail")
	}
}

func TestDecryptTamperedData(t *testing.T) {
	key, _ := GenerateKey()
	v, _ := NewVault(key, "")

	plaintext := []byte("secret data")
	result, _ := v.Encrypt(plaintext)

	result.StorageStateEnc[0] ^= 0xFF
	_, err := v.Decrypt(result.StorageStateEnc, result.Nonce, result.AuthTag, result.SHA256)
	if err == nil {
		t.Error("decryption of tampered data should fail")
	}
}

func TestDecryptWrongSHA256(t *testing.T) {
	key, _ := GenerateKey()
	v, _ := NewVault(key, "")

	plaintext := []byte("secret data")
	result, _ := v.Encrypt(plaintext)

	_, err := v.Decrypt(result.StorageStateEnc, result.Nonce, result.AuthTag, "deadbeef")
	if err == nil {
		t.Error("decryption with wrong SHA-256 should fail")
	}
}

func TestNotConfigured(t *testing.T) {
	v, _ := NewVault("", "")
	if v.IsConfigured() {
		t.Error("vault without key should not be configured")
	}
	_, err := v.Encrypt([]byte("test"))
	if err != ErrKeyNotConfigured {
		t.Errorf("expected ErrKeyNotConfigured, got %v", err)
	}
	_, err = v.Decrypt([]byte("test"), []byte("test"), []byte("test"), "")
	if err != ErrKeyNotConfigured {
		t.Errorf("expected ErrKeyNotConfigured, got %v", err)
	}
}

func TestInvalidKey(t *testing.T) {
	_, err := NewVault("not-base64!!!", "")
	if err == nil {
		t.Error("should fail on invalid base64 key")
	}

	shortKey := base64.StdEncoding.EncodeToString([]byte("short"))
	_, err = NewVault(shortKey, "")
	if err == nil {
		t.Error("should fail on short key")
	}
}

func TestGenerateKey(t *testing.T) {
	k1, err := GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	k2, err := GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if k1 == k2 {
		t.Error("keys should be unique")
	}

	// Verify it's valid base64 and decodes to 32 bytes
	raw, err := base64.StdEncoding.DecodeString(k1)
	if err != nil {
		t.Errorf("key should be valid base64: %v", err)
	}
	if len(raw) != 32 {
		t.Errorf("key should be 32 bytes, got %d", len(raw))
	}
}
