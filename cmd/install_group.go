package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/plainwork/boxx/engine/envfile"
	"github.com/plainwork/boxx/engine/installer"
	"github.com/spf13/cobra"
)

var (
	groupHost    string
	groupDB      string
	groupSlug    string
	groupApps    []string // each "<image>=<path>"
	groupEnvFile string
)

var installGroupCmd = &cobra.Command{
	Use:   "install-group",
	Short: "Install a group of apps behind a single hostname",
	Long: `Install multiple apps behind one hostname, each handling a distinct path prefix.

A group provisions ONE shared database (when --db is given), and each app
receives the same DATABASE_URL.

Each --app is "<image>=<path>". Use "/" for the root path.

Example:
  boxx install-group \
    --host nurun.example.com --db mysql \
    --app ghcr.io/acme/nurun-next:latest=/ \
    --app ghcr.io/acme/nurun-admin:latest=/admin`,
	RunE: func(c *cobra.Command, args []string) error {
			if groupHost == "" {
				return fmt.Errorf("--host is required")
			}
			if len(groupApps) == 0 {
				return fmt.Errorf("at least one --app is required")
			}
			spec := installer.GroupSpec{
				Hostname: groupHost,
				DBEngine: groupDB,
				Slug:     groupSlug,
			}
			// parse env file once; applied to every app in the group
			var sharedEnv map[string]string
			if groupEnvFile != "" {
				var err error
				sharedEnv, err = envfile.ParseFile(groupEnvFile)
				if err != nil {
					return fmt.Errorf("--env-file: %w", err)
				}
			}
			for _, raw := range groupApps {
				i := strings.LastIndex(raw, "=")
				if i < 0 {
					return fmt.Errorf("--app must be \"<image>=<path>\": got %q", raw)
				}
				spec.Apps = append(spec.Apps, installer.GroupApp{
					Image: raw[:i],
					Path:  raw[i+1:],
					Env:   sharedEnv,
				})
			}

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			_, err := installer.InstallGroup(ctx, spec, func(step, msg string) {
				fmt.Fprintf(os.Stdout, "  [%-5s] %s\n", step, msg)
			})
			return err
		},
}

func init() {
	installGroupCmd.Flags().StringVar(&groupHost, "host", "", "public hostname for the group (required)")
	installGroupCmd.Flags().StringVar(&groupDB, "db", "", "shared database engine: mysql or postgres (optional)")
	installGroupCmd.Flags().StringVar(&groupSlug, "slug", "", "override derived group slug (optional)")
	installGroupCmd.Flags().StringArrayVar(&groupApps, "app", nil, "app to install: \"<image>=<path>\" (repeatable)")
	installGroupCmd.Flags().StringVar(&groupEnvFile, "env-file", "", "path to a .env file injected into every app in the group (optional)")
	rootCmd.AddCommand(installGroupCmd)
}
