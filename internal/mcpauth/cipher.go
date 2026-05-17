package mcpauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

type TokenCipher struct {
	aead cipher.AEAD
}

func NewTokenCipher(key []byte) (*TokenCipher, error) {
	block, err := aes.NewCipher(key[:32])
	if err != nil {
		return nil, fmt.Errorf("new aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return &TokenCipher{aead: aead}, nil
}

func (c *TokenCipher) EncryptToBase64(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}
	ciphertext := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (c *TokenCipher) DecryptFromBase64(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := raw[:ns], raw[ns:]
	plain, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt token: %w", err)
	}
	return string(plain), nil
}
