package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
)

const keyMetaKey = "vault_key"

// MetaStore is the minimal persistence contract needed by the local key store.
type MetaStore interface {
	MetaGet(key string) (string, error)
	MetaSet(key, value string) error
}

// Vault encrypts secrets with an automatically generated local key.
// There is no password, lock, or authorization state.
type Vault struct {
	mu   sync.RWMutex
	key  []byte
	meta MetaStore
}

func NewVault(meta MetaStore) *Vault { return &Vault{meta: meta} }

func (v *Vault) keyCopy() ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.key) == 32 {
		out := make([]byte, 32)
		copy(out, v.key)
		return out, nil
	}

	stored, err := v.meta.MetaGet(keyMetaKey)
	if err != nil {
		return nil, err
	}
	if stored != "" {
		key, err := base64.StdEncoding.DecodeString(stored)
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("invalid local vault key")
		}
		v.key = key
	} else {
		v.key = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, v.key); err != nil {
			return nil, err
		}
		if err := v.meta.MetaSet(keyMetaKey, base64.StdEncoding.EncodeToString(v.key)); err != nil {
			return nil, err
		}
	}

	out := make([]byte, 32)
	copy(out, v.key)
	return out, nil
}

func (v *Vault) EncryptSecret(plaintext string) ([]byte, []byte, error) {
	key, err := v.keyCopy()
	if err != nil {
		return nil, nil, err
	}
	return encryptBytes([]byte(plaintext), key)
}

func (v *Vault) DecryptSecret(ciphertext, nonce []byte) (string, error) {
	key, err := v.keyCopy()
	if err != nil {
		return "", err
	}
	pt, err := decryptBytes(ciphertext, nonce, key)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

func encryptBytes(plaintext, key []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return gcm.Seal(nil, nonce, plaintext, nil), nonce, nil
}

func decryptBytes(ciphertext, nonce, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	pt, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return pt, nil
}
