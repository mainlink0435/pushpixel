//go:build linux

package auth

import (
	"fmt"
	"os"
	"strings"
)

func readMachineID() (string, error) {
	paths := []string{"/etc/machine-id", "/var/lib/dbus/machine-id"}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			id := strings.TrimSpace(string(data))
			if id != "" {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("machine-id not found")
}
