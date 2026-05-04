package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const upgradeRepo = "plainwork/boxx"

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade boxx to the latest release",
	RunE: func(c *cobra.Command, args []string) error {
		return runUpgrade()
	},
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade() error {
	client := &http.Client{Timeout: 30 * time.Second}

	// 1. Resolve latest tag from GitHub API
	fmt.Println("checking for latest release…")
	tag, err := latestTag(client)
	if err != nil {
		return fmt.Errorf("could not check for updates: %w", err)
	}

	current := boxxVersion
	if tag == current || tag == "v"+strings.TrimPrefix(current, "v") {
		fmt.Printf("  already up to date (%s)\n", current)
		return nil
	}
	fmt.Printf("  current: %s\n  latest:  %s\n", current, tag)

	// 2. Build download URL
	os_ := runtime.GOOS
	arch := runtime.GOARCH
	archSuffix := arch
	if arch == "arm" {
		archSuffix = "armv7"
	}
	archive := fmt.Sprintf("boxx_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), os_, archSuffix)
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", upgradeRepo, tag, archive)

	fmt.Printf("downloading %s…\n", tag)

	// 3. Download
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d for %s", resp.StatusCode, url)
	}

	// 4. Extract the boxx binary from the tarball
	binary, err := extractBinary(resp.Body)
	if err != nil {
		return fmt.Errorf("extract failed: %w", err)
	}

	// 5. Find where the current executable lives
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine executable path: %w", err)
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return fmt.Errorf("could not resolve executable path: %w", err)
	}

	// 6. Write to a temp file beside the current binary, then rename over it.
	//    On Linux, rename is atomic and works even while the old binary is running
	//    because the kernel holds the old inode open.
	dir := filepath.Dir(self)
	tmp, err := os.CreateTemp(dir, ".boxx-upgrade-*")
	if err != nil {
		return fmt.Errorf("could not write temp file (try with sudo?): %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName) // no-op if rename succeeded
	}()

	if _, err := tmp.Write(binary); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	if err := tmp.Chmod(0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}
	tmp.Close()

	if err := os.Rename(tmpName, self); err != nil {
		return fmt.Errorf("could not replace binary (try with sudo?): %w", err)
	}

	fmt.Printf("  ✓ upgraded to %s at %s\n", tag, self)
	return nil
}

func latestTag(client *http.Client) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", upgradeRepo)
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("no releases found")
	}
	return payload.TagName, nil
}

func extractBinary(r io.Reader) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		// The binary is named "boxx" (no extension) at the top level of the archive
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == "boxx" {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("boxx binary not found in archive")
}
