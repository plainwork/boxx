package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/plainwork/boxx/engine/envfile"
	"github.com/plainwork/boxx/engine/installer"
	"github.com/plainwork/boxx/engine/state"
	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage environment variables for installed apps",
}

// ---- env show ---------------------------------------------------------------

var envShowCmd = &cobra.Command{
	Use:   "show <slug>",
	Short: "Show current env vars for an app",
	Long: `Show the env vars stored in boxx state for an app.
DATABASE_URL and BASE_PATH are managed by boxx and are not shown here.

For a group app use --app:
  boxx env show dev --app nurun`,
	Args: cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		s, err := state.Load()
		if err != nil {
			return err
		}
		env, label, err := resolveEnv(s, args[0], envCmdApp)
		if err != nil {
			return err
		}
		if len(env) == 0 {
			fmt.Fprintf(os.Stdout, "  (no custom env vars set for %s)\n", label)
			return nil
		}
		keys := sortedKeys(env)
		for _, k := range keys {
			fmt.Fprintf(os.Stdout, "  %s=%s\n", k, env[k])
		}
		return nil
	},
}

// ---- env push ---------------------------------------------------------------

var envPushCmd = &cobra.Command{
	Use:   "push <slug>",
	Short: "Set env vars for an app, then redeploy",
	Long: `Load env vars (optionally from a .env file), open $EDITOR to review and
edit values, save to boxx state, then redeploy the app so the new env takes effect.

Auto-managed vars (DATABASE_URL, BASE_PATH) are injected by boxx and should not
be set here — they will be overwritten at deploy time.

For a group app use --app:
  boxx env push dev --app nurun --file .env
  boxx env push dev --app nurun-admin --file .env`,
	Args: cobra.ExactArgs(1),
	RunE: runEnvPush,
}

var envCmdApp  string
var envCmdFile string

func init() {
	envShowCmd.Flags().StringVar(&envCmdApp, "app", "", "target app within a group")
	envPushCmd.Flags().StringVar(&envCmdApp, "app", "", "target app within a group")
	envPushCmd.Flags().StringVar(&envCmdFile, "file", "", "seed from this .env file before opening editor")
	envImportCmd.Flags().StringVar(&envCmdApp, "app", "", "target app within a group")
	envImportCmd.Flags().StringVar(&envCmdFile, "file", "", "path to .env file to import (required)")
	_ = envImportCmd.MarkFlagRequired("file")
	envRollbackCmd.Flags().StringVar(&envCmdApp, "app", "", "target app within a group")
	envCmd.AddCommand(envShowCmd)
	envCmd.AddCommand(envPushCmd)
	envCmd.AddCommand(envImportCmd)
	envCmd.AddCommand(envRollbackCmd)
	rootCmd.AddCommand(envCmd)
}

func runEnvPush(c *cobra.Command, args []string) error {
	slug := args[0]

	s, err := state.Load()
	if err != nil {
		return err
	}

	// load existing state env
	existing, label, err := resolveEnv(s, slug, envCmdApp)
	if err != nil {
		return err
	}

	// merge --file on top (file wins over existing state)
	merged := map[string]string{}
	for k, v := range existing {
		merged[k] = v
	}
	if envCmdFile != "" {
		fromFile, err := envfile.ParseFile(envCmdFile)
		if err != nil {
			return fmt.Errorf("--file: %w", err)
		}
		for k, v := range fromFile {
			merged[k] = v
		}
	}

	// open $EDITOR for review
	edited, err := openInEditor(merged, label)
	if err != nil {
		return err
	}

	// strip boxx-managed keys — they're always injected at deploy time
	stripManagedKeys(edited)

	// backup current env before overwriting
	backupEnv(s, slug, envCmdApp, "env push")

	// save to state
	if err := applyEnvToState(s, slug, envCmdApp, edited); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "  [env  ] saved %d var(s) for %s\n", len(edited), label)

	// redeploy so the new env takes effect
	slugRef := slug
	if envCmdApp != "" {
		slugRef = slug + "/" + envCmdApp
	}
	fmt.Fprintf(os.Stdout, "  [deploy] redeploying %s…\n", slugRef)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	return installer.Deploy(ctx, installer.DeploySpec{Slug: slugRef}, func(step, msg string) {
		fmt.Fprintf(os.Stdout, "  [%-5s] %s\n", step, msg)
	})
}

