//go:build linux

package bootstrap

import "syscall"

// MemTotal returns total physical memory in bytes.
func MemTotal() (uint64, error) {
	var info syscall.Sysinfo_t
	if err := syscall.Sysinfo(&info); err != nil {
		return 0, err
	}
	return uint64(info.Totalram) * uint64(info.Unit), nil
}
