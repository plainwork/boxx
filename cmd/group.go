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
	groupAddApps    []string // each "<image>=<path>"
	groupAddEnvFile string
)

var groupCmd = &cobra.Command{
	Use:   "group",
	Short: "Manage the apps in a group",
}

var groupAddCmd = &cobra.Command{
	Use:   "add <group>",
	Short: "Add apps to an installed group",
	Long: `Add apps to a group that is already installed. The new apps answer the
group's hostnames on their own path prefix and get the group's DATABASE_URL.
The apps already in the group keep running untouched.

Each --app is "<image>=<path>", as in install-group. A path may not be one the
group already serves.

Example:
  boxx group add nurun-example-com \
    --app ghcr.io/acme/nurun-docs:latest=/docs`,
	Args: cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		if len(groupAddApps) == 0 {
			return fmt.Errorf("at least one --app is required")
		}
		var env map[string]string
		if groupAddEnvFile != "" {
			var err error
			env, err = envfile.ParseFile(groupAddEnvFile)
			if err != nil {
				return fmt.Errorf("--env-file: %w", err)
			}
			for k := range managedKeys {
				delete(env, k)
			}
		}
		apps, err := parseGroupApps(groupAddApps, env)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		_, err = installer.AddGroupApps(ctx, args[0], apps, func(step, msg string) {
			fmt.Fprintf(os.Stdout, "  [%-5s] %s\n", step, msg)
		})
		return err
	},
}

// parseGroupApps turns --app "<image>=<path>" values into group apps sharing env.
func parseGroupApps(raws []string, env map[string]string) ([]installer.GroupApp, error) {
	apps := make([]installer.GroupApp, 0, len(raws))
	for _, raw := range raws {
		i := strings.LastIndex(raw, "=")
		if i < 0 {
			return nil, fmt.Errorf("--app must be \"<image>=<path>\": got %q", raw)
		}
		apps = append(apps, installer.GroupApp{Image: raw[:i], Path: raw[i+1:], Env: env})
	}
	return apps, nil
}

func init() {
	groupAddCmd.Flags().StringArrayVar(&groupAddApps, "app", nil, "app to add: \"<image>=<path>\" (repeatable)")
	groupAddCmd.Flags().StringVar(&groupAddEnvFile, "env-file", "", "path to a .env file injected into each new app (optional)")
	groupCmd.AddCommand(groupAddCmd)
	rootCmd.AddCommand(groupCmd)
}
