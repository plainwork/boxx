//go:build darwin

package bootstrap

import (
	"os/exec"
	"strconv"
	"strings"
)

// MemTotal returns total physical memory in bytes.
func MemTotal() (uint64, error) {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
}
