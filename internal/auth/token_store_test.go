package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	token := &oauth2.Token{
		AccessToken:  "access-123",
		TokenType:    "Bearer",
		RefreshToken: "refresh-456",
		Expiry:       time.Now().Add(1 * time.Hour),
	}

	if err := store.Save(token); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.AccessToken != token.AccessToken {
		t.Errorf("access token: got %s, want %s", loaded.AccessToken, token.AccessToken)
	}
	if loaded.RefreshToken != token.RefreshToken {
		t.Errorf("refresh token: got %s, want %s", loaded.RefreshToken, token.RefreshToken)
	}
	if loaded.TokenType != token.TokenType {
		t.Errorf("token type: got %s, want %s", loaded.TokenType, token.TokenType)
	}
}

func TestLoad_NotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	_, err = store.Load()
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	token := &oauth2.Token{AccessToken: "test"}
	if err := store.Save(token); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := store.Delete(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = store.Load()
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestDelete_NoFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if err := store.Delete(); err != nil {
		t.Fatalf("delete without file: %v", err)
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	original := []byte("hello pushpixel token data")
	encrypted, err := encrypt(key, original)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decrypted, err := decrypt(key, encrypted)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(decrypted) != string(original) {
		t.Errorf("got %s, want %s", decrypted, original)
	}
}

func TestEncrypt_WrongKeyFails(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	key2[0] = 1

	encrypted, err := encrypt(key1, []byte("secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = decrypt(key2, encrypted)
	if err == nil {
		t.Fatal("expected error with wrong key")
	}
}

func TestFilePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}

	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	token := &oauth2.Token{AccessToken: "test"}
	if err := store.Save(token); err != nil {
		t.Fatalf("save: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "token.enc"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if info.Mode()&0077 != 0 {
		t.Errorf("expected no group/other permissions, got %v", info.Mode())
	}
}

func TestSaveOverwrites(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if err := store.Save(&oauth2.Token{AccessToken: "first"}); err != nil {
		t.Fatalf("save first: %v", err)
	}
	if err := store.Save(&oauth2.Token{AccessToken: "second"}); err != nil {
		t.Fatalf("save second: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.AccessToken != "second" {
		t.Errorf("expected overwritten token, got %s", loaded.AccessToken)
	}
}
