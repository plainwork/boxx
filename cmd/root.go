package cmd

import (
	"fmt"

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
