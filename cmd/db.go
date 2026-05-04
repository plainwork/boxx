package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/plainwork/boxx/engine/db"
	"github.com/plainwork/boxx/engine/dockerx"
	"github.com/plainwork/boxx/engine/state"
	"github.com/plainwork/boxx/engine/util"
	"github.com/spf13/cobra"
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database management commands",
}

var dbResetUserCmd = &cobra.Command{
	Use:   "reset-user <slug>",
	Short: "Re-grant database credentials for an installed app",
	Long: `Re-grants the app user's credentials on the database container.

Useful when a MySQL container is getting "Access denied" errors after a restart.
The root password is recovered automatically from docker inspect when not in state.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slug := args[0]
		s, err := state.Load()
		if err != nil {
			return err
		}
		dbRec := dbRecForSlug(s, slug)
		if dbRec == nil {
			return fmt.Errorf("no database found for slug %q", slug)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		fmt.Printf("Resetting app user on %s...\n", dbRec.Container)
		if err := db.ResetAppUser(ctx, dbRec); err != nil {
			return fmt.Errorf("reset failed: %w", err)
		}
		persistRootPw(s, slug, dbRec)
		fmt.Println("Done. Restart the app container to reconnect:")
		fmt.Printf("  docker restart boxx-app-%s-blue boxx-app-%s-green 2>/dev/null\n", slug, slug)
		return nil
	},
}

var dbRecreateVersion string

var dbRecreateCmd = &cobra.Command{
	Use:   "recreate <slug>",
	Short: "Recreate the database container (preserves volume data)",
	Long: `Stops and removes the database container then starts a fresh one against
the same named volume. Use this to switch MySQL versions, e.g. from mysql:8
(8.4, no native_password) to mysql:8.0.

DATA IS PRESERVED as long as the new version can read the old data files.
If you are downgrading (e.g. 8.4 → 8.0), dump the data first or use --wipe.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slug := args[0]
		wipe, _ := cmd.Flags().GetBool("wipe")

		s, err := state.Load()
		if err != nil {
			return err
		}
		dbRec := dbRecForSlug(s, slug)
		if dbRec == nil {
			return fmt.Errorf("no database found for slug %q", slug)
		}

		version := dbRecreateVersion
		if version == "" {
			version = dbRec.Version
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		fmt.Printf("Stopping %s...\n", dbRec.Container)
		_ = dockerx.Stop(ctx, dbRec.Container, 10)
		_ = dockerx.Rm(ctx, dbRec.Container)

		if wipe {
			fmt.Printf("Wiping volume boxx_db_%s...\n", slug)
			_ = dockerx.RmVolume(ctx, "boxx_db_"+slug)
		}

		newRootPw := dbRec.RootPassword
		if newRootPw == "" {
			newRootPw = util.RandomPassword()
		}

		fmt.Printf("Starting %s (mysql:%s)...\n", dbRec.Container, version)
		if err := dockerx.Run(ctx, dockerx.RunOpts{
			Name:    dbRec.Container,
			Image:   "mysql:" + version,
			Network: "boxx_net",
			Restart: "unless-stopped",
			Env: []string{
				"MYSQL_ROOT_PASSWORD=" + newRootPw,
				"MYSQL_DATABASE=" + dbRec.Database,
				"MYSQL_USER=" + dbRec.Username,
				"MYSQL_PASSWORD=" + dbRec.Password,
			},
			Cmd: []string{"--default-authentication-plugin=mysql_native_password"},
			Volumes: map[string]string{
				"boxx_db_" + slug: "/var/lib/mysql",
			},
		}); err != nil {
			return err
		}

		fmt.Print("Waiting for MySQL to be ready")
		for i := 0; i < 30; i++ {
			_, err := dockerx.Exec(ctx, dbRec.Container,
				"mysqladmin", "ping", "-h", "127.0.0.1",
				"-u", dbRec.Username, "-p"+dbRec.Password, "--silent")
			if err == nil {
				fmt.Println(" ready.")
				break
			}
			fmt.Print(".")
			time.Sleep(2 * time.Second)
		}

		// Persist updated version + root password.
		dbRec.Version = version
		dbRec.RootPassword = newRootPw
		persistRootPw(s, slug, dbRec)

		fmt.Printf("Done. Restart the app: docker restart boxx-app-%s-blue boxx-app-%s-green 2>/dev/null\n", slug, slug)
		return nil
	},
}

