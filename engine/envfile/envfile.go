// Package envfile parses and formats .env files.
//
// Format rules:
//   - KEY=VALUE (unquoted or quoted with " or ')
//   - Lines starting with # are comments (ignored)
//   - Empty lines are ignored
//   - Inline comments after values are NOT supported (avoids ambiguity)
package envfile

import (
	"fmt"
	"sort"
	"strings"
)

// ParseFile is an alias for Parse (which takes a file path).
func ParseFile(path string) (map[string]string, error) {
	return Parse(path)
}

// Format serialises a map to .env text (keys sorted for determinism).
// Values containing spaces, tabs, quotes, or # are double-quoted.
func Format(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		v := env[k]
		if strings.ContainsAny(v, " \t#\"'\\") {
			sb.WriteString(fmt.Sprintf("%s=%q\n", k, v))
		} else {
			fmt.Fprintf(&sb, "%s=%s\n", k, v)
		}
	}
	return sb.String()
}
