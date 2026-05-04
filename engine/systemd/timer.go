// Package systemd manages the boxx-updates systemd timer on Linux.
// On non-Linux hosts the functions return ErrNotLinux so callers can print a
// friendly message instead of silently failing.
package systemd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ErrNotLinux is returned when the host is not Linux.
var ErrNotLinux = errors.New("systemd timer management is only supported on Linux")

const (
	serviceName = "boxx-updates"
	serviceFile = "/etc/systemd/system/boxx-updates.service"
	timerFile   = "/etc/systemd/system/boxx-updates.timer"
)

var serviceUnit = `[Unit]
Description=boxx automatic update check
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/boxx updates run
StandardOutput=journal
StandardError=journal
`

var timerUnit = `[Unit]
Description=boxx automatic update check (every 12 h)

[Timer]
OnBootSec=5min
OnUnitActiveSec=12h
Unit=boxx-updates.service

[Install]
WantedBy=timers.target
`

// Install writes the service + timer unit files and enables/starts the timer.
func Install() error {
	if runtime.GOOS != "linux" {
		return ErrNotLinux
	}
	if err := os.WriteFile(serviceFile, []byte(serviceUnit), 0644); err != nil {
		return fmt.Errorf("write service unit: %w", err)
	}
	if err := os.WriteFile(timerFile, []byte(timerUnit), 0644); err != nil {
		return fmt.Errorf("write timer unit: %w", err)
	}
	cmds := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "--now", serviceName + ".timer"},
	}
	for _, args := range cmds {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w\n%s", strings.Join(args, " "), err, out)
		}
	}
	return nil
}

// Remove disables and removes the timer and service unit files.
func Remove() error {
	if runtime.GOOS != "linux" {
		return ErrNotLinux
	}
	// Ignore errors — unit may not be installed.
	_ = exec.Command("systemctl", "disable", "--now", serviceName+".timer").Run()
	_ = os.Remove(timerFile)
	_ = os.Remove(serviceFile)
	_ = exec.Command("systemctl", "daemon-reload").Run()
	return nil
}

// Status returns a human-readable status string for the timer.
func Status() (string, error) {
	if runtime.GOOS != "linux" {
		return "", ErrNotLinux
	}
	out, err := exec.Command("systemctl", "status", serviceName+".timer").CombinedOutput()
	if err != nil {
		// status exits non-zero when unit is inactive/not-found; still return output.
		return string(out), nil
	}
	return string(out), nil
}