// ---- helpers ----------------------------------------------------------------

// resolveEnv returns the current env map for the target plus a human label.
func resolveEnv(s *state.State, slug, appSlug string) (map[string]string, string, error) {
	if appSlug != "" {
		g, ok := s.Groups[slug]
		if !ok {
			return nil, "", fmt.Errorf("group %q not found — run 'boxx ls' to list installed apps", slug)
		}
		a, ok := g.Apps[appSlug]
		if !ok {
			apps := make([]string, 0, len(g.Apps))
			for k := range g.Apps {
				apps = append(apps, k)
			}
			return nil, "", fmt.Errorf("app %q not found in group %q (apps: %s)", appSlug, slug, strings.Join(apps, ", "))
		}
		label := slug + "/" + appSlug
		env := map[string]string{}
		for k, v := range a.Env {
			env[k] = v
		}
		return env, label, nil
	}

	if app, ok := s.Singles[slug]; ok {
		env := map[string]string{}
		for k, v := range app.Env {
			env[k] = v
		}
		return env, slug, nil
	}

	if g, ok := s.Groups[slug]; ok {
		// group slug given without --app: list the apps so the user knows
		apps := make([]string, 0, len(g.Apps))
		for k := range g.Apps {
			apps = append(apps, k)
		}
		return nil, "", fmt.Errorf(
			"%q is a group with apps: %s\nUse --app to target one, e.g.:\n  boxx env push %s --app %s",
			slug, strings.Join(apps, ", "), slug, apps[0],
		)
	}

	return nil, "", fmt.Errorf("app %q not found — run 'boxx ls' to list installed apps", slug)
}

func applyEnvToState(s *state.State, slug, appSlug string, env map[string]string) error {
	if appSlug != "" {
		g := s.Groups[slug]
		a := g.Apps[appSlug]
		a.Env = env
		g.Apps[appSlug] = a
		s.Groups[slug] = g
	} else {
		app := s.Singles[slug]
		app.Env = env
		s.Singles[slug] = app
	}
	return state.Save(s)
}

// ---- env import -------------------------------------------------------------

var envImportCmd = &cobra.Command{
	Use:   "import <slug>",
	Short: "Replace env vars for an app from a file, then redeploy",
	Long: `Replace all env vars from a .env file (no editor opens).

For a group app use --app:
  boxx env import dev --app nurun --file prod.env`,
	Args: cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		slug := args[0]
		s, err := state.Load()
		if err != nil {
			return err
		}
		parsed, err := envfile.ParseFile(envCmdFile)
		if err != nil {
			return fmt.Errorf("--file: %w", err)
		}
		stripManagedKeys(parsed)
		backupEnv(s, slug, envCmdApp, "env import")
		_, label, err := resolveEnv(s, slug, envCmdApp)
		if err != nil {
			return err
		}
		if err := applyEnvToState(s, slug, envCmdApp, parsed); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "  [env  ] imported %d var(s) for %s\n", len(parsed), label)
		slugRef := slug
		if envCmdApp != "" {
			slugRef = slug + "/" + envCmdApp
		}
		fmt.Fprintf(os.Stdout, "  [deploy] redeploying %s…\n", slugRef)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		return installer.Deploy(ctx, installer.DeploySpec{Slug: slugRef}, func(step, msg string) {
			fmt.Fprintf(os.Stdout, "  [%-5s] %s\n", step, msg)
		})
	},
}

// ---- env rollback -----------------------------------------------------------

