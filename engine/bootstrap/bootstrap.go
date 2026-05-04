// Package bootstrap detects the host environment and (eventually) installs
// the prerequisites boxx needs to run — primarily Docker.
package bootstrap

import (
	"bufio"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// Host describes the machine boxx is running on.
type Host struct {
	OS     string // "darwin", "linux"
	Arch   string // "amd64", "arm64", "arm"
	Distro string // "macOS", "ubuntu", "debian", "raspbian", "alpine", "unknown"
}

// Detect inspects the running system. It never errors — unknown fields are returned as "unknown".
func Detect() Host {
	h := Host{OS: runtime.GOOS, Arch: runtime.GOARCH, Distro: "unknown"}
	switch runtime.GOOS {
	case "darwin":
		h.Distro = "macOS"
	case "linux":
		h.Distro = linuxDistro()
	}
	return h
}

func linuxDistro() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return "unknown"
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if v, ok := strings.CutPrefix(line, "ID="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return "unknown"
}

// PortFree returns true if nothing is currently listening on the given TCP port.
func PortFree(port int) bool {
	l, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// DiskUsage returns (free, total) bytes available at path.
func DiskUsage(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return st.Bavail * uint64(st.Bsize), st.Blocks * uint64(st.Bsize), nil
}
