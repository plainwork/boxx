package dockerx

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// IsMutableTag returns true when the image uses a tag that registries commonly
// overwrite (latest, dev, main, master, edge, or a bare semver channel like
// "1", "1.2", "1.2.3"). Returns false for digest-pinned refs (@sha256:...).
func IsMutableTag(image string) bool {
	if strings.Contains(image, "@sha256:") {
		return false
	}
	colon := strings.LastIndex(image, ":")
	if colon < 0 {
		return true // no tag at all == :latest behaviour
	}
	tag := image[colon+1:]
	for _, m := range []string{"latest", "dev", "main", "master", "edge", "stable", "nightly", "release"} {
		if strings.EqualFold(tag, m) {
			return true
		}
	}
	// Bare semver channels: "1", "1.2", "1.2.3" — purely numeric + dots
	for _, c := range tag {
		if c != '.' && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// RemoteDigest returns the manifest digest for an image tag without pulling the
// full image. It uses `docker manifest inspect --verbose` and returns the digest
// of the linux/amd64 platform manifest, or the first entry for single-arch images.
//
// Returns empty string if the digest cannot be determined (e.g. registry
// unreachable, auth failure). The caller should treat "" as "cannot tell".
func RemoteDigest(ctx context.Context, image string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "manifest", "inspect", "--verbose", image).Output()
	if err != nil {
		return "", fmt.Errorf("manifest inspect %s: %w", image, err)
	}

	// docker manifest inspect --verbose returns a JSON array for multi-arch
	// images and a single JSON object for single-arch images.
	type descriptor struct {
		Digest   string `json:"digest"`
		Platform struct {
			OS   string `json:"os"`
			Arch string `json:"architecture"`
		} `json:"platform"`
	}
	type entry struct {
		Descriptor descriptor `json:"Descriptor"`
	}

	// Try array (multi-arch manifest list)
	var entries []entry
	if json.Unmarshal(out, &entries) == nil && len(entries) > 0 {
		// Prefer linux/amd64; fall back to first entry
		for _, e := range entries {
			if e.Descriptor.Platform.OS == "linux" && e.Descriptor.Platform.Arch == "amd64" {
				return e.Descriptor.Digest, nil
			}
		}
		return entries[0].Descriptor.Digest, nil
	}

	// Try single-arch object
	var single entry
	if json.Unmarshal(out, &single) == nil && single.Descriptor.Digest != "" {
		return single.Descriptor.Digest, nil
	}

	return "", fmt.Errorf("could not parse manifest for %s", image)
}

// LocalDigest returns the content digest of an already-pulled image via
// `docker image inspect`. Returns empty string if the image is not locally
// present or has no RepoDigests (e.g. was built locally).
func LocalDigest(ctx context.Context, image string) string {
	out, err := exec.CommandContext(ctx,
		"docker", "image", "inspect", "--format", "{{index .RepoDigests 0}}", image,
	).Output()
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(out))
	if ref == "" || ref == "<no value>" {
		return ""
	}
	// RepoDigests format: "registry/repo@sha256:abc123"
	if i := strings.Index(ref, "@"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}