var envRollbackCmd = &cobra.Command{
	Use:   "rollback <slug>",
	Short: "Restore the previous env backup and redeploy",
	Long: `Restore the single previous env snapshot (taken before the last push or import)
and redeploy the app.

For a group app use --app:
  boxx env rollback dev --app nurun`,
	Args: cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		slug := args[0]
		s, err := state.Load()
		if err != nil {
			return err
		}
		prev, label, err := resolvePrevEnv(s, slug, envCmdApp)
		if err != nil {
			return err
		}
		if prev == nil {
			return fmt.Errorf("no env backup found for %s — nothing to roll back", label)
		}
		fmt.Fprintf(os.Stdout, "  [env  ] rolling back %s to backup from %s (reason: %s)\n",
			label, prev.BackupTime.Format(time.RFC3339), prev.Reason)
		// Clear the backup after restoring so it's not applied twice.
		clearPrevEnv(s, slug, envCmdApp)
		if err := applyEnvToState(s, slug, envCmdApp, prev.Env); err != nil {
			return err
		}
		slugRef := slug
		if envCmdApp != "" {
			slugRef = slug + "/" + envCmdApp
		}
		fmt.Fprintf(os.Stdout, "  [deploy] redeploying %s…\n", slugRef)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		return installer.Deploy(ctx, installer.DeploySpec{Slug: slugRef}, func(step, msg string) {
			fmt.Fprintf(os.Stdout, "  [%-5s] %s\n", step, msg)
		})
	},
}

// openInEditor writes env to a temp file, opens $EDITOR, and returns the parsed result.
func openInEditor(env map[string]string, label string) (map[string]string, error) {
	tmp, err := os.CreateTemp("", "boxx-env-*.env")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())

	fmt.Fprintf(tmp, "# boxx env — %s\n", label)
	fmt.Fprintf(tmp, "# Edit values below, then save and close to apply.\n")
	fmt.Fprintf(tmp, "# Lines starting with # are ignored. DATABASE_URL and BASE_PATH are managed by boxx.\n\n")
	fmt.Fprint(tmp, envfile.Format(env))
	tmp.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "nano"
	}

	cmd := exec.Command(editor, tmp.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("editor exited with error: %w", err)
	}

	return envfile.ParseFile(tmp.Name())
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// managedKeys are always injected by boxx at deploy time and must not be stored
// as user-supplied env — doing so would let stale values override boxx's values
// on future deploys. Defined in config.go.

func stripManagedKeys(env map[string]string) {
	for k := range managedKeys {
		delete(env, k)
	}
}

// backupEnv stores a snapshot of the current env as PrevEnv before overwriting.
func backupEnv(s *state.State, slug, appSlug, reason string) {
	snap := func(m map[string]string) map[string]string {
		cp := make(map[string]string, len(m))
		for k, v := range m {
			cp[k] = v
		}
		return cp
	}
	backup := &state.EnvBackup{BackupTime: time.Now().UTC(), Reason: reason}
	if appSlug == "" {
		app := s.Singles[slug]
		backup.Env = snap(app.Env)
		app.PrevEnv = backup
		s.Singles[slug] = app
	} else {
		grp := s.Groups[slug]
		app := grp.Apps[appSlug]
		backup.Env = snap(app.Env)
		app.PrevEnv = backup
		grp.Apps[appSlug] = app
		s.Groups[slug] = grp
	}
}

// resolvePrevEnv returns the stored EnvBackup (nil if none) and a human label.
func resolvePrevEnv(s *state.State, slug, appSlug string) (*state.EnvBackup, string, error) {
	if appSlug == "" {
		app, ok := s.Singles[slug]
		if !ok {
			return nil, "", fmt.Errorf("no single app %q", slug)
		}
		return app.PrevEnv, slug, nil
	}
	grp, ok := s.Groups[slug]
	if !ok {
		return nil, "", fmt.Errorf("no group %q", slug)
	}
	app, ok := grp.Apps[appSlug]
	if !ok {
		return nil, "", fmt.Errorf("group %q has no app %q", slug, appSlug)
	}
	return app.PrevEnv, slug + "/" + appSlug, nil
}

// clearPrevEnv removes the stored backup so it cannot be double-applied.
func clearPrevEnv(s *state.State, slug, appSlug string) {
	if appSlug == "" {
		app := s.Singles[slug]
		app.PrevEnv = nil
		s.Singles[slug] = app
		return
	}
	grp := s.Groups[slug]
	app := grp.Apps[appSlug]
	app.PrevEnv = nil
	grp.Apps[appSlug] = app
	s.Groups[slug] = grp
}
