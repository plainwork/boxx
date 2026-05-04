// Package util holds small helpers shared across the engine.
package util

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

var slugBad = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify converts an arbitrary string into a safe DNS/container-friendly slug.
//
//	"GHCR.io/Acme/Nurun-Admin:v2" -> "nurun-admin"
//	"foo.example.com"             -> "foo-example-com"
func Slugify(s string) string {
	s = strings.ToLower(s)
	// strip image registry/path/tag noise: keep only the last path segment, drop tag
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.IndexAny(s, ":@"); i >= 0 {
		s = s[:i]
	}
	s = slugBad.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "app"
	}
	return s
}

// RandomPassword returns a 32-char hex string suitable for a generated DB password.
func RandomPassword() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RegistryHost returns the registry host for an image reference, or "" for Docker Hub.
//
//	"ghcr.io/acme/foo:v1" -> "ghcr.io"
//	"acme/foo"            -> ""
//	"foo"                 -> ""
func RegistryHost(image string) string {
	first := image
	if i := strings.Index(image, "/"); i >= 0 {
		first = image[:i]
	} else {
		return ""
	}
	if strings.ContainsAny(first, ".:") || first == "localhost" {
		return first
	}
	return ""
}
