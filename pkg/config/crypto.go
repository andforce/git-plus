package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/scrypt"
)

const (
	encryptedTokenPrefix = "$encrypted$1$"
	encryptionSaltSize   = 16
	encryptionKeySize    = 32
)

var (
	errUnencryptedToken      = errors.New("token must use encrypted format")
	errPassphraseRequired    = errors.New("token passphrase is required")
	errInvalidEncryptedToken = errors.New("invalid encrypted token")
	errTokenDecryptionFailed = errors.New("token decryption failed")
)

func EncryptToken(plaintext string, passphrase string) (string, error) {
	if passphrase == "" {
		return "", errPassphraseRequired
	}

	salt := make([]byte, encryptionSaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	key, err := deriveEncryptionKey(passphrase, salt)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(append(salt, nonce...), ciphertext...)

	return encryptedTokenPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func DecryptToken(ciphertext string, passphrase string) (string, error) {
	payload, err := parseEncryptedTokenPayload(ciphertext)
	if err != nil {
		return "", err
	}
	if passphrase == "" {
		return "", errPassphraseRequired
	}

	if len(payload) <= encryptionSaltSize {
		return "", errInvalidEncryptedToken
	}

	salt := payload[:encryptionSaltSize]
	key, err := deriveEncryptionKey(passphrase, salt)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	minPayloadSize := encryptionSaltSize + gcm.NonceSize() + gcm.Overhead()
	if len(payload) < minPayloadSize {
		return "", errInvalidEncryptedToken
	}

	nonce := payload[encryptionSaltSize : encryptionSaltSize+gcm.NonceSize()]
	encryptedData := payload[encryptionSaltSize+gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return "", errTokenDecryptionFailed
	}

	return string(plaintext), nil
}

func IsEncryptedToken(value string) bool {
	return strings.HasPrefix(value, encryptedTokenPrefix)
}

func parseEncryptedTokenPayload(value string) ([]byte, error) {
	if !IsEncryptedToken(value) {
		return nil, errUnencryptedToken
	}

	payload := strings.TrimPrefix(value, encryptedTokenPrefix)
	if payload == "" {
		return nil, errInvalidEncryptedToken
	}

	decodedPayload, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, errInvalidEncryptedToken
	}

	return decodedPayload, nil
}

func deriveEncryptionKey(passphrase string, salt []byte) ([]byte, error) {
	key, err := scrypt.Key([]byte(passphrase), salt, 32768, 8, 1, encryptionKeySize)
	if err != nil {
		return nil, fmt.Errorf("derive encryption key: %w", err)
	}

	return key, nil
}

func resolveConfigSecrets(cfg Config, opts SecretOptions) (Config, error) {
	resolved := cfg
	resolved.Sources = make([]SourceConfig, len(cfg.Sources))

	for index, source := range cfg.Sources {
		resolvedSource := source
		if strings.TrimSpace(source.Token) == "" {
			return Config{}, fmt.Errorf("source %d token is required", index)
		}
		if !IsEncryptedToken(source.Token) {
			return Config{}, fmt.Errorf("source %d token must use %s format", index, encryptedTokenPrefix+"...")
		}

		plaintext, err := DecryptToken(source.Token, opts.Passphrase)
		if err != nil {
			return Config{}, fmt.Errorf("resolve source %d token: %w", index, err)
		}

		resolvedSource.Token = plaintext
		resolved.Sources[index] = resolvedSource
	}

	return resolved, nil
}