func init() {
	dbRecreateCmd.Flags().StringVar(&dbRecreateVersion, "version", "", "MySQL/Postgres version to use (default: keep current)")
	dbRecreateCmd.Flags().Bool("wipe", false, "Also wipe the data volume (ALL DATA LOST)")
	dbForwardCmd.Flags().Int("port", 3306, "Local port to listen on")
	dbCmd.AddCommand(dbResetUserCmd)
	dbCmd.AddCommand(dbRecreateCmd)
	dbCmd.AddCommand(dbForwardCmd)
	dbCmd.AddCommand(dbUnforwardCmd)
	rootCmd.AddCommand(dbCmd)
}

var dbForwardCmd = &cobra.Command{
	Use:   "forward <slug>",
	Short: "Forward a local port to the app's database container",
	Long: `Starts a lightweight socat proxy that forwards 127.0.0.1:<port> to the
database container inside boxx_net. Useful for connecting a local GUI
client (TablePlus, DBeaver, etc.) to a locally-installed app's DB.

  boxx db forward nurun
  boxx db forward nurun --port 3307

Stop with: boxx db unforward <slug>`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slug := args[0]
		port, _ := cmd.Flags().GetInt("port")

		s, err := state.Load()
		if err != nil {
			return err
		}
		dbRec := dbRecForSlug(s, slug)
		if dbRec == nil {
			return fmt.Errorf("no database found for slug %q", slug)
		}

		fwdName := "boxx-fwd-" + slug
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Remove any existing forward for this slug.
		_ = dockerx.Rm(ctx, fwdName)

		dbPort := "3306"
		if dbRec.Engine == "postgres" || dbRec.Engine == "postgresql" {
			dbPort = "5432"
		}

		listenAddr := fmt.Sprintf("127.0.0.1:%d", port)

		if err := dockerx.Run(ctx, dockerx.RunOpts{
			Name:    fwdName,
			Image:   "alpine/socat",
			Network: "boxx_net",
			Restart: "unless-stopped",
			Ports:   map[string]string{listenAddr: dbPort},
			// socat takes exactly two address args — must be separate slice elements.
			Cmd: []string{
				fmt.Sprintf("TCP-LISTEN:%s,fork,reuseaddr", dbPort),
				fmt.Sprintf("TCP:%s:%s", dbRec.Container, dbPort),
			},
		}); err != nil {
			return fmt.Errorf("start forward: %w", err)
		}

		fmt.Printf("Forwarding %s → %s:%s\n\n", listenAddr, dbRec.Container, dbPort)
		fmt.Printf("  Engine:   %s\n", dbRec.Engine)
		fmt.Printf("  Host:     127.0.0.1\n")
		fmt.Printf("  Port:     %d\n", port)
		fmt.Printf("  Database: %s\n", dbRec.Database)
		fmt.Printf("  User:     %s\n", dbRec.Username)
		fmt.Printf("  Password: %s\n", dbRec.Password)
		fmt.Printf("\nStop with: boxx db unforward %s\n", slug)
		return nil
	},
}

var dbUnforwardCmd = &cobra.Command{
	Use:   "unforward <slug>",
	Short: "Stop the local port-forward for an app's database",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slug := args[0]
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		fwdName := "boxx-fwd-" + slug
		if err := dockerx.Rm(ctx, fwdName); err != nil {
			return err
		}
		fmt.Printf("Stopped forward for %s\n", slug)
		return nil
	},
}

func dbRecForSlug(s *state.State, slug string) *state.DB {
	if app, ok := s.Singles[slug]; ok {
		return app.DB
	}
	for _, g := range s.Groups {
		if g.Slug == slug && g.DB != nil {
			return g.DB
		}
	}
	return nil
}

func persistRootPw(s *state.State, slug string, d *state.DB) {
	if app, ok := s.Singles[slug]; ok && app.DB != nil {
		app.DB.RootPassword = d.RootPassword
		app.DB.Version = d.Version
		s.Singles[slug] = app
		_ = state.Save(s)
	}
}
