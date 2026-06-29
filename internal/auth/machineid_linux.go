//go:build linux

package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func readMachineID(tokenDir string) (string, error) {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		data, err := os.ReadFile(p)
		if err == nil {
			id := strings.TrimSpace(string(data))
			if id != "" {
				return id, nil
			}
		}
	}

	keyPath := filepath.Join(tokenDir, ".machine-key")
	if data, err := os.ReadFile(keyPath); err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id, nil
		}
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("generate machine key: %w", err)
	}
	keyHex := hex.EncodeToString(key)
	if err := os.WriteFile(keyPath, []byte(keyHex), 0600); err != nil {
		return "", fmt.Errorf("write machine key: %w", err)
	}
	return keyHex, nil
}
