package dockerx

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// HostInfo holds capacity figures for the Docker host.
type HostInfo struct {
	CPUs      int    // logical CPU count visible to Docker
	MemTotal  uint64 // total RAM in bytes
	DiskTotal uint64 // total bytes on the root filesystem
	DiskFree  uint64
}

// CPULabel returns a short display string, e.g. "8 cores".
func (h HostInfo) CPULabel() string {
	if h.CPUs == 0 {
		return ""
	}
	return fmt.Sprintf("%d cores", h.CPUs)
}

// MemLabel returns a short display string, e.g. "7.8G".
func (h HostInfo) MemLabel() string { return fmtCapacity(h.MemTotal) }

// DiskLabel returns a short display string, e.g. "1.8T".
func (h HostInfo) DiskLabel() string { return fmtCapacity(h.DiskTotal) }

// GetHostInfo fetches CPU and memory from `docker info` and disk from the OS.
func GetHostInfo(ctx context.Context) (HostInfo, error) {
	out, err := exec.CommandContext(ctx, "docker", "info",
		"--format", "{{.NCPU}} {{.MemTotal}}").Output()
	if err != nil {
		return HostInfo{}, err
	}

	parts := strings.Fields(strings.TrimSpace(string(out)))
	var h HostInfo
	if len(parts) >= 1 {
		h.CPUs, _ = strconv.Atoi(parts[0])
	}
	if len(parts) >= 2 {
		v, _ := strconv.ParseUint(parts[1], 10, 64)
		h.MemTotal = v
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		bs := uint64(stat.Bsize)
		h.DiskTotal = stat.Blocks * bs
		h.DiskFree = stat.Bfree * bs
	}

	return h, nil
}

// FmtCap formats bytes as a short human-readable capacity string.
func FmtCap(b uint64) string { return fmtCapacity(b) }

// fmtCapacity formats bytes as a short human-readable capacity string.
func fmtCapacity(b uint64) string {
	const (
		_  = iota
		KB = 1 << (10 * iota)
		MB
		GB
		TB
	)
	switch {
	case b >= TB:
		return fmt.Sprintf("%.1fT", float64(b)/TB)
	case b >= GB:
		return fmt.Sprintf("%.1fG", float64(b)/GB)
	case b >= MB:
		return fmt.Sprintf("%.1fM", float64(b)/MB)
	default:
		return fmt.Sprintf("%dK", b/KB)
	}
}
