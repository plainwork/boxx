package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/plainwork/boxx/engine/opslog"
	"github.com/plainwork/boxx/engine/release"
	"github.com/plainwork/boxx/tui"
	"github.com/spf13/cobra"
)

var boxxVersion = "dev"

// SetVersion is called from main to inject the build-time version.
func SetVersion(v string) {
	boxxVersion = v
	tui.SetVersion(v)
}

var rootCmd = &cobra.Command{
	Use:   "boxx",
	Short: "boxx — install and run dockerized apps anywhere, simply.",
	Long:  `boxx is a tiny TUI + CLI that installs and orchestrates dockerized apps on a single host.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(c *cobra.Command, args []string) {
		// Prune ops log to a rolling 7-day window (best-effort).
		opslog.Prune(7 * 24 * time.Hour) //nolint:errcheck
		// Start async refresh of the self-update cache for next invocation.
		release.RefreshAsync()
	},
	PersistentPostRun: func(c *cobra.Command, args []string) {
		// Print a non-intrusive upgrade notice when a newer version is cached.
		tag := release.Cached()
		if release.IsNewer(tag, boxxVersion) {
			fmt.Fprintf(os.Stderr, "\n  boxx %s available — run `boxx upgrade` to install\n", tag)
		}
	},
	RunE: func(c *cobra.Command, args []string) error {
		return tui.Run()
	},
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the boxx version",
		Run: func(c *cobra.Command, args []string) {
			fmt.Println(boxxVersion)
		},
	})
}
