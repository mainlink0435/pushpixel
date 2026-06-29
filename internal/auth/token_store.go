package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/oauth2"
)

type TokenStore interface {
	Save(token *oauth2.Token) error
	Load() (*oauth2.Token, error)
	Delete() error
}

type FileTokenStore struct {
	path string
	key  []byte
}

func NewFileTokenStore(dir string) (*FileTokenStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create token dir: %w", err)
	}

	key, err := machineKey(dir)
	if err != nil {
		return nil, fmt.Errorf("derive machine key: %w", err)
	}

	return &FileTokenStore{
		path: filepath.Join(dir, "token.enc"),
		key:  key,
	}, nil
}

func (s *FileTokenStore) Save(token *oauth2.Token) error {
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	encrypted, err := encrypt(s.key, data)
	if err != nil {
		return fmt.Errorf("encrypt token: %w", err)
	}

	if err := os.WriteFile(s.path, encrypted, 0600); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}

	return nil
}

func (s *FileTokenStore) Load() (*oauth2.Token, error) {
	encrypted, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("token not found")
		}
		return nil, fmt.Errorf("read token file: %w", err)
	}

	data, err := decrypt(s.key, encrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt token: %w", err)
	}

	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}

	return &token, nil
}

func (s *FileTokenStore) Delete() error {
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete token file: %w", err)
	}
	return nil
}

func machineKey(tokenDir string) ([]byte, error) {
	machineID, err := readMachineID(tokenDir)
	if err != nil {
		return nil, err
	}

	salt := []byte("pushpixel-token-v1")
	info := []byte("aes-256-gcm-key")

	hkdf := hkdf.New(sha256.New, []byte(machineID), salt, info)
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf, key); err != nil {
		return nil, fmt.Errorf("hkdf derive: %w", err)
	}

	return key, nil
}

func encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := aesGCM.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

func decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := aesGCM.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt failed: %w", err)
	}

	return plaintext, nil
}
