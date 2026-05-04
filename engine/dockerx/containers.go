package dockerx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// ----- networks -----

// NetworkEnsure creates the named bridge network if it doesn't already exist.
func NetworkEnsure(ctx context.Context, name string) error {
	out, err := exec.CommandContext(ctx, "docker", "network", "ls", "--format", "{{.Name}}").Output()
	if err != nil {
		return err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == name {
			return nil
		}
	}
	cmd := exec.CommandContext(ctx, "docker", "network", "create", name)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("network create %s: %s", name, strings.TrimSpace(string(b)))
	}
	return nil
}

// ----- containers -----

// ContainerExists returns true if a container with the given name exists (any state).
func ContainerExists(ctx context.Context, name string) (bool, error) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "-a", "--filter", "name=^/"+name+"$", "--format", "{{.Names}}").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == name, nil
}

// ContainerRunning returns true if a container with the given name is currently running.
func ContainerRunning(ctx context.Context, name string) (bool, error) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "--filter", "name=^/"+name+"$", "--format", "{{.Names}}").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == name, nil
}

// RunOpts is the subset of `docker run` knobs boxx needs.
type RunOpts struct {
	Name     string
	Image    string
	Network  string
	Env      []string          // KEY=VALUE
	Ports    map[string]string // host -> container, e.g. "127.0.0.1:2019" -> "2019"
	Volumes  map[string]string // host_path_or_volume_name -> container_path
	Cmd      []string          // override CMD
	Restart  string            // "" | "always" | "unless-stopped"
}

// Run creates and starts a detached container. Fails if a container with the
// same name already exists — call Rm first if you intend to replace it.
func Run(ctx context.Context, opts RunOpts) error {
	args := []string{"run", "-d", "--name", opts.Name}
	if opts.Network != "" {
		args = append(args, "--network", opts.Network)
	}
	if opts.Restart != "" {
		args = append(args, "--restart", opts.Restart)
	}
	for _, e := range opts.Env {
		args = append(args, "-e", e)
	}
	for host, ctr := range opts.Ports {
		args = append(args, "-p", host+":"+ctr)
	}
	for src, dst := range opts.Volumes {
		args = append(args, "-v", src+":"+dst)
	}
	args = append(args, opts.Image)
	args = append(args, opts.Cmd...)

	if b, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("docker run %s: %s", opts.Name, strings.TrimSpace(string(b)))
	}
	return nil
}

// Restart restarts a running container. Missing container is not an error.
func Restart(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", "restart", name)
	if b, err := cmd.CombinedOutput(); err != nil {
		s := strings.TrimSpace(string(b))
		if strings.Contains(s, "No such container") {
			return nil
		}
		return errors.New(s)
	}
	return nil
}

// Stop sends SIGTERM (and after timeout SIGKILL) to a container. Missing container is not an error.
func Stop(ctx context.Context, name string, timeoutSec int) error {
	cmd := exec.CommandContext(ctx, "docker", "stop", "-t", fmt.Sprintf("%d", timeoutSec), name)
	if b, err := cmd.CombinedOutput(); err != nil {
		s := strings.TrimSpace(string(b))
		if strings.Contains(s, "No such container") {
			return nil
		}
		return errors.New(s)
	}
	return nil
}

// Rm removes a container. Missing container is not an error.
func Rm(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", name)
	if b, err := cmd.CombinedOutput(); err != nil {
		s := strings.TrimSpace(string(b))
		if strings.Contains(s, "No such container") {
			return nil
		}
		return errors.New(s)
	}
	return nil
}

// RmVolume removes a docker volume. Missing volume is not an error.
func RmVolume(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", "volume", "rm", "-f", name)
	if b, err := cmd.CombinedOutput(); err != nil {
		s := strings.TrimSpace(string(b))
		if strings.Contains(s, "No such volume") || strings.Contains(s, "no such volume") {
			return nil
		}
		return errors.New(s)
	}
	return nil
}

// Pull pulls an image, streaming nothing — caller decides whether to surface progress.
func Pull(ctx context.Context, image string) error {
	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	b, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	out := strings.TrimSpace(string(b))
	// Classify common failure modes so callers can surface actionable messages.
	switch {
	case strings.Contains(out, "unauthorized") || strings.Contains(out, "denied") ||
		strings.Contains(out, "authentication required") || strings.Contains(out, "403"):
		return fmt.Errorf("pull %s: authentication failed — run 'docker login %s' first\n  (%s)",
			image, registryHost(image), out)
	case strings.Contains(out, "not found") || strings.Contains(out, "manifest unknown") ||
		strings.Contains(out, "404"):
		return fmt.Errorf("pull %s: image not found — check the image name and tag\n  (%s)", image, out)
	case strings.Contains(out, "no such host") || strings.Contains(out, "connection refused") ||
		strings.Contains(out, "network") || strings.Contains(out, "dial"):
		return fmt.Errorf("pull %s: network error — check connectivity\n  (%s)", image, out)
	default:
		return fmt.Errorf("pull %s: %s", image, out)
	}
}

