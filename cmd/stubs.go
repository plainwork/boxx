package cmd

import (
	"errors"

	"github.com/spf13/cobra"
)

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "Install Docker and prepare this host (Linux/Pi only)",
	RunE: func(c *cobra.Command, args []string) error {
		return errors.New("bootstrap: not implemented yet (Phase 1.3)")
	},
}

func init() {
	rootCmd.AddCommand(bootstrapCmd)
}
