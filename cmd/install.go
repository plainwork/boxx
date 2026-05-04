package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/plainwork/boxx/engine/envfile"
	"github.com/plainwork/boxx/engine/installer"
	"github.com/spf13/cobra"
)

var (
	installHost    string
	installDB      string
	installSlug    string
	installEnvFile string
)

var installCmd = &cobra.Command{
	Use:   "install <image>",
	Short: "Install a single app from a docker image",
	Long: `Install a single app behind the boxx Caddy proxy.

The image must follow the boxx contract:
  • listens on port 80
  • serves GET /up returning 2xx
  • persists data under /storage
  • reads DATABASE_URL when --db is given

Example:
  boxx install ghcr.io/acme/nurun-next:latest --host nurun.example.com --db mysql`,
	Args: cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		if installHost == "" {
			return fmt.Errorf("--host is required")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		spec := installer.SingleSpec{
			Image:    args[0],
			Hostname: installHost,
			DBEngine: installDB,
			Slug:     installSlug,
		}
		if installEnvFile != "" {
			env, err := envfile.ParseFile(installEnvFile)
			if err != nil {
				return fmt.Errorf("--env-file: %w", err)
			}
			spec.Env = env
		}
		_, err := installer.InstallSingle(ctx, spec, func(step, msg string) {
			fmt.Fprintf(os.Stdout, "  [%-5s] %s\n", step, msg)
		})
		return err
	},
}

func init() {
	installCmd.Flags().StringVar(&installHost, "host", "", "public hostname for the app (required)")
	installCmd.Flags().StringVar(&installDB, "db", "", "provision a database: mysql or postgres (optional)")
	installCmd.Flags().StringVar(&installSlug, "slug", "", "override derived app slug (optional)")
	installCmd.Flags().StringVar(&installEnvFile, "env-file", "", "path to a .env file to inject into the container (optional)")
	rootCmd.AddCommand(installCmd)
}
