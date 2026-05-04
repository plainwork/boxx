// Package envfile parses .env files into a key=value map.
// Syntax rules:
//   - Blank lines are ignored.
//   - Lines starting with # are ignored.
//   - KEY=VALUE: key is everything before the first '=', value is the rest.
//   - Surrounding whitespace on key and value is trimmed.
//   - Value may optionally be wrapped in single or double quotes (stripped).
package envfile

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Parse reads path and returns all key/value pairs found in the file.
func Parse(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("envfile: %w", err)
	}
	defer f.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 1 {
			return nil, fmt.Errorf("envfile: line %d: invalid format (expected KEY=VALUE)", lineNum)
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// strip optional surrounding quotes
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		result[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("envfile: %w", err)
	}
	return result, nil
}
