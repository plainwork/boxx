package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/plainwork/boxx/engine/envfile"
	"github.com/plainwork/boxx/engine/state"
	"github.com/spf13/cobra"
)

// managedKeys are auto-injected by boxx and must not be overridden via config.
var managedKeys = map[string]bool{
	"DATABASE_URL": true,
	"BASE_PATH":    true,
	"PORT":         true,
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage per-app environment variables",
}

var configSetCmd = &cobra.Command{
	Use:   "set <slug> KEY=VALUE",
	Short: "Set an environment variable for an app",
	Args:  cobra.ExactArgs(2),
	RunE: func(c *cobra.Command, args []string) error {
		slug := args[0]
		kv := args[1]
		idx := strings.IndexByte(kv, '=')
		if idx < 1 {
			return fmt.Errorf("invalid format: expected KEY=VALUE")
		}
		key := strings.TrimSpace(kv[:idx])
		val := kv[idx+1:]
		if managedKeys[key] {
			return fmt.Errorf("%s is managed automatically by boxx and cannot be set via config", key)
		}
		s, err := state.Load()
		if err != nil {
			return err
		}
		if err := setEnv(s, slug, key, val); err != nil {
			return err
		}
		if err := state.Save(s); err != nil {
			return err
		}
		fmt.Printf("Set %s for %s.\nRun 'boxx deploy %s' to apply.\n", key, slug, slug)
		return nil
	},
}

var configUnsetCmd = &cobra.Command{
	Use:   "unset <slug> KEY",
	Short: "Remove an environment variable from an app",
	Args:  cobra.ExactArgs(2),
	RunE: func(c *cobra.Command, args []string) error {
		slug, key := args[0], args[1]
		s, err := state.Load()
		if err != nil {
			return err
		}
		if err := unsetEnv(s, slug, key); err != nil {
			return err
		}
		if err := state.Save(s); err != nil {
			return err
		}
		fmt.Printf("Unset %s for %s.\nRun 'boxx deploy %s' to apply.\n", key, slug, slug)
		return nil
	},
}

var configLsCmd = &cobra.Command{
	Use:   "ls <slug>",
	Short: "List stored environment variables for an app",
	Args:  cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		slug := args[0]
		s, err := state.Load()
		if err != nil {
			return err
		}
		env, err := getEnv(s, slug)
		if err != nil {
			return err
		}
		if len(env) == 0 {
			fmt.Printf("No config vars set for %s.\n", slug)
			return nil
		}
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("%s=%s\n", k, env[k])
		}
		return nil
	},
}

var configImportCmd = &cobra.Command{
	Use:   "import <slug> <file>",
	Short: "Import environment variables from a .env file",
	Long: `Reads KEY=VALUE pairs from a .env file and stores them for the app.
Lines starting with # and blank lines are ignored.
The auto-managed keys DATABASE_URL, BASE_PATH, and PORT are always skipped.`,
	Args: cobra.ExactArgs(2),
	RunE: func(c *cobra.Command, args []string) error {
		slug, path := args[0], args[1]
		pairs, err := envfile.Parse(path)
		if err != nil {
			return err
		}
		s, err := state.Load()
		if err != nil {
			return err
		}
		skipped := []string{}
		imported := []string{}
		for k, v := range pairs {
			if managedKeys[k] {
				skipped = append(skipped, k)
				continue
			}
			if err := setEnv(s, slug, k, v); err != nil {
				return err
			}
			imported = append(imported, k)
		}
		if err := state.Save(s); err != nil {
			return err
		}
		sort.Strings(imported)
		sort.Strings(skipped)
		if len(imported) > 0 {
			fmt.Printf("Imported %d var(s) for %s: %s\n", len(imported), slug, strings.Join(imported, ", "))
		}
		if len(skipped) > 0 {
			fmt.Printf("Skipped (auto-managed): %s\n", strings.Join(skipped, ", "))
		}
		if len(imported) > 0 {
			fmt.Printf("Run 'boxx deploy %s' to apply.\n", slug)
		}
		return nil
	},
}

// ---------- helpers ----------

func resolveSlug(s *state.State, slug string) (kind, groupSlug, appSlug string, err error) {
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) == 2 {
		g, ok := s.Groups[parts[0]]
		if !ok {
			return "", "", "", fmt.Errorf("no group with slug %q", parts[0])
		}
		if _, ok := g.Apps[parts[1]]; !ok {
			return "", "", "", fmt.Errorf("no app %q in group %q", parts[1], parts[0])
		}
		return "group", parts[0], parts[1], nil
	}
	if _, ok := s.Singles[slug]; ok {
		return "single", slug, "", nil
	}
	return "", "", "", fmt.Errorf("no installed app with slug %q (use \"group/app\" form for grouped apps)", slug)
}

func setEnv(s *state.State, slug, key, val string) error {
	kind, gslug, aslug, err := resolveSlug(s, slug)
	if err != nil {
		return err
	}
	if kind == "single" {
		a := s.Singles[gslug]
		if a.Env == nil {
			a.Env = map[string]string{}
		}
		a.Env[key] = val
		s.Singles[gslug] = a
	} else {
		g := s.Groups[gslug]
		a := g.Apps[aslug]
		if a.Env == nil {
			a.Env = map[string]string{}
		}
		a.Env[key] = val
		g.Apps[aslug] = a
		s.Groups[gslug] = g
	}
	return nil
}

func unsetEnv(s *state.State, slug, key string) error {
	kind, gslug, aslug, err := resolveSlug(s, slug)
	if err != nil {
		return err
	}
	if kind == "single" {
		a := s.Singles[gslug]
		delete(a.Env, key)
		s.Singles[gslug] = a
	} else {
		g := s.Groups[gslug]
		a := g.Apps[aslug]
		delete(a.Env, key)
		g.Apps[aslug] = a
		s.Groups[gslug] = g
	}
	return nil
}

func getEnv(s *state.State, slug string) (map[string]string, error) {
	kind, gslug, aslug, err := resolveSlug(s, slug)
	if err != nil {
		return nil, err
	}
	if kind == "single" {
		return s.Singles[gslug].Env, nil
	}
	return s.Groups[gslug].Apps[aslug].Env, nil
}

func init() {
	configCmd.AddCommand(configSetCmd, configUnsetCmd, configLsCmd, configImportCmd)
	rootCmd.AddCommand(configCmd)
	_ = os.Stderr // satisfy import if needed
}
