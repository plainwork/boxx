package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/plainwork/boxx/engine/installer"
	"github.com/spf13/cobra"
)

var (
	deployImage string
	deployDrain int
)

var deployCmd = &cobra.Command{
	Use:   "deploy <slug>",
	Short: "Zero-downtime deploy of an installed app",
	Long: `Redeploy an installed app with zero downtime using blue/green:
launch the new container, wait for /up, atomically swap the proxy, then
drain and remove the old container.

For grouped apps use "<group>/<app>" form, e.g.:
  boxx deploy nurun-example-com/nurun-admin --image ghcr.io/acme/nurun-admin:v2`,
	Args: cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		return installer.Deploy(ctx, installer.DeploySpec{
			Slug:      args[0],
			NewImage:  deployImage,
			DrainSecs: deployDrain,
		}, func(step, msg string) {
			fmt.Fprintf(os.Stdout, "  [%-5s] %s\n", step, msg)
		})
	},
}

func init() {
	deployCmd.Flags().StringVar(&deployImage, "image", "", "deploy a new image (defaults to re-pulling current tag)")
	deployCmd.Flags().IntVar(&deployDrain, "drain", 10, "seconds to drain before removing the old container")
	rootCmd.AddCommand(deployCmd)
}