// registryHost extracts the registry hostname from an image reference.
// "ghcr.io/acme/app:tag" → "ghcr.io", "ubuntu:22.04" → "docker.io"
func registryHost(image string) string {
	ref := image
	if i := strings.Index(ref, "/"); i > 0 {
		host := ref[:i]
		// A registry host contains a dot or colon; otherwise it's a Docker Hub user
		if strings.ContainsAny(host, ".:") {
			return host
		}
	}
	return "docker.io"
}

// Exec runs a command inside a running container and returns stdout.
func Exec(ctx context.Context, name string, cmd ...string) ([]byte, error) {
	args := append([]string{"exec", name}, cmd...)
	c := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("exec %s: %s", name, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// Inspect returns the parsed JSON of `docker inspect <name>` (first object).
func Inspect(ctx context.Context, name string) (map[string]any, error) {
	out, err := exec.CommandContext(ctx, "docker", "inspect", name).Output()
	if err != nil {
		return nil, err
	}
	var arr []map[string]any
	if err := json.NewDecoder(bytes.NewReader(out)).Decode(&arr); err != nil {
		return nil, err
	}
	if len(arr) == 0 {
		return nil, errors.New("not found")
	}
	return arr[0], nil
}

// ContainerEnv returns the environment variables of a container as a KEY→value map.
// It reads them from `docker inspect` so the container doesn't need to be running.
func ContainerEnv(ctx context.Context, name string) (map[string]string, error) {
	info, err := Inspect(ctx, name)
	if err != nil {
		return nil, err
	}
	config, _ := info["Config"].(map[string]any)
	if config == nil {
		return nil, errors.New("no Config in inspect output")
	}
	envList, _ := config["Env"].([]any)
	out := make(map[string]string, len(envList))
	for _, v := range envList {
		s, _ := v.(string)
		if k, val, ok := strings.Cut(s, "="); ok {
			out[k] = val
		}
	}
	return out, nil
}

// LogsTail returns the last N lines from a container's logs.
func LogsTail(ctx context.Context, name string, n int) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "logs", "--tail", fmt.Sprintf("%d", n), name).CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

// ContainerStats holds a single snapshot from `docker stats --no-stream`.
type ContainerStats struct {
	CPUPerc    float64 // 0–100 per core, e.g. 2.5 = 2.5%
	CPUPeak    float64 // highest CPUPerc seen (computed by caller)
	MemUsage   uint64  // bytes in use
	MemLimit   uint64  // bytes total
	MemPeak    uint64  // highest MemUsage seen in bytes (computed by caller)
	NetIn      uint64  // cumulative bytes received
	NetOut     uint64  // cumulative bytes sent
	NetInRate  float64 // bytes per minute (computed by caller)
	NetOutRate float64 // bytes per minute (computed by caller)
	NetErrPerc float64 // error packet percentage (computed by caller)
}

// Stats fetches a single-shot stats snapshot for each of the named containers.
// Missing or stopped containers are silently omitted from the result map.
func Stats(ctx context.Context, names []string) (map[string]ContainerStats, error) {
	if len(names) == 0 {
		return map[string]ContainerStats{}, nil
	}
	// Note: {{.MemUsage}} outputs "128MiB / 7.6GiB" (usage/limit combined).
	// There is no separate {{.MemLimit}} template field in docker stats.
	args := append([]string{
		"stats", "--no-stream",
		"--format", `{"name":"{{.Name}}","cpu":"{{.CPUPerc}}","mem":"{{.MemUsage}}","net":"{{.NetIO}}"}`,
	}, names...)
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		// Non-zero exit usually means no containers found — return empty map.
		return map[string]ContainerStats{}, nil
	}
	result := make(map[string]ContainerStats)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw struct {
			Name string `json:"name"`
			CPU  string `json:"cpu"`
			Mem  string `json:"mem"`
			Net  string `json:"net"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		s := ContainerStats{
			CPUPerc: parsePercent(raw.CPU),
		}
		// MemUsage format: "128MiB / 7.6GiB"
		memParts := strings.SplitN(raw.Mem, " / ", 2)
		if len(memParts) == 2 {
			s.MemUsage = parseBytes(memParts[0])
			s.MemLimit = parseBytes(memParts[1])
		}
		// NetIO format: "1.2MB / 3.4kB"
		netParts := strings.SplitN(raw.Net, " / ", 2)
		if len(netParts) == 2 {
			s.NetIn = parseBytes(netParts[0])
			s.NetOut = parseBytes(netParts[1])
		}
		result[raw.Name] = s
	}
	return result, nil
}

// parsePercent converts "2.50%" → 2.5.
func parsePercent(s string) float64 {
	s = strings.TrimSuffix(strings.TrimSpace(s), "%")
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

// parseBytes converts docker human strings like "128MiB", "1.2GB", "512B" to bytes.
func parseBytes(s string) uint64 {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		factor uint64
	}{
		{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10},
		{"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3},
		{"G", 1 << 30}, {"M", 1 << 20}, {"k", 1 << 10}, {"B", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			num := strings.TrimSuffix(s, u.suffix)
			var f float64
			fmt.Sscanf(num, "%f", &f)
			return uint64(f * float64(u.factor))
		}
	}
	var n uint64
	fmt.Sscanf(s, "%d", &n)
	return n
}

// Discard is a convenience io.Writer for callers that want to swallow command output.
var Discard io.Writer = io.Discard
