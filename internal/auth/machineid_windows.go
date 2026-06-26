//go:build windows

package auth

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func readMachineID() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.READ)
	if err != nil {
		return "", fmt.Errorf("open registry key: %w", err)
	}
	defer k.Close()

	guid, _, err := k.GetStringValue("MachineGuid")
	if err != nil {
		return "", fmt.Errorf("read MachineGuid: %w", err)
	}
	return strings.TrimSpace(guid), nil
}
