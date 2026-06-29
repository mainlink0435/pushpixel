//go:build !windows && !linux

package auth

import (
	"fmt"
	"os"
)

func readMachineID(_ string) (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("hostname: %w", err)
	}
	return hostname, nil
}
