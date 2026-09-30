package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/plainwork/boxx/engine/envfile"
	"github.com/plainwork/boxx/engine/installer"
	"github.com/plainwork/boxx/engine/state"
	"github.com/spf13/cobra"
)

var (
	installHosts     []string
	installRedirects []string
	installDB        string
	installSlug      string
	installEnvFile   string
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

The first --host is the primary hostname. Repeat --host to serve the app on
more hostnames (including wildcards like *.example.com), and use
--redirect-host for hostnames that should 308-redirect to the primary.

Example:
  boxx install ghcr.io/acme/nurun-next:latest --host nurun.example.com --db mysql
  boxx install ghcr.io/acme/saas:latest --host example.com --host '*.example.com' --redirect-host www.example.com`,
	Args: cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		primary, aliases, err := hostsFromFlags(installHosts, installRedirects)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		spec := installer.SingleSpec{
			Image:    args[0],
			Hostname: primary,
			Aliases:  aliases,
			DBEngine: installDB,
			Slug:     installSlug,
		}
		if installEnvFile != "" {
			env, err := envfile.ParseFile(installEnvFile)
			if err != nil {
				return fmt.Errorf("--env-file: %w", err)
			}
			for k := range managedKeys {
				delete(env, k)
			}
			spec.Env = env
		}
		app, err := installer.InstallSingle(ctx, spec, func(step, msg string) {
			fmt.Fprintf(os.Stdout, "  [%-5s] %s\n", step, msg)
		})
		if err != nil {
			return err
		}
		printHostNotes(app.Slug, state.HostNames("", app.Aliases))
		return nil
	},
}

func init() {
	installCmd.Flags().StringSliceVar(&installHosts, "host", nil, "public hostname for the app (required; repeatable, first is primary)")
	installCmd.Flags().StringSliceVar(&installRedirects, "redirect-host", nil, "hostname that redirects to the primary (repeatable)")
	installCmd.Flags().StringVar(&installDB, "db", "", "provision a database: mysql or postgres (optional)")
	installCmd.Flags().StringVar(&installSlug, "slug", "", "override derived app slug (optional)")
	installCmd.Flags().StringVar(&installEnvFile, "env-file", "", "path to a .env file to inject into the container (optional)")
	rootCmd.AddCommand(installCmd)
}
